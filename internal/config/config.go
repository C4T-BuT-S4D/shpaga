package config

import (
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

type Config struct {
	TelegramToken       string        `mapstructure:"telegram_token"`
	BotHandleTimeout    time.Duration `mapstructure:"bot_handle_timeout"`
	JoinLoginTimeout    time.Duration `mapstructure:"join_login_timeout"`
	CTFTimeClientID     string        `mapstructure:"ctftime_client_id"`
	CTFTimeClientSecret string        `mapstructure:"ctftime_client_secret"`
	CTFTimeOAuthHost    string        `mapstructure:"ctftime_oauth_host"`
	CTFTimeRedirectURL  string        `mapstructure:"ctftime_redirect_url"`

	PostgresDSN string `mapstructure:"postgres_dsn"`
}

func New() *Config {
	cfg := &Config{}
	if err := viper.Unmarshal(cfg); err != nil {
		logrus.Fatalf("unmarshalling config: %v", err)
	}
	return cfg
}

func SetupCommon() {
	viper.SetDefault("ctftime_oauth_host", "oauth.ctftime.org")
	viper.SetDefault("ctftime_redirect_url", "http://localhost:8080/oauth_callback")
	viper.SetEnvPrefix("SHPAGA")

	viper.MustBindEnv("telegram_token")
	viper.MustBindEnv("ctftime_client_id")
	viper.MustBindEnv("postgres_dsn")
	viper.AutomaticEnv()
}
