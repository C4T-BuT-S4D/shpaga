package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/C4T-BuT-S4D/shpaga/internal/models"
	"github.com/google/uuid"
	"gopkg.in/telebot.v4"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Storage struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Storage {
	return &Storage{db: db}
}

func (s *Storage) Migrate(ctx context.Context) error {
	if err := s.getDB(ctx).AutoMigrate(models.All...); err != nil {
		return fmt.Errorf("migrating database: %w", err)
	}
	return nil
}

func (s *Storage) GetOrCreateGlobalState(ctx context.Context) (*models.GlobalState, error) {
	var res models.GlobalState
	if err := s.getDB(ctx).Transaction(func(tx *gorm.DB) error {
		// Optimistic check if global state exists
		if err := tx.First(&res).Error; err == nil {
			return nil
		}

		if err := tx.
			Clauses(clause.OnConflict{DoNothing: true}).
			Create(&models.GlobalState{ID: 1}).
			Error; err != nil {
			return fmt.Errorf("creating global state: %w", err)
		}

		if err := tx.First(&res).Error; err != nil {
			return fmt.Errorf("getting global state: %w", err)
		}

		return nil
	}); err != nil {
		return nil, fmt.Errorf("in tx: %w", err)
	}

	return &res, nil
}

func (s *Storage) UpdateLastUpdate(ctx context.Context, updateID int) error {
	if err := s.
		getDB(ctx).
		Model(&models.GlobalState{}).
		Where("id = 1").
		Update("last_update_id", updateID).
		Error; err != nil {
		return fmt.Errorf("updating last update: %w", err)
	}
	return nil
}

func (s *Storage) GetOrCreateChatState(ctx context.Context, chatID int64, chatType telebot.ChatType) (*models.ChatState, error) {
	var res models.ChatState
	if err := s.getDB(ctx).Transaction(func(tx *gorm.DB) error {
		// Optimistic check if chat state exists
		if err := tx.Where("chat_id = ?", chatID).First(&res).Error; err == nil {
			return nil
		}

		if err := tx.
			Clauses(clause.OnConflict{DoNothing: true}).
			Create(&models.ChatState{
				ChatID:   chatID,
				ChatType: chatType,
			}).
			Error; err != nil {
			return fmt.Errorf("creating chat state: %w", err)
		}

		if err := tx.Where("chat_id = ?", chatID).First(&res).Error; err != nil {
			return fmt.Errorf("getting chat state: %w", err)
		}

		return nil
	}); err != nil {
		return nil, fmt.Errorf("in tx: %w", err)
	}

	return &res, nil
}

func (s *Storage) GetChatStates(ctx context.Context) ([]*models.ChatState, error) {
	var res []*models.ChatState
	if err := s.getDB(ctx).Find(&res).Error; err != nil {
		return nil, fmt.Errorf("getting chat states: %w", err)
	}
	return res, nil
}

func (s *Storage) UpdateChatState(ctx context.Context, chatState *models.ChatState) error {
	if err := s.getDB(ctx).Save(chatState).Error; err != nil {
		return fmt.Errorf("updating chat state: %w", err)
	}
	return nil
}

func (s *Storage) GetUser(ctx context.Context, userID string) (*models.User, error) {
	var user models.User
	if err := s.getDB(ctx).Where("id = ?", userID).First(&user).Error; err != nil {
		return nil, fmt.Errorf("getting user: %w", err)
	}
	return &user, nil
}

func (s *Storage) GetChatUser(ctx context.Context, chatID, telegramID int64) (*models.User, error) {
	var user models.User
	if err := s.
		getDB(ctx).
		Where("chat_id = ? AND telegram_id = ?", chatID, telegramID).
		First(&user).
		Error; err != nil {
		return nil, fmt.Errorf("getting user: %w", err)
	}
	return &user, nil
}

func (s *Storage) GetOrCreateUser(ctx context.Context, chatID, telegramID int64, defaultStatus models.UserStatus) (*models.User, error) {
	userToCreate := &models.User{
		ID:         uuid.New().String(),
		ChatID:     chatID,
		TelegramID: telegramID,
		Status:     defaultStatus,
	}

	var user models.User
	if err := s.getDB(ctx).Transaction(func(tx *gorm.DB) error {
		// Optimistic check if user exists
		if err := tx.
			Where("chat_id = ? AND telegram_id = ?", chatID, telegramID).
			First(&user).
			Error; err == nil {
			return nil
		}

		if err := tx.
			Clauses(clause.OnConflict{
				Columns: []clause.Column{
					{Name: "chat_id"},
					{Name: "telegram_id"},
				},
				DoNothing: true,
			}).
			Create(userToCreate).
			Error; err != nil {
			return fmt.Errorf("creating user: %w", err)
		}

		if err := tx.
			Where("chat_id = ? AND telegram_id = ?", chatID, telegramID).
			First(&user).
			Error; err != nil {
			return fmt.Errorf("getting user: %w", err)
		}

		return nil
	}); err != nil {
		return nil, fmt.Errorf("in tx: %w", err)
	}

	return &user, nil
}

func (s *Storage) OnUserAuthorized(ctx context.Context, userID string, ctftimeUserID int64) error {
	if err := s.
		getDB(ctx).
		Model(&models.User{}).
		Where("id = ?", userID).
		Updates(map[string]any{
			"ctftime_user_id": ctftimeUserID,
			"status":          models.UserStatusActive,
		}).
		Error; err != nil {
		return fmt.Errorf("updating user: %w", err)
	}

	return nil
}

func (s *Storage) SetUserStatus(ctx context.Context, userID string, status models.UserStatus) error {
	if err := s.
		getDB(ctx).
		Model(&models.User{}).
		Where("id = ?", userID).
		Updates(map[string]any{
			"status": status,
		}).
		Error; err != nil {
		return fmt.Errorf("updating user: %w", err)
	}

	return nil
}

func (s *Storage) AddMessage(ctx context.Context, msg *models.Message) error {
	if err := s.getDB(ctx).Create(msg).Error; err != nil {
		return fmt.Errorf("creating message: %w", err)
	}
	return nil
}

func (s *Storage) GetMessagesForUser(
	ctx context.Context,
	userID string,
	chatID int64,
	messageType models.MessageType,
) ([]*models.Message, error) {
	var result []*models.Message
	if err := s.
		getDB(ctx).
		Where(
			"associated_user_id = ? AND chat_id = ? AND message_type = ?",
			userID,
			chatID,
			messageType,
		).
		Limit(100).
		Find(&result).
		Error; err != nil {
		return nil, fmt.Errorf("getting message: %w", err)
	}

	return result, nil
}

func (s *Storage) GetMessagesOlderThan(ctx context.Context, olderThan time.Time) ([]*models.Message, error) {
	var result []*models.Message
	if err := s.
		getDB(ctx).
		Where("created_at < ?", olderThan).
		Limit(100).
		Find(&result).
		Error; err != nil {
		return nil, fmt.Errorf("getting messages: %w", err)
	}
	return result, nil
}

func (s *Storage) DeleteMessages(ctx context.Context, messages []*models.Message) error {
	if err := s.getDB(ctx).Delete(messages).Error; err != nil {
		return fmt.Errorf("deleting messages: %w", err)
	}
	return nil
}

func (s *Storage) getDB(ctx context.Context) *gorm.DB {
	return s.db.WithContext(ctx)
}
