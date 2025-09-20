package ohlcv

import (
	"context"
	"ema10-15/internal/db"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"ema10-15/internal/logger"

	"github.com/shashwatVar/dhan-market-feed-go/pkg/marketfeed"
)

type OHLCVData struct {
	Open      []float64 `json:"open"`
	High      []float64 `json:"high"`
	Low       []float64 `json:"low"`
	Close     []float64 `json:"close"`
	Volume    []float64 `json:"volume"`
	Timestamp []float64 `json:"timestamp"`
}

type Processor struct {
	state            State
	httpClient       HTTPClient
	tickInterval     time.Duration
	stopChan         chan struct{}
	marketFeed       marketfeed.MarketFeedInterface
	intentionalClose bool
	abortNewTrades   bool
	reconnectChan    chan struct{}
}

type TradeInfo struct {
	StopLoss float64
	Target   float64
	Orders   []Order
}

type TriggerInfo struct {
	TriggerCandle Candle
}

type OptionObservation struct {
	SecurityID  int
	LotUnits    float64
	StrikePrice float64
	OptionType  string // "CE" or "PE"
	TradeInfo   *TradeInfo
	TriggerInfo *TriggerInfo
}

type Order struct {
	DhanClientID    string  `json:"dhanClientId"`
	TransactionType string  `json:"transactionType"`
	ExchangeSegment string  `json:"exchangeSegment"`
	ProductType     string  `json:"productType"`
	OrderType       string  `json:"orderType"`
	Validity        string  `json:"validity"`
	SecurityID      string  `json:"securityId"`
	Quantity        int     `json:"quantity"`
	Price           float64 `json:"price"`
	Token           string  `json:"token"`
	IsHedge         bool    `json:"isHedge,omitempty"`
	IsCompleted     bool    `json:"isCompleted,omitempty"`
}

type OrderParams struct {
	ATPOptionValue          float64
	ATPOptionType           string
	OppositeOptionType      string
	OrderTransactionType    string
	OppositeTransactionType string
	HedgeOptionValue        float64
	HedgeOptionType         string
	HedgeTransactionType    string
	PlaceHedgeOrdersFirst   bool
}

type OrderResponse struct {
	OrderID string `json:"orderId"`
	Status  string `json:"orderStatus"`
}

type OrderList struct {
	Quantity            int     `json:"quantity"`
	OMSErrorDescription string  `json:"omsErrorDescription"`
	ExchangeTime        string  `json:"exchangeTime"`
	CreateTime          string  `json:"createTime"`
	TransactionType     string  `json:"transactionType"`
	DRVStrikePrice      float64 `json:"drvStrikePrice"`
	DRVOptionType       string  `json:"drvOptionType"`
	SecurityID          string  `json:"securityId"`
	ClientID            string  `json:"clientId"`
}

type State struct {
	OptionChain     []*db.MarketData
	IndexSecurityID int
	IndexLTP        float64
	ObservedCE      *OptionObservation
	ObservedPE      *OptionObservation
	HedgeOrders     []Order
	HedgeGap        float64
	AdminToken      string
	mutex           sync.RWMutex
	OptionExpiry    string
	EmaData         []*db.OptionsEmaConfig
}

type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

func NewProcessor(httpClient HTTPClient) *Processor {
	return &Processor{
		httpClient:    httpClient,
		tickInterval:  5 * time.Minute,
		stopChan:      make(chan struct{}),
		reconnectChan: make(chan struct{}),
	}
}

func (p *Processor) Initialize() error {
	logger.Log.Info("Initializing Processor...")

	adminClient, err := db.FetchAdminClient()

	if err != nil {
		return logger.Log.ErrorWithReturnWrap(err, "Could not fetch admin client")
	}

	indexConfig, err := db.FetchIndexConfig()
	if err != nil {
		return logger.Log.ErrorWithReturnWrap(err, "Could not fetch index config")
	}

	options, err := db.FetchOptions(indexConfig.OptionExpiry)
	if err != nil {
		return logger.Log.ErrorWithReturnWrap(err, "Could not fetch options")
	}

	emaData, err := db.FetchAllEma10Data()
	if err != nil {
		return logger.Log.ErrorWithReturnWrap(err, "Could not fetch ema data")
	}

	p.state.mutex.Lock()
	defer p.state.mutex.Unlock()

	p.state.AdminToken = adminClient.Token
	p.state.IndexLTP = 0
	p.state.ObservedCE = &OptionObservation{}
	p.state.ObservedPE = &OptionObservation{}
	p.state.HedgeGap = 750
	p.state.IndexSecurityID = indexConfig.IndexSecurityID
	p.state.OptionChain = options
	p.state.OptionExpiry = indexConfig.OptionExpiry
	p.abortNewTrades = false
	p.state.EmaData = emaData

	// Initialize MarketFeed
	instruments := []marketfeed.Instrument{
		{
			ExchangeSegment: 0,
			SecurityID:      strconv.Itoa(p.state.IndexSecurityID),
			FeedType:        15,
		},
	}

	for _, option := range options {
		instruments = append(instruments, marketfeed.Instrument{
			ExchangeSegment: 2,
			SecurityID:      strconv.Itoa(option.SemSmstSecurityID),
			FeedType:        15,
		})
	}

	p.marketFeed = marketfeed.NewMarketFeed(
		adminClient.ClientID,
		adminClient.Token,
		instruments,
		p.handleMarketFeedConnect,
		p.handleMarketFeedMessage,
		p.handleMarketFeedClose,
	)

	logger.Log.WithFields(map[string]any{
		"indexSecurityID": p.state.IndexSecurityID,
	}).Info("Processor initialized")

	go p.reconnectionLoop()

	return nil
}

func (p *Processor) handleMarketFeedConnect() error {
	logger.Log.Info("Connected to market feed")
	return nil
}

func (p *Processor) handleMarketFeedMessage(data any) error {
	if tickerPacket, ok := data.(marketfeed.TickerPacket); ok {
		p.state.mutex.Lock()
		defer p.state.mutex.Unlock()

		// update index LTP
		if tickerPacket.SecurityID == uint32(p.state.IndexSecurityID) {
			p.state.IndexLTP = float64(tickerPacket.LastTradedPrice)
		}

		if int(tickerPacket.SecurityID) == p.state.ObservedCE.SecurityID && p.state.ObservedCE.TriggerInfo != nil {
			currentLtp := float64(tickerPacket.LastTradedPrice)

			logger.Log.WithFields(map[string]any{
				"option":     p.state.ObservedCE,
				"currentLtp": currentLtp,
			}).Info("Observed CE")

			if p.state.ObservedCE.TradeInfo != nil {
				// check if target / stoploss is met
				if currentLtp < p.state.ObservedCE.TradeInfo.Target {
					logger.Log.WithFields(map[string]any{
						"option":     p.state.ObservedCE,
						"currentLtp": currentLtp,
						"target":     p.state.ObservedCE.TradeInfo.Target,
						"stopLoss":   p.state.ObservedCE.TradeInfo.StopLoss,
					}).Info("Target met")

					return p.updateTradeInfoAndCloseTrade(currentLtp, p.state.ObservedCE)
				}

				if currentLtp > p.state.ObservedCE.TradeInfo.StopLoss {
					logger.Log.WithFields(map[string]any{
						"option":     p.state.ObservedCE,
						"currentLtp": currentLtp,
						"target":     p.state.ObservedCE.TradeInfo.Target,
						"stopLoss":   p.state.ObservedCE.TradeInfo.StopLoss,
					}).Info("Stoploss met")

					return p.updateTradeInfoAndCloseTrade(currentLtp, p.state.ObservedCE)
				}

				// if currentLtp is more than target - triggerCandle.high / 2 - set stoploss to triggerCandle.high
				if currentLtp < (p.state.ObservedCE.TriggerInfo.TriggerCandle.Low - (math.Abs(p.state.ObservedCE.TriggerInfo.TriggerCandle.Low-p.state.ObservedCE.TradeInfo.Target) / 2)) {
					p.state.ObservedCE.TradeInfo.StopLoss = p.state.ObservedCE.TriggerInfo.TriggerCandle.Low
					logger.Log.WithFields(map[string]any{
						"option":     p.state.ObservedCE,
						"currentLtp": currentLtp,
						"target":     p.state.ObservedCE.TradeInfo.Target,
						"stopLoss":   p.state.ObservedCE.TradeInfo.StopLoss,
					}).Info("StopLoss updated")

					return nil
				}
			} else {
				// check if trade condition is met
				if currentLtp < p.state.ObservedCE.TriggerInfo.TriggerCandle.Low {
					logger.Log.WithFields(map[string]any{
						"option":        p.state.ObservedCE,
						"currentLtp":    currentLtp,
						"triggerCandle": p.state.ObservedCE.TriggerInfo.TriggerCandle,
					}).Info("Trade Entry Condition met")

					return p.updateTradeInfoAndInitiateTrade(currentLtp, p.state.ObservedCE)
				}
			}
		}

		if int(tickerPacket.SecurityID) == p.state.ObservedPE.SecurityID && p.state.ObservedPE.TriggerInfo != nil {
			currentLtp := float64(tickerPacket.LastTradedPrice)

			logger.Log.WithFields(map[string]any{
				"option":     p.state.ObservedPE,
				"currentLtp": currentLtp,
			}).Info("Observed PE")

			if p.state.ObservedPE.TradeInfo != nil {
				// check if target / stoploss is met
				if currentLtp < p.state.ObservedPE.TradeInfo.Target {
					logger.Log.WithFields(map[string]any{
						"option":     p.state.ObservedPE,
						"currentLtp": currentLtp,
						"target":     p.state.ObservedPE.TradeInfo.Target,
						"stopLoss":   p.state.ObservedPE.TradeInfo.StopLoss,
					}).Info("Target met")

					return p.updateTradeInfoAndCloseTrade(currentLtp, p.state.ObservedPE)
				}

				if currentLtp > p.state.ObservedPE.TradeInfo.StopLoss {
					logger.Log.WithFields(map[string]any{
						"option":     p.state.ObservedPE,
						"currentLtp": currentLtp,
						"target":     p.state.ObservedPE.TradeInfo.Target,
						"stopLoss":   p.state.ObservedPE.TradeInfo.StopLoss,
					}).Info("Stoploss met")

					return p.updateTradeInfoAndCloseTrade(currentLtp, p.state.ObservedPE)
				}

				// if currentLtp is more than target - triggerCandle.high / 2 - set stoploss to triggerCandle.high
				if currentLtp < (p.state.ObservedPE.TriggerInfo.TriggerCandle.Low - (math.Abs(p.state.ObservedPE.TriggerInfo.TriggerCandle.Low-p.state.ObservedPE.TradeInfo.Target) / 2)) {
					p.state.ObservedPE.TradeInfo.StopLoss = p.state.ObservedPE.TriggerInfo.TriggerCandle.Low
					logger.Log.WithFields(map[string]any{
						"option":     p.state.ObservedPE,
						"currentLtp": currentLtp,
						"target":     p.state.ObservedPE.TradeInfo.Target,
						"stopLoss":   p.state.ObservedPE.TradeInfo.StopLoss,
					}).Info("StopLoss updated")

					return nil
				}
			} else {
				// check if trade condition is met
				if currentLtp < p.state.ObservedPE.TriggerInfo.TriggerCandle.Low {
					logger.Log.WithFields(map[string]any{
						"option":        p.state.ObservedPE,
						"currentLtp":    currentLtp,
						"triggerCandle": p.state.ObservedPE.TriggerInfo.TriggerCandle,
					}).Info("Trade Entry Condition met")

					return p.updateTradeInfoAndInitiateTrade(currentLtp, p.state.ObservedPE)
				}
			}
		}
	}
	return nil
}

func (p *Processor) handleMarketFeedClose(err error) error {
	if p.intentionalClose {
		logger.Log.Info("Market feed connection closed intentionally")
		p.intentionalClose = false // Reset the flag
		return nil
	}

	logger.Log.WithError(err).Error("Market feed connection closed unexpectedly")

	// Signal reconnection only if not intentionally closed
	select {
	case p.reconnectChan <- struct{}{}:
		logger.Log.Info("Queued reconnection attempt")
	default:
		logger.Log.Info("Reconnection already queued")
	}

	return nil
}

func (p *Processor) reconnectionLoop() {
	for {
		select {
		case <-p.stopChan:
			logger.Log.Info("Stopping reconnection loop due to processor shutdown")
			return
		case <-p.reconnectChan:
			// Check if we should attempt reconnection
			if !p.intentionalClose {
				logger.Log.Info("Attempting to reconnect to market feed...")
				err := p.createWSConnection()
				if err == nil {
					logger.Log.Info("Successfully reconnected to market feed")
				} else {
					logger.Log.WithError(err).Error("Failed to reconnect to market feed")
					// Schedule another reconnection attempt
					time.AfterFunc(2*time.Second, func() {
						if !p.intentionalClose {
							select {
							case p.reconnectChan <- struct{}{}:
							default:
							}
						}
					})
				}
			} else {
				logger.Log.Info("Skipping reconnection attempt due to intentional close")
			}
		}
	}
}

func (p *Processor) StartPolling() {
	logger.Log.Info("Starting OHLCV polling")
	if err := p.createWSConnection(); err != nil {
		logger.Log.ErrorWithReturnWrap(err, "error creating WebSocket connection")
	}

	for {
		select {
		case <-p.stopChan:
			logger.Log.Info("Stopping OHLCV processor")
			return
		default:
			nextTick := p.timeUntilNextFifteenMinMark()
			timer := time.NewTimer(nextTick)

			<-timer.C

			if err := p.checkTradeEntryConditions(); err != nil {
				logger.Log.WithError(err).Error("Error fetching and processing data")
			}

			timer.Stop()
		}
	}
}

func (p *Processor) timeUntilNextFifteenMinMark() time.Duration {
	now := time.Now()
	nextFifteenMin := now.Truncate(15 * time.Minute).Add(4 * time.Minute).Add(55 * time.Second)
	return nextFifteenMin.Sub(now)
}

func (p *Processor) Stop() {
	if p.state.ObservedCE.TradeInfo != nil || p.state.ObservedPE.TradeInfo != nil {
		logger.Log.ErrorWithReturn("Cannot stop processor with active trigger")
		p.abortNewTrades = true
	} else {
		close(p.stopChan)
		close(p.reconnectChan)
	}
}

func (p *Processor) checkTradeEntryConditions() error {
	// Start a timeout channel
	timeout := time.After(5*time.Second + time.Nanosecond)

	// Create a channel to signal when hedge logic is complete
	hedgeDone := make(chan error, 1)

	// Run hedge logic in a goroutine
	go func() {
		// if hedgeOrders.length > 0  and orders are not taken - close the hedge orders
		// if hedgeOrders.length > 0 and orders are taken - don't do anything
		// if hedgeOrders.length == 0 and trigger is formed - place hedgeOrders

		// check if orders are not taken and hedgeOrders.length > 0 - close the hedge orders
		if p.state.ObservedCE.TradeInfo == nil && p.state.ObservedPE.TradeInfo == nil && len(p.state.HedgeOrders) > 0 {
			hedgeOrders := []Order{}

			for _, order := range p.state.HedgeOrders {
				order.TransactionType = "SELL"
				hedgeOrders = append(hedgeOrders, order)
			}

			if err := p.placeOrders(hedgeOrders); err != nil {
				hedgeDone <- logger.Log.ErrorWithReturn("error placing orders: %v", err)
				return
			}

			p.state.HedgeOrders = []Order{}
		}

		if len(p.state.HedgeOrders) == 0 {
			if ceCandle, err := p.checkOptionTrigger("CE", p.state.ObservedCE); err != nil {
				hedgeDone <- logger.Log.ErrorWithReturnWrap(err, "error checking CE trigger")
				return
			} else if ceCandle != (Candle{}) {
				orders, err := p.calculateOrderDetails(p.state.ObservedCE, math.Abs(ceCandle.High-ceCandle.Low), true)
				if err != nil {
					hedgeDone <- logger.Log.ErrorWithReturn("error calculating order details: %v", err)
					return
				}

				if err := p.placeOrders(orders); err != nil {
					hedgeDone <- logger.Log.ErrorWithReturn("error placing orders: %v", err)
					return
				}

				p.state.HedgeOrders = append(p.state.HedgeOrders, orders...)
			}

			if peCandle, err := p.checkOptionTrigger("PE", p.state.ObservedPE); err != nil {
				hedgeDone <- logger.Log.ErrorWithReturnWrap(err, "error checking PE trigger")
				return
			} else if peCandle != (Candle{}) {
				orders, err := p.calculateOrderDetails(p.state.ObservedPE, math.Abs(peCandle.High-peCandle.Low), true)
				if err != nil {
					hedgeDone <- logger.Log.ErrorWithReturn("error calculating order details: %v", err)
					return
				}

				if err := p.placeOrders(orders); err != nil {
					hedgeDone <- logger.Log.ErrorWithReturn("error placing orders: %v", err)
					return
				}

				p.state.HedgeOrders = append(p.state.HedgeOrders, orders...)
			}
		}

		hedgeDone <- nil
	}()

	// First wait for the timeout
	<-timeout

	// Then wait for hedge logic to complete
	hedgeErr := <-hedgeDone
	if hedgeErr != nil {
		return hedgeErr
	}

	// fetch and process data
	if err := p.FetchAndProcessData(); err != nil {
		return logger.Log.ErrorWithReturn("error fetching and processing data: %v", err)
	}

	return nil
}

func (p *Processor) FetchAndProcessData() error {
	if p.state.ObservedCE.TradeInfo == nil && p.state.ObservedPE.TradeInfo == nil && !p.abortNewTrades {

		ceCandle, err := p.checkOptionTrigger("CE", p.state.ObservedCE)
		if err != nil {
			return logger.Log.ErrorWithReturnWrap(err, "error checking CE trigger")
		}
		if ceCandle != (Candle{}) {
			p.state.ObservedCE.TriggerInfo = &TriggerInfo{
				TriggerCandle: ceCandle,
			}

			return nil
		}

		peCandle, err := p.checkOptionTrigger("PE", p.state.ObservedPE)
		if err != nil {
			return logger.Log.ErrorWithReturnWrap(err, "error checking PE trigger")
		}
		if peCandle != (Candle{}) {
			p.state.ObservedPE.TriggerInfo = &TriggerInfo{
				TriggerCandle: peCandle,
			}

			return nil
		}
	}

	return nil
}

func (p *Processor) checkOptionTrigger(optionType string, observedOption *OptionObservation) (Candle, error) {
	indexOptionStrikePriceGap := 50
	strikePrice := 0.0

	if p.state.IndexLTP != 0 {
		strikePrice = math.Round(p.state.IndexLTP/float64(indexOptionStrikePriceGap)) * float64(indexOptionStrikePriceGap)
	}

	var optionData *db.MarketData
	for _, option := range p.state.OptionChain {
		if uint32(option.SemStrikePrice) == uint32(strikePrice) && option.SemOptionType == optionType {
			optionData = option
		}
	}

	if optionData == nil {
		logger.Log.WithFields(map[string]any{
			"strikePrice": strikePrice,
			"optionType":  optionType,
		}).Warn("No matching option found for the strike price")
		return Candle{}, nil
	}

	observedOption.SecurityID = optionData.SemSmstSecurityID
	observedOption.LotUnits = optionData.SemLotUnits
	observedOption.StrikePrice = strikePrice
	observedOption.OptionType = optionType
	observedOption.TradeInfo = nil
	observedOption.TriggerInfo = nil

	candles, currentCandle, err := p.fetchCandles(strconv.Itoa(optionData.SemSmstSecurityID), "NSE_FNO", "OPTIDX")
	if err != nil {
		return Candle{}, logger.Log.ErrorWithReturnWrap(err, "error fetching candles")
	}

	symbolOptionType := "PUT"
	if optionType == "CE" {
		symbolOptionType = "CALL"
	}

	initialEma := 0.0

	for _, emaData := range p.state.EmaData {
		if emaData.Strategy == "EMA10-15" && emaData.CustomSymbol == p.state.OptionExpiry+" "+strconv.Itoa(int(strikePrice))+" "+symbolOptionType {
			initialEma = emaData.Ema
		}
	}

	if initialEma == 0.0 {
		logger.Log.WithFields(map[string]any{
			"strikePrice": strikePrice,
			"optionType":  optionType,
		}).Warn("No matching ema data found for the strike price")
		return Candle{}, nil
	}

	ema10 := p.CalculateEMA10(candles, initialEma)

	logger.Log.WithFields(map[string]any{
		"ema10":         ema10,
		"currentCandle": currentCandle,
		"option":        observedOption,
	}).Info("ema10 calculated")

	// check for trigger formation using existing data
	triggerFormation := p.checkForTriggerFormation(currentCandle, ema10)

	if triggerFormation {
		logger.Log.WithFields(map[string]any{
			"strikePrice":   strikePrice,
			"currentCandle": currentCandle,
			"ema10":         ema10,
			"option":        observedOption,
		}).Info("Trigger formed")
		return currentCandle, nil
	}

	return Candle{}, nil
}

func (p *Processor) checkForTriggerFormation(currentCandle Candle, ema10 float64) bool {
	midPt := (currentCandle.High + currentCandle.Low) / 2

	if (currentCandle.Close < ema10) && (midPt > currentCandle.Close) && (currentCandle.Close > currentCandle.Open) {
		return true
	}

	return false
}

func (p *Processor) createWSConnection() error {
	logger.Log.Info("Creating WebSocket connection")
	ctx := context.Background()
	err := p.marketFeed.Connect(ctx)
	if err != nil {
		return logger.Log.ErrorWithReturnWrap(err, "failed to connect to market feed")
	}
	return nil
}
