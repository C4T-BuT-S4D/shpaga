package monitor

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/C4T-BuT-S4D/shpaga/internal/authutil"
	"github.com/C4T-BuT-S4D/shpaga/internal/config"
	"github.com/C4T-BuT-S4D/shpaga/internal/models"
	"github.com/C4T-BuT-S4D/shpaga/internal/storage"
	"github.com/sirupsen/logrus"
	"gopkg.in/telebot.v4"
)

var allowedChatTypes = []telebot.ChatType{
	telebot.ChatGroup,
	telebot.ChatSuperGroup,
	telebot.ChatPrivate,
}

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

	if err := m.storage.UpdateLastUpdate(uc, c.Update().ID); err != nil {
		uc.L().Errorf("failed to update last update: %v", err)
	}

	switch {
	case c.Message() != nil:
		uc.L().Debugf(
			"received message update: message=%v, user_joined=%v, user_left=%v",
			c.Message(),
			c.Message().UserJoined,
			c.Message().UserLeft,
		)
	case c.ChatMember() != nil:
		uc.L().Debugf(
			"received chat member update: chat_member=%v, old_chat_member=%v, new_chat_member=%v",
			c.ChatMember(),
			c.ChatMember().OldChatMember,
			c.ChatMember().NewChatMember,
		)
	default:
		uc.L().Warnf("received unknown update %+v", c.Update())
	}

	if !slices.Contains(allowedChatTypes, c.Chat().Type) {
		uc.L().Debugf("ignoring update from unknown chat type")
		return nil
	}

	switch {
	case c.Chat().Type == telebot.ChatPrivate:
		if err := m.HandlePrivateMessage(uc); err != nil {
			uc.L().Errorf("failed to handle private message: %v", err)
		}
	case c.Message() != nil && c.Message().UserJoined != nil:
		if err := m.HandleNewMember(uc); err != nil {
			uc.L().Errorf("failed to handle user joined: %v", err)
		}
	case c.Message() != nil && c.Message().UserLeft != nil:
		if err := m.HandleMemberLeft(uc); err != nil {
			uc.L().Errorf("failed to handle chat left: %v", err)
		}
	case c.ChatMember() != nil && c.ChatMember().OldChatMember != nil && c.ChatMember().OldChatMember.Member:
		if err := m.HandleMemberLeft(uc); err != nil {
			uc.L().Errorf("failed to handle chat member left: %v", err)
		}
	case c.ChatMember() != nil && c.ChatMember().NewChatMember != nil && c.ChatMember().NewChatMember.Member:
		if err := m.HandleNewMember(uc); err != nil {
			uc.L().Errorf("failed to handle chat member join: %v", err)
		}
	default:
		if err := m.HandleChatMessage(uc); err != nil {
			uc.L().Errorf("failed to handle message: %v", err)
		}
	}

	return nil
}

func (m *Monitor) HandleChatMessage(uc *UpdateContext) error {
	user, err := m.storage.GetOrCreateUser(uc, uc.TC().Chat().ID, uc.TC().Sender().ID, models.UserStatusActive)
	if err != nil {
		return fmt.Errorf("failed to get or create user: %w", err)
	}

	uc.SetLoggerUser(user)

	uc.L().Info("user sent message to chat")

	if user.Status == models.UserStatusJustJoined || user.Status == models.UserStatusKicked {
		uc.L().Info("user just joined, removing message until user logs in")
		if err := uc.Bot().Delete(uc.Message()); err != nil {
			uc.L().Warnf("failed to delete message: %v", err)
		}
	}

	return nil
}

func (m *Monitor) HandleNewMember(uc *UpdateContext) error {
	if uc.Sender().IsBot {
		uc.L().Info("bot joined, ignoring")
		return nil
	}

	uc.L().Info("user joined")

	if uc.Message() != nil {
		uc.L().Info("deleting chat join request message")
		if err := uc.Bot().Delete(uc.Message()); err != nil {
			return fmt.Errorf("failed to delete chat join request: %w", err)
		}
	}

	user, err := m.storage.GetOrCreateUser(uc, uc.Chat().ID, uc.Sender().ID, models.UserStatusJustJoined)
	if err != nil {
		return fmt.Errorf("get or create user: %w", err)
	}

	uc.SetLoggerUser(user)

	if user.Status == models.UserStatusKicked {
		if err := m.storage.SetUserStatus(uc, user.ID, models.UserStatusJustJoined); err != nil {
			return fmt.Errorf("setting kicked user status: %w", err)
		}
		user.Status = models.UserStatusJustJoined
	}

	switch user.Status {
	case models.UserStatusJustJoined:
		uc.L().Info("user just joined, sending welcome message")

		name := ""
		if uc.Sender().FirstName != "" || uc.Sender().LastName != "" {
			name = fmt.Sprintf("%s %s", uc.Sender().FirstName, uc.Sender().LastName)
		} else {
			name = uc.Sender().Username
		}

		botName := uc.Bot().(*telebot.Bot).Me.Username
		url := fmt.Sprintf("t.me/%s?start=%d", botName, user.ChatID)

		greeting := fmt.Sprintf(
			`Welcome to the chat, [%s](tg://user?id=%d)\! `+
				`Please, press the button below, start the bot and follow the instructions `+
				`to log in with [CTFTime](https://ctftime.org)\. `+
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
		uc.L().Info("user already logged in")
		return nil

	case models.UserStatusBanned:
		uc.L().Warn("user is banned, skipping validation, please investigate")
		return nil

	default:
		uc.L().Warnf("user has unexpected status %v, skipping validation", user.Status)
		return nil
	}
}

func (m *Monitor) HandleMemberLeft(uc *UpdateContext) error {
	uc.L().Info("user left the chat")

	if uc.Message() != nil {
		if err := uc.Bot().Delete(uc.Message()); err != nil {
			uc.L().Errorf("failed to delete message: %v", err)
		}
	}

	user, err := m.storage.GetChatUser(uc, uc.Chat().ID, uc.Sender().ID)
	if err != nil {
		return fmt.Errorf("getting user: %w", err)
	}

	uc.SetLoggerUser(user)

	if err := m.removeGreetingsForUser(uc, user); err != nil {
		return fmt.Errorf("removing greetings for user: %w", err)
	}

	return nil
}

func (m *Monitor) HandlePrivateMessage(uc *UpdateContext) error {
	uc.L().Infof("user sent private message %v", uc.Message().Text)

	tokens := strings.Fields(uc.Message().Text)
	if len(tokens) < 2 || tokens[0] != "/start" {
		uc.L().Infof("ignoring non-start message %v", uc.Message().Text)
		return nil
	}

	chatID, err := strconv.ParseInt(tokens[1], 10, 64)
	if err != nil {
		uc.L().Errorf("failed to parse chat id: %v", err)
		if err := uc.TC().Send("Invalid chat id"); err != nil {
			uc.L().Errorf("failed to send message: %v", err)
		}
		return nil
	}

	user, err := m.storage.GetChatUser(uc, chatID, uc.Sender().ID)
	if err != nil {
		return fmt.Errorf("failed to get user: %w", err)
	}

	uc.SetLoggerUser(user)

	if user.Status != models.UserStatusJustJoined {
		uc.L().Warnf("user status is not just joined, ignoring")
		if err := uc.TC().Send(
			fmt.Sprintf("You have an unexpected status `%s`", user.Status),
			telebot.ModeMarkdownV2,
		); err != nil {
			uc.L().Errorf("failed to send message: %v", err)
		}
		return nil
	}

	url, err := authutil.GetCTFTimeOAuthURL(user.ID, user.ChatID, m.config)
	if err != nil {
		return fmt.Errorf("getting oauth url: %w", err)
	}

	text := "Follow the link below to log in with CTFTime"
	markup := &telebot.ReplyMarkup{}
	markup.Inline(markup.Row(markup.URL("Log in with CTFTime", url)))

	if err := uc.TC().Send(text, markup); err != nil {
		return fmt.Errorf("sending login message: %w", err)
	}

	if err := m.removeGreetingsForUser(uc, user); err != nil {
		uc.L().Errorf("failed to remove greetings for user: %v", err)
	}

	return nil
}

func (m *Monitor) removeGreetingsForUser(ctx context.Context, user *models.User) error {
	msgs, err := m.storage.GetMessagesForUser(ctx, user.ID, user.ChatID, models.MessageTypeGreeting)
	if err != nil {
		return fmt.Errorf("getting greetings: %w", err)
	}

	var finalErr error
	for _, msg := range msgs {
		if err := m.bot.Delete(msg); err != nil {
			finalErr = errors.Join(finalErr, fmt.Errorf("deleting message %v: %w", msg, err))
		}
	}

	return finalErr
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
