package monitor

import (
	"context"

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
		"update_id": tc.Update().ID,
	}
	if tc.Chat() != nil {
		fields["chat_id"] = tc.Chat().ID
		fields["chat_type"] = tc.Chat().Type
	}
	if tc.Sender() != nil {
		fields["sender_id"] = tc.Sender().ID
		fields["sender_username"] = tc.Sender().Username
		fields["sender_first_name"] = tc.Sender().FirstName
		fields["sender_last_name"] = tc.Sender().LastName
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
