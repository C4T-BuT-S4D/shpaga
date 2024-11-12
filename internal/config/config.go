package config

import (
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

type Config struct {
	TelegramToken       string `mapstructure:"telegram_token"`
	CTFTimeClientID     string `mapstructure:"ctftime_client_id"`
	CTFTimeClientSecret string `mapstructure:"ctftime_client_secret"`
	CTFTimeOAuthHost    string `mapstructure:"ctftime_oauth_host"`
	CTFTimeRedirectURL  string `mapstructure:"ctftime_redirect_url"`

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

	viper.BindEnv("telegram_token")
	viper.BindEnv("ctftime_client_id")
	viper.BindEnv("postgres_dsn")
	viper.AutomaticEnv()
}
