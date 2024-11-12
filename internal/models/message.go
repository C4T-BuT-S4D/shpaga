package models

import (
	"fmt"
	"time"
)

type MessageType string

const (
	MessageTypeGreeting MessageType = "greeting"
)

type Message struct {
	ChatID    int64  `gorm:"primaryKey"`
	MessageID string `gorm:"primaryKey"`

	MessageType MessageType

	AssociatedUserID string `gorm:"index"`

	CreatedAt time.Time `gorm:"autoCreateTime;index"`
}

func (m *Message) MessageSig() (string, int64) {
	return m.MessageID, m.ChatID
}

func (m *Message) String() string {
	return fmt.Sprintf(
		"Message(%s, %d, %q, %s)",
		m.MessageID,
		m.ChatID,
		m.MessageType,
		m.AssociatedUserID,
	)
}
