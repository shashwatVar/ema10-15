package utils

import (
	"math"
	"time"

	"ema10-15/internal/logger"

	"github.com/sirupsen/logrus"
)

type RetryConfig struct {
	MaxRetries int
	BaseDelay  time.Duration
}

// RetryWithExponentialBackoff is a generic function that retries the provided function
// with exponential backoff if it returns an error
func RetryWithExponentialBackoff(fn func() error, config RetryConfig) error {
	var err error
	for attempt := 0; attempt < config.MaxRetries; attempt++ {
		err = fn()
		if err == nil {
			return nil
		}

		if attempt < config.MaxRetries-1 {
			delay := time.Duration(math.Pow(2, float64(attempt))) * config.BaseDelay
			logger.Log.WithFields(logrus.Fields{
				"delay": delay,
			}).Info("Retrying in")
			time.Sleep(delay)
		}
	}
	return logger.Log.ErrorWithReturnWrap(err, "operation failed after %d attempts", config.MaxRetries)
}
