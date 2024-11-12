package monitor

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/C4T-BuT-S4D/shpaga/internal/authutil"
	"github.com/C4T-BuT-S4D/shpaga/internal/config"
	"github.com/C4T-BuT-S4D/shpaga/internal/models"
	"github.com/C4T-BuT-S4D/shpaga/internal/storage"
	"github.com/sirupsen/logrus"
	"gopkg.in/telebot.v4"
)

type Monitor struct {
	config  *config.Config
	storage *storage.Storage
	bot     telebot.API
}

func New(cfg *config.Config, storage *storage.Storage, bot telebot.API) *Monitor {
	return &Monitor{
		config:  cfg,
		storage: storage,
		bot:     bot,
	}
}

func (m *Monitor) HandleMessage(c telebot.Context) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	user, err := m.storage.GetOrCreateUser(ctx, c.Chat().ID, c.Sender().ID, models.UserStatusActive)
	if err != nil {
		return fmt.Errorf("failed to get or create user: %w", err)
	}

	logrus.Infof("User %d sent message to chat %d", user.TelegramID, c.Chat().ID)

	if user.Status == models.UserStatusJustJoined {
		logrus.Infof("User %d is just joined, removing message until user logs in", user.TelegramID)
		if err := c.Bot().Delete(c.Message()); err != nil {
			logrus.Warnf("failed to delete message: %v", err)
		}
	}

	return nil
}

func (m *Monitor) HandleUserJoined(c telebot.Context) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	logrus.Infof("User %s (%d) joined the chat %d", c.Sender().Username, c.Sender().ID, c.Chat().ID)

	logrus.Infof("Deleting chat join request message")
	if err := c.Bot().Delete(c.Message()); err != nil {
		return fmt.Errorf("failed to delete chat join request: %w", err)
	}

	user, err := m.storage.GetOrCreateUser(ctx, c.Chat().ID, c.Sender().ID, models.UserStatusJustJoined)
	if err != nil {
		return fmt.Errorf("failed to get or create user: %w", err)
	}

	switch user.Status {
	case models.UserStatusJustJoined:
		logrus.Infof("User %d is just joined, sending welcome message", user.TelegramID)

		url, err := authutil.GetCTFTimeOAuthURL(user.ID, c.Chat().ID, m.config)
		if err != nil {
			return fmt.Errorf("failed to get oauth url: %w", err)
		}

		name := ""
		if c.Sender().FirstName != "" || c.Sender().LastName != "" {
			name = fmt.Sprintf("%s %s", c.Sender().FirstName, c.Sender().LastName)
		} else {
			name = c.Sender().Username
		}

		greeting := fmt.Sprintf(
			`Welcome to the chat, %s! Please, press the button below to log in with https://ctftime.org. 
			You won't be able to send messages until you do so. 
			The bot will kick you in 10 minutes if you don't login.`,
			name,
		)
		markup := &telebot.ReplyMarkup{}
		markup.Inline(markup.Row(markup.URL("Log in with CTFTime", url)))
		msg, err := c.Bot().Send(c.Chat(), greeting, markup)
		if err != nil {
			return fmt.Errorf("sending welcome message: %w", err)
		}

		if err := m.storage.AddMessage(ctx, &models.Message{
			ChatID:           c.Chat().ID,
			MessageID:        strconv.Itoa(msg.ID),
			MessageType:      models.MessageTypeGreeting,
			AssociatedUserID: user.ID,
		}); err != nil {
			return fmt.Errorf("adding welcome message to db: %w", err)
		}

		return nil

	case models.UserStatusActive:
		logrus.Infof("User %d is already logged in, skipping validation", user.TelegramID)
		return nil

	case models.UserStatusBanned:
		logrus.Warnf("User %d is banned, skipping validation, please investigate", user.TelegramID)
		return nil
	}

	return nil
}

func (m *Monitor) HandleChatLeft(c telebot.Context) error {
	logrus.Infof("User %s (%d) left the chat %d", c.Sender().Username, c.Sender().ID, c.Chat().ID)
	if err := c.Bot().Delete(c.Message()); err != nil {
		return fmt.Errorf("deleting message: %w", err)
	}
	return nil
}

func (m *Monitor) RunCleaner(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()

	for {
		select {
		case <-t.C:
			msgs, err := m.storage.GetMessagesOlderThan(ctx, time.Now().Add(-time.Minute*10))
			if err != nil {
				logrus.Errorf("failed to get messages: %v", err)
				continue
			}
			if len(msgs) == 0 {
				logrus.Debug("no old messages to clean")
				break
			}

			logrus.Infof("fetched %d old messages, cleaning up", len(msgs))
			for _, msg := range msgs {
				if msg.MessageType == models.MessageTypeGreeting {
					user, err := m.storage.GetUser(ctx, msg.AssociatedUserID)
					if err != nil {
						logrus.Errorf("failed to get user: %v", err)
					} else if user.Status == models.UserStatusJustJoined {
						logrus.Infof("removing user %v by timeout", user.TelegramID)

						if err := m.bot.Unban(
							&telebot.Chat{ID: user.ChatID},
							&telebot.User{ID: user.TelegramID},
						); err != nil {
							logrus.Errorf("failed to kick user %v: %v", user, err)
						}

						if err := m.storage.OnUserBanned(ctx, user.ID); err != nil {
							logrus.Errorf("failed to update user to banned %v: %v", user, err)
						}
					}
				}

				if err := m.bot.Delete(msg); err != nil {
					logrus.Errorf("failed to delete message %v: %v", msg, err)
				}
			}

			if err := m.storage.DeleteMessages(ctx, msgs); err != nil {
				logrus.Errorf("failed to delete messages: %v", err)
			}

		case <-ctx.Done():
			return
		}
	}
}
