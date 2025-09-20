package cronjobs

import (
	"log"
	"net/http"
	"time"
	_ "time/tzdata"

	"ema10-15/internal/ohlcv"

	"github.com/robfig/cron/v3"
)

type CronManager struct {
	cron       *cron.Cron
	httpClient *http.Client
	processor  *ohlcv.Processor
}

func NewCronManager(httpClient *http.Client) *CronManager {
	location, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		log.Printf("Failed to load IST timezone: %v, falling back to UTC", err)
		location = time.UTC
	}

	return &CronManager{
		cron:       cron.New(cron.WithSeconds(), cron.WithLocation(location)),
		httpClient: httpClient,
	}
}

func (cm *CronManager) Start(immediate bool) {
	if immediate {
		cm.startProcessor()
		return
	} else {
		// Start the processor at 9:16 AM on weekdays except Thursday
		cm.cron.AddFunc("0 16 9 * * 1-3,5", cm.startProcessor)
		log.Println("Cron jobs for processor management started")
	}

	// Stop the processor at 2:45 PM on weekdays except Thursday
	cm.cron.AddFunc("0 45 14 * * 1-3,5", cm.stopProcessor)

	// Start the cron scheduler
	cm.cron.Start()
}

func (cm *CronManager) Stop() {
	cm.cron.Stop()
	log.Println("Cron jobs for processor management stopped")
}

func (cm *CronManager) startProcessor() {
	log.Println("Starting processor via cron job")
	cm.processor = ohlcv.NewProcessor(cm.httpClient)
	if err := cm.processor.Initialize(); err != nil {
		log.Printf("Failed to initialize processor: %v", err)
		return
	}
	cm.processor.StartPolling()
}

func (cm *CronManager) stopProcessor() {
	log.Println("Stopping processor via cron job")
	if cm.processor != nil {
		cm.processor.Stop()
	}
}
