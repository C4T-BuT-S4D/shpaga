package logging

import (
	"fmt"
	"path/filepath"
	"runtime"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

func Init() {
	logrus.SetFormatter(&logrus.TextFormatter{
		ForceColors:            true,
		FullTimestamp:          true,
		TimestampFormat:        "2006-01-02 15:04:05",
		DisableLevelTruncation: false,
		CallerPrettyfier: func(f *runtime.Frame) (string, string) {
			filename := filepath.Base(f.File)
			return "", fmt.Sprintf(" %s:%d", filename, f.Line)
		},
	})
	logrus.SetReportCaller(true)

	switch {
	case viper.GetBool("debug") || viper.GetBool("verbose"):
		logrus.SetLevel(logrus.DebugLevel)
	case viper.GetString("log-level") != "":
		level, err := logrus.ParseLevel(viper.GetString("log-level"))
		if err != nil {
			logrus.Fatalf("parsing log level: %v", err)
		}
		logrus.SetLevel(level)
	default:
		logrus.SetLevel(logrus.InfoLevel)
	}
}
