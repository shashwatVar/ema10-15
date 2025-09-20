package ohlcv

import (
	"bytes"
	"ema10-15/internal/logger"
	"ema10-15/internal/utils"
	"encoding/json"
	"net/http"
	"time"
)

type Candle struct {
	Time   time.Time
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume float64
}

func (p *Processor) aggregateCandles(candles []Candle) Candle {
	if len(candles) == 0 {
		return Candle{}
	}

	result := Candle{
		Open:   candles[0].Open,
		High:   candles[0].High,
		Low:    candles[0].Low,
		Close:  candles[len(candles)-1].Close,
		Volume: 0,
		Time:   candles[len(candles)-1].Time,
	}

	for _, c := range candles {
		if c.High > result.High {
			result.High = c.High
		}
		if c.Low < result.Low {
			result.Low = c.Low
		}
		result.Volume += c.Volume
	}

	return result
}

func (p *Processor) fetchCandles(securityID string, exchangeSegment string, instrument string) ([]Candle, Candle, error) {
	url := "https://api.dhan.co/v2/charts/intraday"

	p.state.mutex.RLock()
	payload := map[string]string{
		"securityId":      securityID,
		"exchangeSegment": exchangeSegment,
		"instrument":      instrument,
		"interval":        "1",                                               // 5-minute candles
		"fromDate":        time.Now().AddDate(0, 0, -5).Format("2006-01-02"), // 4 days before today
		"toDate":          time.Now().Format("2006-01-02"),                   // today
	}
	token := p.state.AdminToken
	p.state.mutex.RUnlock()

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return []Candle{}, Candle{}, logger.Log.ErrorWithReturnWrap(err, "could not marshal payload")
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return []Candle{}, Candle{}, logger.Log.ErrorWithReturnWrap(err, "could not create request")
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("access-token", token)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return []Candle{}, Candle{}, logger.Log.ErrorWithReturnWrap(err, "could not fetch data")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return []Candle{}, Candle{}, logger.Log.ErrorWithReturn("API request failed with status code: %d", resp.StatusCode)
	}

	var data OHLCVData
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return []Candle{}, Candle{}, logger.Log.ErrorWithReturnWrap(err, "could not decode OHLCV data")
	}

	minCandles := 15
	var newCandles []Candle

	if len(data.Timestamp) < minCandles {
		return []Candle{}, Candle{}, logger.Log.ErrorWithReturn("not enough data")
	}

	for i := 0; i < len(data.Timestamp); i++ {
		newCandles = append(newCandles, Candle{
			Open:   data.Open[i],
			High:   data.High[i],
			Low:    data.Low[i],
			Close:  data.Close[i],
			Volume: data.Volume[i],
			Time:   utils.ConvertToDateTime(float64(data.Timestamp[i])),
		})
	}

	aggregatedCandles := []Candle{}
	for i := 0; i < len(newCandles); i += 15 {
		aggregateCandles := newCandles[i : i+15]
		aggregatedCandle := p.aggregateCandles(aggregateCandles)
		aggregatedCandles = append(aggregatedCandles, aggregatedCandle)
	}

	currentCandle := aggregatedCandles[len(aggregatedCandles)-1]

	return aggregatedCandles, currentCandle, nil
}
