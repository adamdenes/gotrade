package storage

import (
	"database/sql"
	"log"

	"github.com/adamdenes/gotrade/internal/models"
	"github.com/lib/pq"
)

type Storage interface {
	CreateCandle(*models.Kline) error
	DeleteCandle(int) error
	UpdateCandle(*models.Kline) error
	GetCandleByID(int) (*models.Kline, error)
	Copy([][]string) error
}

type PostgresDB struct {
	db *sql.DB
}

func NewPostgresDB(dsn string) (*PostgresDB, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatal(err)
	}

	if err := db.Ping(); err != nil {
		return nil, err
	}

	return &PostgresDB{db: db}, nil
}

func (p *PostgresDB) Init() error {
	return p.CreateCandleTable()
}

func (p *PostgresDB) CreateCandleTable() error {
	_, err := p.db.Exec("CREATE SCHEMA IF NOT EXISTS binance")
	if err != nil {
		return err
	}
	// 1672531200000,16541.77000000,16543.20000000,16541.73000000,16542.37000000,2.03879000,1672531200999,33726.64613940,104,1.65187000,27326.28208680,0

	query := `CREATE TABLE IF NOT EXISTS binance.kline_data (
		id serial PRIMARY KEY,
		open_time bigint NOT NULL,
		open NUMERIC(18, 8) NOT NULL,
		high NUMERIC(18, 8) NOT NULL,
		low NUMERIC(18, 8) NOT NULL,
		close NUMERIC(18, 8) NOT NULL,
		volume NUMERIC(18, 8) NOT NULL,
		close_time bigint NOT NULL,
		quote_volume NUMERIC(18, 8) NOT NULL,
		count INT NOT NULL,
		taker_buy_volume NUMERIC(18, 8) NOT NULL,
		taker_buy_quote_volume NUMERIC(18, 8) NOT NULL
	);`

	_, err = p.db.Exec(query)
	return err
}

func (p *PostgresDB) Close() {
	p.db.Close()
}

func (p *PostgresDB) CreateCandle(k *models.Kline) error {
	query := `
        INSERT INTO kline_data (
            open_time,
            open,
            high,
            low,
            close,
            volume,
            close_time,
            quote_volume,
            count,
            taker_buy_volume,
            taker_buy_quote_volume
        )
        VALUES (
            $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
        )`

	stmt, err := p.db.Prepare(query)
	if err != nil {
		return err
	}
	defer stmt.Close()

	_, err = stmt.Exec(
		k.OpenTime,
		k.Open,
		k.High,
		k.Low,
		k.Close,
		k.Volume,
		k.CloseTime,
		k.QuoteAssetVolume,
		k.NumberOfTrades,
		k.TakerBuyBaseAssetVol,
		k.TakerBuyQuoteAssetVol,
	)
	if err != nil {
		return err
	}

	return nil
}

func (p *PostgresDB) DeleteCandle(int) error                   { return nil }
func (p *PostgresDB) UpdateCandle(*models.Kline) error         { return nil }
func (p *PostgresDB) GetCandleByID(int) (*models.Kline, error) { return &models.Kline{}, nil }

func (p *PostgresDB) Copy(records [][]string) error {
	tx, err := p.db.Begin()
	if err != nil {
		return err
	}

	stmt, err := tx.Prepare(
		pq.CopyInSchema("binance", "kline_data", "open_time", "open", "high", "low", "close", "volume", "close_time", "quote_volume", "count", "taker_buy_volume", "taker_buy_quote_volume"))
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, row := range records {
		_, err := stmt.Exec(row[0], row[1], row[2], row[3], row[4], row[5], row[6], row[7], row[8], row[9], row[10])
		if err != nil {
			return err
		}
	}

	_, err = stmt.Exec()
	if err != nil {
		return err
	}

	err = tx.Commit()
	if err != nil {
		return err
	}

	return nil
}
