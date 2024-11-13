package main

import (
	"context"
	"errors"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/C4T-BuT-S4D/shpaga/internal/api"
	"github.com/C4T-BuT-S4D/shpaga/internal/config"
	"github.com/C4T-BuT-S4D/shpaga/internal/logging"
	"github.com/C4T-BuT-S4D/shpaga/internal/storage"
	"github.com/labstack/echo/v4"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"gopkg.in/telebot.v4"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	setupConfig()
	logging.Init()

	cfg := config.New()
	logrus.Debugf("config: %+v", cfg)

	bot, err := telebot.NewBot(telebot.Settings{
		Token: cfg.TelegramToken,
		Poller: &telebot.LongPoller{
			Timeout: 10 * time.Second,
		},
	})
	if err != nil {
		logrus.Fatalf("failed to create bot: %v", err)
	}

	db, err := gorm.Open(postgres.Open(cfg.PostgresDSN), &gorm.Config{})
	if err != nil {
		logrus.Fatalf("failed to connect to database: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	initCtx, initCancel := context.WithTimeout(ctx, 10*time.Second)
	defer initCancel()

	store := storage.New(db)
	if err := store.Migrate(initCtx); err != nil {
		logrus.Fatalf("failed to migrate database: %v", err)
	}

	service := api.NewService(cfg, store, bot)
	e := echo.New()
	e.GET("/oauth_callback", service.HandleOAuthCallback())

	go func() {
		if err := e.Start(":8080"); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logrus.Fatalf("failed to start server: %v", err)
		}
	}()

	<-ctx.Done()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	logrus.Info("shutting down server")
	if err := e.Shutdown(shutdownCtx); err != nil {
		logrus.Errorf("failed to shutdown server: %v", err)
	}
}

func setupConfig() {
	viper.MustBindEnv("ctftime_client_secret")
	config.SetupCommon()
}
