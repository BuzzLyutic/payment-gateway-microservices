package logger

import (
	"os"

	"github.com/sirupsen/logrus"
)

var Log *logrus.Logger

func Init(level string) *logrus.Logger {
	Log = logrus.New()

	// Формат JSON для Graylog
	Log.SetFormatter(&logrus.JSONFormatter{
		TimestampFormat: "2006-01-02T15:04:05.000Z07:00",
		FieldMap: logrus.FieldMap{
			logrus.FieldKeyTime:  "timestamp",
			logrus.FieldKeyLevel: "level",
			logrus.FieldKeyMsg:   "message",
		},
	})

	Log.SetOutput(os.Stdout)

	// Установка уровня логирования
	logLevel, err := logrus.ParseLevel(level)
	if err != nil {
		logLevel = logrus.InfoLevel
	}
	Log.SetLevel(logLevel)

	return Log
}

// WithContext возвращает logger с контекстными полями
func WithContext(fields logrus.Fields) *logrus.Entry {
	if Log == nil {
		Init("info")
	}
	return Log.WithFields(fields)
}
