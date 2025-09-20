package ohlcv

import (
	"bytes"
	"ema10-15/internal/db"
	"ema10-15/internal/logger"
	"ema10-15/internal/utils"
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

func (p *Processor) updateTradeInfoAndInitiateTrade(lastTradedPrice float64, optionObservation *OptionObservation) error {
	if optionObservation.TradeInfo != nil {
		return logger.Log.ErrorWithReturn("trade already initiated")
	}

	stopLossPoints := math.Abs(optionObservation.TriggerInfo.TriggerCandle.High - optionObservation.TriggerInfo.TriggerCandle.Low)

	multiplier := 1.9

	if stopLossPoints < 20 {
		multiplier = 2.0
	}

	optionObservation.TradeInfo = &TradeInfo{
		StopLoss: optionObservation.TriggerInfo.TriggerCandle.High + 1,
		Target:   optionObservation.TriggerInfo.TriggerCandle.Low - (optionObservation.TriggerInfo.TriggerCandle.High-optionObservation.TriggerInfo.TriggerCandle.Low)*multiplier,
		Orders:   []Order{},
	}

	logger.Log.WithFields(logrus.Fields{
		"optionObservation": optionObservation,
	}).Info("Option observation")

	// get all orders
	orders, err := p.calculateOrderDetails(optionObservation, stopLossPoints, false)

	if err != nil {
		return logger.Log.ErrorWithReturn("error calculating order details: %v", err)
	}

	// place normal orders
	if err := p.placeOrders(orders); err != nil {
		return logger.Log.ErrorWithReturn("error placing normal orders: %v", err)
	}

	optionObservation.TradeInfo.Orders = orders

	logger.Log.WithFields(logrus.Fields{
		"optionObservation": optionObservation,
		"stopLoss":          optionObservation.TradeInfo.StopLoss,
		"target":            optionObservation.TradeInfo.Target,
		"totalOrders":       len(orders),
	}).Info("Trade initiated")

	return nil
}

func (p *Processor) updateTradeInfoAndCloseTrade(lastTradedPrice float64, optionObservation *OptionObservation) error {
	if optionObservation.TradeInfo == nil {
		return logger.Log.ErrorWithReturn("trade not initiated")
	}

	// Store existing orders before clearing trade info
	existingOrders := optionObservation.TradeInfo.Orders

	// filter out hedge orders and normal orders
	normalOrders := []Order{}

	for _, order := range existingOrders {
		order.TransactionType = "BUY"
		normalOrders = append(normalOrders, order)
	}

	// Log the trade info before clearing
	stopLoss := optionObservation.TradeInfo.StopLoss
	target := optionObservation.TradeInfo.Target

	// Clear the trade info
	optionObservation.TriggerInfo = nil
	optionObservation.TradeInfo = nil

	logger.Log.WithFields(logrus.Fields{
		"optionObservation": optionObservation,
		"lastTradedPrice":   lastTradedPrice,
		"stopLoss":          stopLoss,
		"target":            target,
	}).Info("Placing exit orders")

	// Place exit orders
	if err := p.placeOrders(normalOrders); err != nil {
		return logger.Log.ErrorWithReturn("error placing exit orders: %v", err)
	}

	logger.Log.WithFields(logrus.Fields{
		"optionObservation": optionObservation,
		"stopLoss":          stopLoss,
		"target":            target,
		"totalOrders":       len(existingOrders),
	}).Info("Trade closed")

	return nil
}

func (p *Processor) calculateOrderDetails(optionObservation *OptionObservation, stopLossPoints float64, hedge bool) ([]Order, error) {

	allClients, err := db.FetchAllClients()
	if err != nil {
		return nil, logger.Log.ErrorWithReturn("error fetching all clients: %v", err)
	}

	// get the hedge option
	hedgeOption := &db.MarketData{}

	for _, option := range p.state.OptionChain {
		if optionObservation.OptionType == "CE" && int(option.SemStrikePrice) == int(optionObservation.StrikePrice+p.state.HedgeGap) && option.SemOptionType == "CE" {
			hedgeOption = option
		} else if optionObservation.OptionType == "PE" && int(option.SemStrikePrice) == int(optionObservation.StrikePrice-p.state.HedgeGap) && option.SemOptionType == "PE" {
			hedgeOption = option
		}
	}

	logger.Log.WithFields(logrus.Fields{
		"allClients": len(allClients),
	}).Info("All clients fetched")

	var orders []Order

	for _, client := range allClients {
		adjustedLots := 0

		if client.FixLots {
			adjustedLots = int(client.Lots)
		} else {
			adjustedLots = int(math.Floor(float64(client.AllowedLoss) / (stopLossPoints * float64(optionObservation.LotUnits))))

			if client.RoundUpLG && (float64(adjustedLots)*float64(optionObservation.LotUnits)*stopLossPoints < 0.9*float64(client.AllowedLoss)) {
				adjustedLots = adjustedLots + 1
			}
		}

		adjustedLots = int(math.Min(float64(adjustedLots), client.MaxLots))

		if adjustedLots <= 0 {
			logger.Log.WithFields(logrus.Fields{
				"clientID":       client.ClientID,
				"allowedLoss":    client.AllowedLoss,
				"stopLossPoints": stopLossPoints,
			}).Warn("Skipping client due to insufficient allowed loss")
			continue
		}

		if !hedge {
			orders = append(orders, Order{
				DhanClientID:    client.ClientID,
				TransactionType: "SELL",
				ExchangeSegment: "NSE_FNO",
				ProductType:     "INTRADAY",
				OrderType:       "MARKET",
				Validity:        "DAY",
				SecurityID:      strconv.Itoa(optionObservation.SecurityID),
				Quantity:        int(optionObservation.LotUnits) * adjustedLots,
				Price:           0,
				Token:           client.Token,
				IsHedge:         false,
			})
		}

		if hedge {
			orders = append(orders, Order{
				DhanClientID:    client.ClientID,
				TransactionType: "BUY",
				ExchangeSegment: "NSE_FNO",
				ProductType:     "INTRADAY",
				OrderType:       "MARKET",
				Validity:        "DAY",
				SecurityID:      strconv.Itoa(hedgeOption.SemSmstSecurityID),
				Quantity:        int(optionObservation.LotUnits) * adjustedLots,
				Price:           0,
				Token:           client.Token,
				IsHedge:         true,
			})
		}

	}

	return orders, nil
}

func (p *Processor) placeOrders(orders []Order) error {
	var wg sync.WaitGroup
	errChan := make(chan error, len(orders))

	for _, order := range orders {
		wg.Add(1)
		go func(o Order) {
			defer wg.Done()
			if err := p.placeOrderWithRetry(o); err != nil {
				logger.Log.WithFields(logrus.Fields{
					"clientID": o.DhanClientID,
					"error":    err,
				}).Error("Failed to place order for client")
				errChan <- err
			}
		}(order)
	}

	go func() {
		wg.Wait()
		close(errChan)
	}()

	// Collect errors but don't fail the entire process
	var errors []string
	for err := range errChan {
		errors = append(errors, err.Error())
	}

	if len(errors) > 0 {
		logger.Log.WithFields(logrus.Fields{
			"failedOrders": len(errors),
			"totalOrders":  len(orders),
			"errors":       strings.Join(errors, "; "),
		}).Warn("Some orders failed to place")
	}

	// Return nil to continue processing even if some orders failed
	return nil
}

func (p *Processor) placeOrderWithRetry(order Order) error {
	config := utils.RetryConfig{
		MaxRetries: 3,
		BaseDelay:  time.Second,
	}

	return utils.RetryWithExponentialBackoff(func() error {
		return p.placeOrder(order)
	}, config)
}

func (p *Processor) placeOrder(order Order) error {
	url := "https://api.dhan.co/v2/orders"

	// Create a new struct without the isHedge field
	orderPayload := struct {
		DhanClientID    string `json:"dhanClientId"`
		TransactionType string `json:"transactionType"`
		ExchangeSegment string `json:"exchangeSegment"`
		ProductType     string `json:"productType"`
		OrderType       string `json:"orderType"`
		Validity        string `json:"validity"`
		SecurityID      string `json:"securityId"`
		Quantity        int    `json:"quantity"`
		Price           int    `json:"price"`
	}{
		DhanClientID:    order.DhanClientID,
		TransactionType: order.TransactionType,
		ExchangeSegment: order.ExchangeSegment,
		ProductType:     order.ProductType,
		OrderType:       order.OrderType,
		Validity:        order.Validity,
		SecurityID:      order.SecurityID,
		Quantity:        order.Quantity,
		Price:           int(order.Price),
	}

	jsonPayload, err := json.Marshal(orderPayload)
	if err != nil {
		return logger.Log.ErrorWithReturn("error marshalling order: %v", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return logger.Log.ErrorWithReturn("error creating request: %v", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("access-token", order.Token)

	// log the request
	logger.Log.WithFields(logrus.Fields{
		"url":     req.URL.String(),
		"method":  req.Method,
		"headers": req.Header,
		"body":    string(jsonPayload),
	}).Info("Placing order")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return logger.Log.ErrorWithReturn("error making request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return logger.Log.ErrorWithReturn("API request failed with status code: %d", resp.StatusCode)
	}

	var orderResponse OrderResponse
	if err := json.NewDecoder(resp.Body).Decode(&orderResponse); err != nil {
		return logger.Log.ErrorWithReturnWrap(err, "could not decode order response")
	}

	logger.Log.WithFields(logrus.Fields{
		"order":         order,
		"orderResponse": orderResponse,
	}).Info("Order placed successfully")

	return nil
}
