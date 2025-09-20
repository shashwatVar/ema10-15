package logger

import (
	"fmt"
	"os"

	"github.com/sirupsen/logrus"
)

var Log *Logger

type Logger struct {
	*logrus.Logger
}

func (l *Logger) ErrorWithReturn(format string, args ...interface{}) error {
	msg := fmt.Sprintf(format, args...)
	l.Error(msg)
	return fmt.Errorf(msg)
}

func (l *Logger) ErrorWithReturnWrap(err error, format string, args ...interface{}) error {
	msg := fmt.Sprintf(format, args...)
	l.WithError(err).Error(msg)
	return fmt.Errorf("%s: %w", msg, err)
}

func Init() {
	Log = &Logger{logrus.New()}
	Log.SetOutput(os.Stdout)
	Log.SetFormatter(&logrus.JSONFormatter{
		FieldMap: logrus.FieldMap{
			logrus.FieldKeyTime:  "timestamp",
			logrus.FieldKeyLevel: "level",
			logrus.FieldKeyMsg:   "message",
		},
	})
}
