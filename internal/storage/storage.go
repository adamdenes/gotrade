package storage

import (
	"archive/zip"
	"context"

	"github.com/adamdenes/gotrade/internal/models"
)

type Storage interface {
	Create(*models.Kline) error
	Delete(int) error
	Update(*models.Kline) error
	GetCandleByOpenTime(int) (*models.Kline, error)
	FetchData(context.Context, string, string, int64, int64) ([]*models.KlineSimple, error)
	Copy([]byte, *string, *string) error
	Stream(*zip.Reader) error
	QueryLastRow() (*models.KlineRequest, error)
	SaveSymbols(map[string]*models.SymbolFilter) error
	SaveTrade(*models.Trade) error
	GetTrade(int64) (*models.Trade, error)
	UpdateTrade(int64, string) error
	FetchTrades() ([]*models.Trade, error)
	SaveOrder(string, *models.PostOrderResponse) error
	FetchOrders() ([]*models.GetOrderResponse, error)
	UpdateOrder(*models.GetOrderResponse) error
	CreateBot(*models.TradingBot) error
	GetBot(string, string) (*models.TradingBot, error)
	DeleteBot(int) error
	GetBots() ([]*models.TradingBot, error)
	ExecuteMaterializedViewCreation(mvc *models.MaterializedViewConfig) error
	RefreshContinuousAggregate(string) error
}
