package backtest

import (
	"github.com/adamdenes/gotrade/internal/models"
)

type BacktestEngine[S any] struct {
	cash     float64
	data     []*models.KlineSimple
	strategy Strategy[S]
}

func NewBacktestEngine(
	initialCash float64,
	OHLCData []*models.KlineSimple,
	strategy Strategy[any],
) *BacktestEngine[any] {
	return &BacktestEngine[any]{
		cash:     initialCash,
		data:     OHLCData,
		strategy: strategy,
	}
}

func (b *BacktestEngine[S]) Init() {
	b.strategy.SetData(b.data)
}

func (b *BacktestEngine[S]) Run() {
	b.strategy.Execute()
}

func (b *BacktestEngine[S]) Reset() {
	b.cash = 0
	b.data = nil
	b.strategy.SetData(nil)
	b.strategy = nil
}

func (b *BacktestEngine[S]) GetCash() float64 {
	return b.cash
}

func (b *BacktestEngine[S]) SetCash(cash float64) {
	b.cash = cash
}

func (b *BacktestEngine[S]) GetData() []*models.KlineSimple {
	return b.data
}

func (b *BacktestEngine[S]) SetData(historicalData []*models.KlineSimple) {
	b.data = historicalData
}

func (b *BacktestEngine[S]) GetStrategy() Strategy[S] {
	return b.strategy
}

func (b *BacktestEngine[S]) SetStrategy(strategy Strategy[S]) {
	b.strategy = strategy
}
