package models

type GlobalState struct {
	ID           int `gorm:"primaryKey"`
	LastUpdateID int
}
