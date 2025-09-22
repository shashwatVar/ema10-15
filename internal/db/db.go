package db

import (
	"database/sql"
	"fmt"

	"ema10-15/internal/logger"

	_ "github.com/lib/pq"
)

var db *sql.DB

func Initialize(dataSourceName string) error {
	var err error
	db, err = sql.Open("postgres", dataSourceName)
	if err != nil {
		return logger.Log.ErrorWithReturnWrap(err, "Could not open the database")
	}

	if err := db.Ping(); err != nil {
		return logger.Log.ErrorWithReturnWrap(err, "Could not ping the database")
	}

	return nil
}

func GetDB() *sql.DB {
	return db
}

func Close() error {
	return db.Close()
}

var FetchAdminClient = func() (*Client, error) {
	var client Client
	query := `SELECT * FROM clients WHERE is_admin = true LIMIT 1`
	err := db.QueryRow(query).Scan(
		&client.ID,
		&client.Name,
		&client.ClientID,
		&client.Token,
		&client.IsAdmin,
		&client.CreatedAt,
	)
	if err != nil {
		return nil, logger.Log.ErrorWithReturnWrap(err, "Could not fetch admin client")
	}
	return &client, nil
}

var FetchAllClients = func() ([]*Ema10_15ClientDetails, error) {
	var clients []*Ema10_15ClientDetails
	query := `
		SELECT 
			ec.id, ec.client_id, ec.fix_lots, ec.allowed_loss, ec.is_active, ec.lots, ec.max_lots, ec.round_up_lg,
			c.name, c.token, c.is_admin, c.client_id
		FROM 
			ema10_15_clients ec
		JOIN 
			clients c ON ec.client_id = c.id
		WHERE 
			ec.is_active = true
	`
	rows, err := db.Query(query)
	if err != nil {
		return nil, logger.Log.ErrorWithReturnWrap(err, "Could not fetch clients")
	}
	defer rows.Close()
	for rows.Next() {
		var client Ema10_15ClientDetails
		err := rows.Scan(
			&client.ID,
			&client.ClientID,
			&client.FixLots,
			&client.AllowedLoss,
			&client.IsActive,
			&client.Lots,
			&client.MaxLots,
			&client.RoundUpLG,
			&client.Name,
			&client.Token,
			&client.IsAdmin,
			&client.ClientID,
		)
		if err != nil {
			return nil, fmt.Errorf("error scanning client: %w", err)
		}
		clients = append(clients, &client)
	}

	return clients, nil
}

var FetchMarketDataBySecurityID = func(securityID string) (*MarketData, error) {
	var marketData MarketData
	query := `
        SELECT * FROM market_data WHERE sem_smst_security_id = $1
    `
	err := db.QueryRow(query, securityID).Scan(
		&marketData.ID,
		&marketData.SemExmExchID,
		&marketData.SemSegment,
		&marketData.SemSmstSecurityID,
		&marketData.SemInstrumentName,
		&marketData.SemExpiryCode,
		&marketData.SemTradingSymbol,
		&marketData.SemLotUnits,
		&marketData.SemCustomSymbol,
		&marketData.SemExpiryDate,
		&marketData.SemStrikePrice,
		&marketData.SemOptionType,
		&marketData.SemTickSize,
		&marketData.SemExpiryFlag,
		&marketData.SemExchInstrumentID,
		&marketData.SemSeries,
		&marketData.SmSymbolName,
	)
	if err != nil {
		return nil, logger.Log.ErrorWithReturnWrap(err, "Could not fetch market data for security ID %s", securityID)
	}
	return &marketData, nil
}

var FetchIndexConfig = func() (*Config, error) {
	var config Config
	query := `SELECT index_security_id, index_exchange_segment, index_instrument, option_expiry FROM ema10_15_config WHERE id = 4`
	err := db.QueryRow(query).Scan(
		&config.IndexSecurityID,
		&config.IndexExchangeSegment,
		&config.IndexInstrument,
		&config.OptionExpiry,
	)

	if err != nil {
		return nil, logger.Log.ErrorWithReturnWrap(err, "Could not fetch config")
	}
	return &config, nil
}

var FetchOptions = func(optionExpiry string) ([]*MarketData, error) {
	var indexOptions []*MarketData

	query := `SELECT * FROM market_data WHERE sem_instrument_name = 'OPTIDX' AND sem_exm_exch_id = 'NSE' AND sem_custom_symbol LIKE $1`
	rows, err := db.Query(query, optionExpiry+"%")

	if err != nil {
		return nil, logger.Log.ErrorWithReturnWrap(err, "Could not fetch index options")
	}

	defer rows.Close()
	for rows.Next() {
		var indexOption MarketData
		err := rows.Scan(
			&indexOption.ID,
			&indexOption.SemExmExchID,
			&indexOption.SemSegment,
			&indexOption.SemSmstSecurityID,
			&indexOption.SemInstrumentName,
			&indexOption.SemExpiryCode,
			&indexOption.SemTradingSymbol,
			&indexOption.SemLotUnits,
			&indexOption.SemCustomSymbol,
			&indexOption.SemExpiryDate,
			&indexOption.SemStrikePrice,
			&indexOption.SemOptionType,
			&indexOption.SemTickSize,
			&indexOption.SemExpiryFlag,
			&indexOption.SemExchInstrumentID,
			&indexOption.SemSeries,
			&indexOption.SmSymbolName,
		)
		if err != nil {
			return nil, logger.Log.ErrorWithReturnWrap(err, "Could not scan index option")
		}
		indexOptions = append(indexOptions, &indexOption)
	}
	return indexOptions, nil
}

var FetchAllEma10Data = func() ([]*OptionsEmaConfig, error) {
	var emaData []*OptionsEmaConfig
	query := `SELECT strategy, custom_symbol, ema FROM options_ema_config WHERE strategy = 'EMA10-15'`
	rows, err := db.Query(query)
	if err != nil {
		return nil, logger.Log.ErrorWithReturnWrap(err, "Could not fetch all EMA data")
	}
	defer rows.Close()

	for rows.Next() {
		var ema OptionsEmaConfig
		err := rows.Scan(
			&ema.Strategy,
			&ema.CustomSymbol,
			&ema.Ema,
		)
		if err != nil {
			return nil, fmt.Errorf("error scanning EMA data: %w", err)
		}
		emaData = append(emaData, &ema)
	}

	return emaData, nil
}
