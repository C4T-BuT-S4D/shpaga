package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/C4T-BuT-S4D/shpaga/internal/authutil"
	"github.com/C4T-BuT-S4D/shpaga/internal/config"
	"github.com/C4T-BuT-S4D/shpaga/internal/models"
	"github.com/C4T-BuT-S4D/shpaga/internal/storage"
	"github.com/go-resty/resty/v2"
	"github.com/labstack/echo/v4"
	"github.com/sirupsen/logrus"
	"gopkg.in/telebot.v4"
)

type Service struct {
	config  *config.Config
	storage *storage.Storage
	bot     telebot.API

	client *resty.Client
}

func NewService(cfg *config.Config, storage *storage.Storage, bot telebot.API) *Service {
	return &Service{
		config:  cfg,
		storage: storage,
		bot:     bot,
		client:  resty.New().SetBaseURL(fmt.Sprintf("https://%s", cfg.CTFTimeOAuthHost)),
	}
}

func (s *Service) HandleOAuthCallback() echo.HandlerFunc {
	return func(c echo.Context) error {
		code := c.QueryParam("code")
		if code == "" {
			return c.JSON(http.StatusBadRequest, echo.Map{"error": "code is required"})
		}

		stateRaw := c.QueryParam("state")
		if stateRaw == "" {
			return c.JSON(http.StatusBadRequest, echo.Map{"error": "state is required"})
		}

		state, err := authutil.StateFromString(stateRaw)
		if err != nil {
			logrus.Error("failed to unmarshal state: %v", err)
			return c.JSON(http.StatusBadRequest, echo.Map{"error": "failed to unmarshal state"})
		}

		logrus.Infof("Received oauth callback for %v", state)

		token, err := s.getOAuthToken(code)
		if err != nil {
			logrus.Errorf("failed to get oauth token: %v", err)
			return c.JSON(http.StatusInternalServerError, echo.Map{"error": "failed to get oauth token"})
		}

		logrus.Infof("Received oauth token for %v", state)

		ctftimeUserID, err := s.getUser(token)
		if err != nil {
			logrus.Errorf("failed to get ctftime user id: %v", err)
			return c.JSON(http.StatusInternalServerError, echo.Map{"error": "failed to get user"})
		}

		logrus.Infof("Received ctftime user id %d for user %s in chat %d", ctftimeUserID, state.UserID, state.ChatID)

		if err := s.storage.OnUserAuthorized(c.Request().Context(), state.UserID, ctftimeUserID); err != nil {
			logrus.Errorf("failed to set oauth token: %v", err)
			return c.JSON(http.StatusInternalServerError, echo.Map{"error": "failed to set oauth token"})
		}

		logrus.Infof("Successfully set oauth token for %v", state)

		if err := s.removeGreetings(c.Request().Context(), state); err != nil {
			logrus.Errorf("failed to remove greetings: %v", err)
		}

		return c.String(http.StatusOK, "Successfully authorized, you can close this page.")
	}
}

func (s *Service) getOAuthToken(code string) (string, error) {
	type oauthTokenResponse struct {
		AccessToken string `json:"access_token"`
	}

	resp, err := s.client.R().
		SetQueryParams(map[string]string{
			"client_id":     s.config.CTFTimeClientID,
			"client_secret": s.config.CTFTimeClientSecret,
			"code":          code,
			"grant_type":    "authorization_code",
			"redirect_uri":  s.config.CTFTimeRedirectURL,
		}).
		SetResult(&oauthTokenResponse{}).
		Post("/token")
	if err != nil {
		return "", fmt.Errorf("sending request: %w", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d %s", resp.StatusCode(), string(resp.Body()))
	}

	return resp.Result().(*oauthTokenResponse).AccessToken, nil
}

func (s *Service) getUser(token string) (int64, error) {
	type oauthUserResponse struct {
		ID int64 `json:"id"`
	}

	resp, err := s.client.R().
		SetHeader("Authorization", fmt.Sprintf("Bearer %s", token)).
		SetResult(&oauthUserResponse{}).
		Get("/user")
	if err != nil {
		return 0, fmt.Errorf("sending request: %w", err)
	}

	return resp.Result().(*oauthUserResponse).ID, nil
}

func (s *Service) removeGreetings(ctx context.Context, state *authutil.State) error {
	msgs, err := s.storage.GetMessagesForUser(ctx, state.UserID, state.ChatID, models.MessageTypeGreeting)
	if err != nil {
		return fmt.Errorf("getting messages: %w", err)
	}

	var finalErr error
	for _, msg := range msgs {
		if err := s.bot.Delete(msg); err != nil {
			finalErr = errors.Join(finalErr, fmt.Errorf("removing message %v", msg))
		}
	}

	return nil
}
