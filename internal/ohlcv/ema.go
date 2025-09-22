package ohlcv

func (p *Processor) CalculateEMA10(candles []Candle, initialEma float64) float64 {
	// Calculate the multiplier
	multiplier := 2.0 / (10.0 + 1.0)

	// Initialize EMA with the provided initial value
	ema := initialEma

	// Calculate EMA for each candle using range
	for _, candle := range candles {
		ema = (candle.Close * multiplier) + (ema * (1 - multiplier))
	}

	return ema
}
