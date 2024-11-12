package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/C4T-BuT-S4D/shpago/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Storage struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Storage {
	return &Storage{db: db}
}

func (s *Storage) Migrate() error {
	if err := s.db.AutoMigrate(&models.User{}, &models.Message{}); err != nil {
		return fmt.Errorf("migrating database: %w", err)
	}
	return nil
}

func (s *Storage) GetUser(ctx context.Context, userID string) (*models.User, error) {
	var user models.User
	if err := s.db.WithContext(ctx).Where("id = ?", userID).First(&user).Error; err != nil {
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
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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
	if err := s.db.
		WithContext(ctx).
		Debug().
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

func (s *Storage) OnUserBanned(ctx context.Context, userID string) error {
	if err := s.db.
		WithContext(ctx).
		Debug().
		Model(&models.User{}).
		Where("id = ?", userID).
		Updates(map[string]any{
			"status": models.UserStatusBanned,
		}).
		Error; err != nil {
		return fmt.Errorf("updating user: %w", err)
	}

	return nil
}

func (s *Storage) AddMessage(ctx context.Context, msg *models.Message) error {
	if err := s.db.Create(msg).Error; err != nil {
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
	if err := s.db.
		WithContext(ctx).
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
	if err := s.db.
		WithContext(ctx).
		Where("created_at < ?", olderThan).
		Limit(100).
		Find(&result).
		Error; err != nil {
		return nil, fmt.Errorf("getting messages: %w", err)
	}
	return result, nil
}

func (s *Storage) DeleteMessages(ctx context.Context, messages []*models.Message) error {
	if err := s.db.WithContext(ctx).Delete(messages).Error; err != nil {
		return fmt.Errorf("deleting messages: %w", err)
	}
	return nil
}
