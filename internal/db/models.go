package db

import (
	"time"
)

type Client struct {
	ID        int       `db:"id"`
	Name      string    `db:"name"`
	ClientID  string    `db:"client_id"`
	Token     string    `db:"token"`
	IsAdmin   bool      `db:"is_admin"`
	CreatedAt time.Time `db:"created_at"`
}

type Ema10_15Clients struct {
	ID          int     `db:"id"`
	ClientID    int     `db:"client_id"` // client id from clients table
	FixLots     bool    `db:"fix_lots"`
	AllowedLoss float64 `db:"allowed_loss"`
	IsActive    bool    `db:"is_active"`
	Lots        float64 `db:"lots"`
	MaxLots     float64 `db:"max_lots"`
	RoundUpLG   bool    `db:"round_up_lg"`
}

type Ema10_15ClientDetails struct {
	Ema10_15Clients
	Name     string `db:"name"`
	Token    string `db:"token"`
	IsAdmin  bool   `db:"is_admin"`
	ClientID string `db:"client_id"` // String client ID from clients table
}

type MarketData struct {
	ID                  int       `db:"id"`
	SemExmExchID        string    `db:"sem_exm_exch_id"`
	SemSegment          string    `db:"sem_segment"`
	SemSmstSecurityID   int       `db:"sem_smst_security_id"`
	SemInstrumentName   string    `db:"sem_instrument_name"`
	SemExpiryCode       int       `db:"sem_expiry_code"`
	SemTradingSymbol    string    `db:"sem_trading_symbol"`
	SemLotUnits         float64   `db:"sem_lot_units"`
	SemCustomSymbol     string    `db:"sem_custom_symbol"`
	SemExpiryDate       time.Time `db:"sem_expiry_date"`
	SemStrikePrice      float64   `db:"sem_strike_price"`
	SemOptionType       string    `db:"sem_option_type"`
	SemTickSize         float64   `db:"sem_tick_size"`
	SemExpiryFlag       string    `db:"sem_expiry_flag"`
	SemExchInstrumentID string    `db:"sem_exch_instrument_id"`
	SemSeries           string    `db:"sem_series"`
	SmSymbolName        string    `db:"sm_symbol_name"`
}

// New model based on the image
type Config struct {
	IndexSecurityID      int    `db:"index_security_id"`
	IndexExchangeSegment string `db:"index_exchange_segment"`
	IndexInstrument      string `db:"index_instrument"`
	OptionExpiry         string `db:"option_expiry"`
}

type OptionsEmaConfig struct {
	Strategy     string  `db:"strategy"`
	CustomSymbol string  `db:"custom_symbol"`
	Ema          float64 `db:"ema"`
}
