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
	query := `CREATE TABLE IF NOT EXISTS kline_data (
		id serial PRIMARY KEY,
		open_time TIMESTAMP WITH TIME ZONE NOT NULL,
		open NUMERIC(18, 8) NOT NULL,
		high NUMERIC(18, 8) NOT NULL,
		low NUMERIC(18, 8) NOT NULL,
		close NUMERIC(18, 8) NOT NULL,
		volume NUMERIC(18, 8) NOT NULL,
		close_time TIMESTAMP WITH TIME ZONE NOT NULL,
		quote_volume NUMERIC(18, 8) NOT NULL,
		count INT NOT NULL,
		taker_buy_volume NUMERIC(18, 8) NOT NULL,
		taker_buy_quote_volume NUMERIC(18, 8) NOT NULL
	);`

	_, err := p.db.Exec(query)
	return err
}

func (p *PostgresDB) Close() {
	p.db.Close()
}

func (p *PostgresDB) CreateCandle(*models.Kline) error         { return nil }
func (p *PostgresDB) DeleteCandle(int) error                   { return nil }
func (p *PostgresDB) UpdateCandle(*models.Kline) error         { return nil }
func (p *PostgresDB) GetCandleByID(int) (*models.Kline, error) { return &models.Kline{}, nil }
