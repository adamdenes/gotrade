package storage

import (
	"archive/zip"
	"context"

	"github.com/adamdenes/gotrade/internal/models"
)

type Storage interface {
	CreateCandle(*models.Kline) error
	DeleteCandle(int) error
	UpdateCandle(*models.Kline) error
	GetCandleByOpenTime(int) (*models.Kline, error)
	FetchData(context.Context, string, string, int64, int64) ([]*models.KlineSimple, error)
	Copy([]byte, *string, *string) error
	Stream(*zip.Reader) error
	QueryLastRow() (*models.KlineRequest, error)
}
