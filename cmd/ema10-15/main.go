package main

import (
	"flag"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"ema10-15/internal/cronjobs"
	"ema10-15/internal/db"

	"ema10-15/internal/logger"

	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

type TickerPacket struct {
	FeedResponseCode byte
	MessageLength    uint16
	ExchangeSegment  byte
	SecurityID       uint32
	LastTradedPrice  float32
	LastTradeTime    uint32
}

func main() {
	logger.Init()

	// Parse command line flags
	immediate := flag.Bool("immediate", false, "Start processing immediately without cron schedule")
	flag.Parse()

	if err := godotenv.Load(); err != nil {
		logger.Log.WithError(err).Error("Error loading .env file")
	}

	dbDSN := os.Getenv("DB_DSN")

	if dbDSN == "" {
		logger.Log.Error("DB_DSN environment variable is not set")
	}
	if err := db.Initialize(dbDSN); err != nil {
		logger.Log.WithError(err).Error("Failed to initialize database")
	}
	defer db.Close()

	httpClient := &http.Client{
		Timeout: 30 * time.Second,
	}

	cronManager := cronjobs.NewCronManager(httpClient)

	logger.Log.Info("Starting scheduled processing...")
	cronManager.Start(*immediate)

	// Set up graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	// Wait for termination signal
	<-stop

	logger.Log.Info("Shutting down...")
	if !*immediate {
		cronManager.Stop()
	}
	logger.Log.Info("Application terminated")
}
