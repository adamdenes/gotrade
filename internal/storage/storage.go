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
