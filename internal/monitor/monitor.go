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

func (m *Monitor) HandleAnyUpdate(c telebot.Context) error {
	ctx, cancel := context.WithTimeout(context.Background(), m.config.BotHandleTimeout)
	defer cancel()

	uc := NewUpdateContext(ctx, c)

	uc.L().Debugf(
		"Received update message=%v, user_joined=%v, user_left=%v",
		c.Message(),
		c.Message().UserJoined,
		c.Message().UserLeft,
	)

	if err := m.storage.UpdateLastUpdate(uc, c.Update().ID); err != nil {
		uc.L().Errorf("failed to update last update: %v", err)
	}

	if c.Chat().Type != telebot.ChatGroup && c.Chat().Type != telebot.ChatSuperGroup {
		uc.L().Debugf("ignoring update from non-group chat %d", c.Chat().ID)
		return nil
	}

	if c.Message() == nil {
		uc.L().Debugf("ignoring update without message")
		return nil
	}

	switch {
	case c.Message().UserJoined != nil:
		if err := m.HandleUserJoined(uc); err != nil {
			uc.L().Errorf("failed to handle user joined: %v", err)
		}
	case c.Message().UserLeft != nil:
		if err := m.HandleChatLeft(uc); err != nil {
			uc.L().Errorf("failed to handle chat left: %v", err)
		}
	default:
		if err := m.HandleMessage(uc); err != nil {
			uc.L().Errorf("failed to handle message: %v", err)
		}
	}

	return nil
}

func (m *Monitor) HandleMessage(uc *UpdateContext) error {
	user, err := m.storage.GetOrCreateUser(uc, uc.TC().Chat().ID, uc.TC().Sender().ID, models.UserStatusActive)
	if err != nil {
		return fmt.Errorf("failed to get or create user: %w", err)
	}

	uc.L().Infof("User %d sent message to chat %d", user.TelegramID, uc.Chat().ID)

	if user.Status == models.UserStatusJustJoined || user.Status == models.UserStatusKicked {
		uc.L().Infof("User %d is just joined, removing message until user logs in", user.TelegramID)
		if err := uc.Bot().Delete(uc.Message()); err != nil {
			uc.L().Warnf("failed to delete message: %v", err)
		}
	}

	return nil
}

func (m *Monitor) HandleUserJoined(uc *UpdateContext) error {
	if uc.Sender().IsBot {
		uc.L().Infof("bot %s (%d) joined the chat %d, ignoring", uc.Sender().Username, uc.Sender().ID, uc.Chat().ID)
		return nil
	}

	uc.L().Infof("User %s (%d) joined the chat %d", uc.Sender().Username, uc.Sender().ID, uc.Chat().ID)

	uc.L().Infof("Deleting chat join request message")
	if err := uc.Bot().Delete(uc.Message()); err != nil {
		return fmt.Errorf("failed to delete chat join request: %w", err)
	}

	user, err := m.storage.GetOrCreateUser(uc, uc.Chat().ID, uc.Sender().ID, models.UserStatusJustJoined)
	if err != nil {
		return fmt.Errorf("failed to get or create user: %w", err)
	}

	if user.Status == models.UserStatusKicked {
		if err := m.storage.SetUserStatus(uc, user.ID, models.UserStatusJustJoined); err != nil {
			return fmt.Errorf("setting kicked user status: %w", err)
		}
		user.Status = models.UserStatusJustJoined
	}

	switch user.Status {
	case models.UserStatusJustJoined:
		uc.L().Infof("User %d is just joined, sending welcome message", user.TelegramID)

		url, err := authutil.GetCTFTimeOAuthURL(user.ID, uc.Chat().ID, m.config)
		if err != nil {
			return fmt.Errorf("failed to get oauth url: %w", err)
		}

		name := ""
		if uc.Sender().FirstName != "" || uc.Sender().LastName != "" {
			name = fmt.Sprintf("%s %s", uc.Sender().FirstName, uc.Sender().LastName)
		} else {
			name = uc.Sender().Username
		}

		greeting := fmt.Sprintf(
			`Welcome to the chat, [%s](tg://user?id=%d)\! `+
				`Please, press the button below to log in with [CTFTime](https://ctftime.org)\. `+
				`You won't be able to send messages until you do so\. `+
				`The bot will kick you in %d minutes if you don't login\.`,
			name,
			uc.Sender().ID,
			m.config.JoinLoginTimeout/time.Minute,
		)
		markup := &telebot.ReplyMarkup{}
		markup.Inline(markup.Row(markup.URL("Log in with CTFTime", url)))
		msg, err := uc.Bot().Send(uc.Chat(), greeting, markup, telebot.ModeMarkdownV2)
		if err != nil {
			return fmt.Errorf("sending welcome message: %w", err)
		}

		if err := m.storage.AddMessage(uc, &models.Message{
			ChatID:           uc.Chat().ID,
			MessageID:        strconv.Itoa(msg.ID),
			MessageType:      models.MessageTypeGreeting,
			AssociatedUserID: user.ID,
		}); err != nil {
			return fmt.Errorf("adding welcome message to db: %w", err)
		}

		return nil

	case models.UserStatusActive:
		uc.L().Infof("User %d is already logged in, skipping validation", user.TelegramID)
		return nil

	case models.UserStatusBanned:
		uc.L().Warnf("User %d is banned, skipping validation, please investigate", user.TelegramID)
		return nil
	}

	return nil
}

func (m *Monitor) HandleChatLeft(uc *UpdateContext) error {
	uc.L().Infof("User %s (%d) left the chat %d", uc.Sender().Username, uc.Sender().ID, uc.Chat().ID)
	if err := uc.Bot().Delete(uc.Message()); err != nil {
		return fmt.Errorf("deleting message: %w", err)
	}
	return nil
}

func (m *Monitor) RunCleaner(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()

	logger := logrus.WithField("component", "monitor_cleaner")

	for {
		select {
		case <-t.C:
			msgs, err := m.storage.GetMessagesOlderThan(
				ctx,
				time.Now().Add(-m.config.JoinLoginTimeout),
			)
			if err != nil {
				logger.Errorf("failed to get messages: %v", err)
				continue
			}
			if len(msgs) == 0 {
				logger.Debug("no old messages to clean")
				break
			}

			logger.Infof("fetched %d old messages, cleaning up", len(msgs))
			for _, msg := range msgs {
				if msg.MessageType == models.MessageTypeGreeting {
					user, err := m.storage.GetUser(ctx, msg.AssociatedUserID)
					if err != nil {
						logger.Errorf("failed to get user: %v", err)
					} else if user.Status == models.UserStatusJustJoined {
						logger.Infof("removing user %v by timeout", user.TelegramID)

						if err := m.bot.Unban(
							&telebot.Chat{ID: user.ChatID},
							&telebot.User{ID: user.TelegramID},
						); err != nil {
							logger.Errorf("failed to kick user %v: %v", user, err)
						}

						if err := m.storage.OnUserKicked(ctx, user.ID); err != nil {
							logger.Errorf("failed to update user to kicked %v: %v", user, err)
						}
					}
				}

				if err := m.bot.Delete(msg); err != nil {
					logger.Errorf("failed to delete message %v: %v", msg, err)
				}
			}

			if err := m.storage.DeleteMessages(ctx, msgs); err != nil {
				logger.Errorf("failed to delete messages: %v", err)
			}

		case <-ctx.Done():
			return
		}
	}
}
