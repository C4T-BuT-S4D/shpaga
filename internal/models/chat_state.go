package models

import (
	"time"

	"gopkg.in/telebot.v4"
)

type ChatState struct {
	ChatID    int64 `gorm:"primaryKey"`
	ChatType  telebot.ChatType
	CreatedAt time.Time `gorm:"autoCreateTime"`

	Member *telebot.ChatMember  `gorm:"type:jsonb;serializer:json"`
	Admins []telebot.ChatMember `gorm:"type:jsonb;serializer:json"`
}

func (s *ChatState) IsGroup() bool {
	return s.ChatType == telebot.ChatGroup || s.ChatType == telebot.ChatSuperGroup
}
