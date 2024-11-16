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

	chatState, err := m.storage.GetOrCreateChatState(ctx, c.Chat().ID, c.Chat().Type)
	if err != nil {
		logrus.Errorf("failed to get or create chat state for update %v: %v", c.Update(), err)
		return nil
	}

	uc := NewUpdateContext(ctx, c, chatState)

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
	case c.Callback() != nil:
		uc.L().Debugf("received callback query update: callback=%v", c.Callback())
	default:
		uc.L().Warnf("received unknown update %+v", c.Update())
		return nil
	}

	if !slices.Contains(allowedChatTypes, c.Chat().Type) {
		uc.L().Debugf("ignoring update from unknown chat type")
		return nil
	}

	chatMemberJoined := false
	chatMemberLeft := false

	if member := c.ChatMember(); member != nil && uc.ChatState().IsGroup() {
		chatMemberJoined = isLeftStatus(member.OldChatMember) && isMemberStatus(member.NewChatMember)
		chatMemberLeft = isMemberStatus(member.OldChatMember) && isLeftStatus(member.NewChatMember)

		if chatMemberJoined == chatMemberLeft {
			uc.L().Warnf("unexpected member status, old=%v, new=%v", member.OldChatMember, member.NewChatMember)
		}
	}

	if uc.ChatState().IsGroup() {
		uc.L().Debugf("bot chat member status: %v", chatState.Member)
		if uc.ChatState().Member != nil && uc.ChatState().Member.Role != telebot.Administrator {
			uc.L().Warnf("bot is not an admin, skipping update (role: %v)", uc.ChatState().Member.Role)
			return nil
		}
	}

	switch {
	case c.Chat().Type == telebot.ChatPrivate:
		if err := m.HandlePrivateMessage(uc); err != nil {
			uc.L().Errorf("failed to handle private message: %v", err)
		}
	case uc.ChatState().IsGroup() && c.Message() != nil && c.Message().UserJoined != nil:
		if err := uc.Bot().Delete(uc.Message()); err != nil {
			uc.L().Errorf("failed to delete join message: %v", err)
		}
	case uc.ChatState().IsGroup() && c.Message() != nil && c.Message().UserLeft != nil:
		if err := uc.Bot().Delete(uc.Message()); err != nil {
			uc.L().Errorf("failed to delete left message: %v", err)
		}
	case chatMemberLeft:
		if err := m.HandleMemberLeft(uc); err != nil {
			uc.L().Errorf("failed to handle chat member left: %v", err)
		}
	case chatMemberJoined:
		if err := m.HandleNewMember(uc); err != nil {
			uc.L().Errorf("failed to handle chat member join: %v", err)
		}
	case uc.ChatState().IsGroup() && c.Callback() != nil && CallbackActionNewMemberAccept.DataMatches(c.Callback().Data):
		if err := m.HandleNewMemberCallbackAction(uc, CallbackActionNewMemberAccept); err != nil {
			uc.L().Errorf("failed to handle new member accept: %v", err)
		}
	case uc.ChatState().IsGroup() && c.Callback() != nil && CallbackActionNewMemberKick.DataMatches(c.Callback().Data):
		if err := m.HandleNewMemberCallbackAction(uc, CallbackActionNewMemberKick); err != nil {
			uc.L().Errorf("failed to handle new member kick: %v", err)
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
		markup.Inline(
			markup.Row(
				markup.URL("Log in with CTFTime", url),
			),
			markup.Row(
				markup.Data(
					"✅ Accept (admin only)",
					CallbackActionNewMemberAccept.String(),
					strconv.FormatInt(uc.Sender().ID, 10),
				),
				markup.Data(
					"❌ Kick (admin only)",
					CallbackActionNewMemberKick.String(),
					strconv.FormatInt(uc.Sender().ID, 10),
				),
			),
		)
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

func (m *Monitor) HandleNewMemberCallbackAction(uc *UpdateContext, action CallbackAction) error {
	uc.L().Infof("handling new member callback action %v, data %v", action, uc.Callback().Data)

	if err := m.checkSenderAdmin(uc); err != nil {
		uc.L().Warnf("sender is not an admin: %v", err)
		if err := uc.TC().Respond(&telebot.CallbackResponse{
			Text: fmt.Sprintf("you are not an admin: %v", err),
		}); err != nil {
			uc.L().Errorf("failed to respond: %v", err)
		}
		return nil
	}

	tokens := strings.SplitN(uc.Callback().Data, "|", 2)
	if len(tokens) != 2 {
		uc.L().Warnf("unexpected callback data: %v", uc.Callback().Data)
		if err := uc.TC().Respond(&telebot.CallbackResponse{Text: "bad callback data"}); err != nil {
			uc.L().Errorf("failed to respond: %v", err)
		}
		return nil
	}

	targetUserID, err := strconv.ParseInt(tokens[1], 10, 64)
	if err != nil {
		uc.L().Warnf("failed to parse target user id: %v", err)
		if err := uc.TC().Respond(&telebot.CallbackResponse{Text: "bad user id"}); err != nil {
			uc.L().Errorf("failed to respond: %v", err)
		}
		return nil
	}

	user, err := m.storage.GetChatUser(uc, uc.Chat().ID, targetUserID)
	if err != nil {
		return fmt.Errorf("getting user: %w", err)
	}

	if user.Status != models.UserStatusJustJoined {
		uc.L().Warnf("user status is not just joined, ignoring")
		if err := uc.TC().Respond(&telebot.CallbackResponse{Text: "user status is not just joined"}); err != nil {
			uc.L().Errorf("failed to respond: %v", err)
		}
		return nil
	}

	switch action {
	case CallbackActionNewMemberAccept:
		if err := m.storage.SetUserStatus(uc, user.ID, models.UserStatusActive); err != nil {
			return fmt.Errorf("setting user status: %w", err)
		}

	case CallbackActionNewMemberKick:
		if err := m.bot.Unban(uc.Chat(), &telebot.User{ID: targetUserID}); err != nil {
			return fmt.Errorf("kicking user: %w", err)
		}
		if err := m.storage.SetUserStatus(uc, user.ID, models.UserStatusKicked); err != nil {
			return fmt.Errorf("setting user status: %w", err)
		}
	}

	if err := m.removeGreetingsForUser(uc, user); err != nil {
		return fmt.Errorf("removing greetings for user: %w", err)
	}

	return nil
}

func (m *Monitor) removeGreetingsForUser(uc *UpdateContext, user *models.User) error {
	msgs, err := m.storage.GetMessagesForUser(uc, user.ID, user.ChatID, models.MessageTypeGreeting)
	if err != nil {
		return fmt.Errorf("getting greetings: %w", err)
	}

	for _, msg := range msgs {
		m.deleteMessageChecked(msg, uc.L())
	}

	return nil
}

func (m *Monitor) RunCleaner(ctx context.Context) {
	logger := logrus.WithField("component", "monitor_cleaner")

	run := func() {
		msgs, err := m.storage.GetMessagesOlderThan(
			ctx,
			time.Now().Add(-m.config.JoinLoginTimeout),
		)
		if err != nil {
			logger.Errorf("failed to get messages: %v", err)
			return
		}
		if len(msgs) == 0 {
			return
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

					if err := m.storage.SetUserStatus(ctx, user.ID, models.UserStatusKicked); err != nil {
						logger.Errorf("failed to update user to kicked %v: %v", user, err)
					}
				}
			}

			m.deleteMessageChecked(msg, logger)
		}

		if err := m.storage.DeleteMessages(ctx, msgs); err != nil {
			logger.Errorf("failed to delete messages: %v", err)
		}
	}

	t := time.NewTicker(m.config.CleanerInterval)
	defer t.Stop()

	run()
	for {
		select {
		case <-t.C:
			run()
		case <-ctx.Done():
			return
		}
	}
}

func (m *Monitor) RunUpdateChatAdmins(ctx context.Context) {
	logger := logrus.WithField("component", "monitor_chat_admins")

	run := func() {
		chats, err := m.storage.GetChatStates(ctx)
		if err != nil {
			logger.Errorf("failed to get chat states: %v", err)
			return
		}

		me := m.bot.(*telebot.Bot).Me

		for _, chat := range chats {
			member, err := m.bot.ChatMemberOf(&telebot.Chat{ID: chat.ChatID}, &telebot.User{ID: me.ID})
			if err != nil {
				logger.Errorf("failed to get self chat member for chat %v: %v", chat, err)
				continue
			}
			logger.Debugf("chat %v has self member role: %v", chat.ChatID, member.Role)
			chat.Member = member

			admins, err := m.bot.AdminsOf(&telebot.Chat{ID: chat.ChatID})
			if err != nil {
				logger.Errorf("failed to get chat admins for chat %v: %v", chat, err)
				continue
			}
			logger.Debugf("chat %v has %d admins", chat, len(admins))
			chat.Admins = admins

			if err := m.storage.UpdateChatState(ctx, chat); err != nil {
				logger.Errorf("failed to update chat state for chat %v: %v", chat, err)
			}
		}
	}

	t := time.NewTicker(m.config.ChatSyncerInterval)
	defer t.Stop()

	run()
	for {
		select {
		case <-t.C:
			run()
		case <-ctx.Done():
			return
		}
	}
}

func (m *Monitor) deleteMessageChecked(msg telebot.Editable, logger *logrus.Entry) {
	if err := m.bot.Delete(msg); err != nil {
		if errors.Is(err, telebot.ErrNotFoundToDelete) {
			logger.Debugf("message %v was already deleted", msg)
		} else {
			logger.Errorf("failed to delete message %v: %v", msg, err)
		}
	}
}

func (m *Monitor) checkSenderAdmin(uc *UpdateContext) error {
	if len(uc.ChatState().Admins) == 0 {
		return fmt.Errorf("no admins synced for chat")
	}

	if !slices.ContainsFunc(uc.ChatState().Admins, func(u telebot.ChatMember) bool {
		return u.User.ID == uc.Sender().ID
	}) {
		return fmt.Errorf("user is not one of %d known admins", len(uc.ChatState().Admins))
	}

	return nil
}

func isMemberStatus(member *telebot.ChatMember) bool {
	return member != nil && member.Role == telebot.Member
}

func isLeftStatus(member *telebot.ChatMember) bool {
	return member == nil || member.Role == telebot.Left || member.Role == telebot.Kicked
}
