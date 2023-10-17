package backtest

import (
	"github.com/adamdenes/gotrade/internal/models"
)

type BacktestEngine struct {
	cash     float64
	data     []*models.KlineSimple
	strategy Strategy
}

func NewBacktestEngine(
	initialCash float64,
	OHLCData []*models.KlineSimple,
	strategy Strategy,
) *BacktestEngine {
	return &BacktestEngine{
		cash:     initialCash,
		data:     OHLCData,
		strategy: strategy,
	}
}

func (b *BacktestEngine) Init() {
	b.strategy.SetData(b.data)
}

func (b *BacktestEngine) Run() {
	b.strategy.Execute()
}

func (b *BacktestEngine) Reset() {
	b.cash = 0
	b.data = nil
	b.strategy.SetData(nil)
	b.strategy = nil
}

func (b *BacktestEngine) GetCash() float64 {
	return b.cash
}

func (b *BacktestEngine) SetCash(cash float64) {
	b.cash = cash
}

func (b *BacktestEngine) GetData() []*models.KlineSimple {
	return b.data
}

func (b *BacktestEngine) SetData(historicalData []*models.KlineSimple) {
	b.data = historicalData
}

func (b *BacktestEngine) GetStrategy() Strategy {
	return b.strategy
}

func (b *BacktestEngine) SetStrategy(strategy Strategy) {
	b.strategy = strategy
}
