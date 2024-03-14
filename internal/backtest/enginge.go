package backtest

import "github.com/adamdenes/gotrade/internal/models"

type Engine interface {
	Run()
	Init()
	Reset()
	FillOrders()
	Metrics()
}

type Strategy[S any] interface {
	Execute()
	IsBacktest(bool)
	SetBalance(float64)
	GetBalance() float64
	SetOrders([]models.TypeOfOrder)
	GetOrders() []models.TypeOfOrder
	SetPositionSize(float64)
	GetPositionSize() float64
	SetData([]*models.KlineSimple)
	SetAsset(string)
	Buy(string, float64) models.TypeOfOrder
	Sell(string, float64) models.TypeOfOrder
	PlaceOrder(models.TypeOfOrder) error
	GetClosePrices()
	GetName() string
	RoundToStepSize(float64, float64) float64
	RoundToTickSize(float64, float64) float64
	CalculatePositionSize(string, float64, float64, float64) (float64, error)
	DetermineEntryAndStopLoss(string, float64) (float64, float64)
}
