package main

import (
	"context"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/C4T-BuT-S4D/shpago/internal/config"
	"github.com/C4T-BuT-S4D/shpago/internal/logging"
	"github.com/C4T-BuT-S4D/shpago/internal/monitor"
	"github.com/C4T-BuT-S4D/shpago/internal/storage"
	"github.com/sirupsen/logrus"
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
		logrus.Fatalf("Failed to create bot: %v", err)
	}

	db, err := gorm.Open(postgres.Open(cfg.PostgresDSN), &gorm.Config{})
	if err != nil {
		logrus.Fatalf("Failed to connect to database: %v", err)
	}

	store := storage.New(db)
	if err := store.Migrate(); err != nil {
		logrus.Fatalf("Failed to migrate database: %v", err)
	}

	if err := db.Exec("TRUNCATE TABLE users").Error; err != nil {
		logrus.Fatalf("Failed to truncate users table: %v", err)
	}

	mon := monitor.New(cfg, store, bot)

	bot.Handle(telebot.OnText, mon.HandleMessage)
	bot.Handle(telebot.OnUserJoined, mon.HandleUserJoined)
	bot.Handle(telebot.OnUserLeft, mon.HandleChatLeft)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		bot.Start()
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		mon.RunCleaner(ctx)
	}()

	<-ctx.Done()

	bot.Stop()

	logrus.Info("waiting for services to finish")
	wg.Wait()
}

func setupConfig() {
	config.SetupCommon()
}
