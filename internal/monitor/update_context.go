package monitor

import (
	"context"

	"github.com/C4T-BuT-S4D/shpaga/internal/models"
	"github.com/sirupsen/logrus"
	"gopkg.in/telebot.v4"
)

type UpdateContext struct {
	context.Context
	tc  telebot.Context
	log *logrus.Entry
}

func NewUpdateContext(c context.Context, tc telebot.Context) *UpdateContext {
	fields := logrus.Fields{
		"update.id": tc.Update().ID,
	}
	if tc.Chat() != nil {
		fields["chat.id"] = tc.Chat().ID
		fields["chat.type"] = tc.Chat().Type
	}
	if tc.Sender() != nil {
		fields["sender.id"] = tc.Sender().ID
		fields["sender.username"] = tc.Sender().Username
		fields["sender.first_name"] = tc.Sender().FirstName
		fields["sender.last_name"] = tc.Sender().LastName
	}

	return &UpdateContext{
		Context: c,
		tc:      tc,
		log:     logrus.WithFields(fields),
	}
}

func (uc *UpdateContext) L() *logrus.Entry {
	return uc.log
}

func (uc *UpdateContext) SetLoggerUser(user *models.User) {
	uc.log = uc.log.WithFields(logrus.Fields{
		"user.id":          user.ID,
		"user.telegram_id": user.TelegramID,
		"user.ctftime_id":  user.CTFTimeUserID,
		"user.status":      user.Status,
	})
}

func (uc *UpdateContext) TC() telebot.Context {
	return uc.tc
}

func (uc *UpdateContext) Bot() telebot.API {
	return uc.tc.Bot()
}

func (uc *UpdateContext) Message() *telebot.Message {
	return uc.tc.Message()
}

func (uc *UpdateContext) Chat() *telebot.Chat {
	return uc.tc.Chat()
}

func (uc *UpdateContext) Sender() *telebot.User {
	return uc.tc.Sender()
}
