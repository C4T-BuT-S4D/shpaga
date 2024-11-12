package models

import "time"

type UserStatus string

const (
	UserStatusJustJoined UserStatus = "just_joined"
	UserStatusBanned     UserStatus = "banned"
	UserStatusActive     UserStatus = "active"
)

type User struct {
	ID         string `gorm:"type:uuid;primaryKey"`
	ChatID     int64  `gorm:"uniqueIndex:idx_chat_telegram"`
	TelegramID int64  `gorm:"uniqueIndex:idx_chat_telegram"`

	CTFTimeUserID int64 `gorm:"column:ctftime_user_id"`

	CreatedAt time.Time `gorm:"autoCreateTime"`
	UpdatedAt time.Time `gorm:"autoUpdateTime"`
	Status    UserStatus
}
