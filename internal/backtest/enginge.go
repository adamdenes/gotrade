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
	Buy(asset string, price float64) models.TypeOfOrder
	Sell(asset string, price float64) models.TypeOfOrder
	PlaceOrder(o models.TypeOfOrder)
	GetClosePrices()
	GetName() string
	RoundToStepSize(value, stepSize float64) float64
	RoundToTickSize(value, tickSize float64) float64
}
