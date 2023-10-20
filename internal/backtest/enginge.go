package backtest

import "github.com/adamdenes/gotrade/internal/models"

type Engine interface {
	Run()
	Init()
	Reset()
	FillOrders()
}

type Strategy[S any] interface {
	Execute()
	IsBacktest(bool)
	SetBalance(float64)
	GetBalance() float64
	SetOrders([]*models.Order)
	GetOrders() []*models.Order
	SetPositionSize(float64)
	GetPositionSize() float64
	SetData([]*models.KlineSimple)
	SetAsset(string)
	Buy(asset string, quantity float64, price float64) *models.Order
	Sell(asset string, quantity float64, price float64) *models.Order
}
