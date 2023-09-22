package storage

import (
	"database/sql"
	"log"

	"github.com/adamdenes/gotrade/internal/models"
	_ "github.com/lib/pq"
)

type Storage interface {
	CreateCandle(*models.Kline) error
	DeleteCandle(int) error
	UpdateCandle(*models.Kline) error
	GetCandleByID(int) (*models.Kline, error)
}

type PostgresStore struct {
	db *sql.DB
}

func NewPostgresStore(dsn string) (*PostgresStore, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatal(err)
	}

	if err := db.Ping(); err != nil {
		return nil, err
	}

	return &PostgresStore{db: db}, nil
}

func (p *PostgresStore) Init() error {
	return p.CreateCandleTable()
}

func (p *PostgresStore) CreateCandleTable() error {
	query := `CREATE TABLE IF NOT EXISTS kline_data (
		id serial PRIMARY KEY,
		open_time TIMESTAMP WITH TIME ZONE NOT NULL,
		open_price NUMERIC(18, 8) NOT NULL,
		high_price NUMERIC(18, 8) NOT NULL,
		low_price NUMERIC(18, 8) NOT NULL,
		close_price NUMERIC(18, 8) NOT NULL,
		volume NUMERIC(18, 8) NOT NULL,
		close_time TIMESTAMP WITH TIME ZONE NOT NULL,
		quote_asset_volume NUMERIC(18, 8) NOT NULL,
		number_of_trades INT NOT NULL,
		taker_buy_base_asset_volume NUMERIC(18, 8) NOT NULL,
		taker_buy_quote_asset_volume NUMERIC(18, 8) NOT NULL
	);`

	_, err := p.db.Exec(query)
	return err
}

func (p *PostgresStore) Close() {
	p.db.Close()
}

func (p *PostgresStore) CreateCandle(*models.Kline) error         { return nil }
func (p *PostgresStore) DeleteCandle(int) error                   { return nil }
func (p *PostgresStore) UpdateCandle(*models.Kline) error         { return nil }
func (p *PostgresStore) GetCandleByID(int) (*models.Kline, error) { return &models.Kline{}, nil }
