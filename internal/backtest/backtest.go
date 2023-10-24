package backtest

import (
	"fmt"

	"github.com/adamdenes/gotrade/internal/models"
)

type BacktestEngine[S any] struct {
	cash         float64
	positionSize float64
	data         []*models.KlineSimple
	strategy     Strategy[S]
	DataChannel  chan *models.Order
}

func NewBacktestEngine(
	initialCash float64,
	OHLCData []*models.KlineSimple,
	strategy Strategy[any],
) *BacktestEngine[any] {
	return &BacktestEngine[any]{
		cash:        initialCash,
		data:        OHLCData,
		strategy:    strategy,
		DataChannel: make(chan *models.Order, 1),
	}
}

func (b *BacktestEngine[S]) Init() {
	b.strategy.SetPositionSize(1.0)
	b.strategy.IsBacktest(true)
}

func (b *BacktestEngine[S]) Run() {
	b.strategy.SetData(b.data)
	b.strategy.Execute()
	b.FillOrders()
}

func (b *BacktestEngine[S]) FillOrders() {
	orders := b.strategy.GetOrders()
	if len(orders) < 1 {
		return
	}

	currBar := b.data[len(b.data)-1]
	fmt.Println("Order len:", len(orders), "Current balance:", b.cash, "Current Bar:", currBar)

	for i, order := range orders {
		if order.Side == models.BUY {
			// Calculate the cost of buying the specified quantity at the open price
			cost := currBar.Open * order.Quantity

			// Check if there is enough cash to fill the buy order
			if b.cash >= cost {
				// Fill the buy order
				b.cash -= cost
				b.strategy.SetPositionSize(order.Quantity)
				fmt.Printf(
					"BUY filled! Cash: %v, Position Size: %v\n",
					b.cash,
					b.strategy.GetPositionSize(),
				)
				// Mark order as filled / remove from order list
				orders = append(orders[:i], orders[i+1:]...)
				b.DataChannel <- order
			}
		} else if order.Side == models.SELL {
			// Calculate the revenue from selling the specified quantity at the open price
			revenue := currBar.Open * order.Quantity

			// Check if there are enough assets to fill the sell order
			if b.strategy.GetPositionSize() >= order.Quantity {
				// Fill the sell order
				b.cash += revenue
				b.strategy.SetPositionSize(-order.Quantity)
				fmt.Printf("SELL filled! Cash: %v, Position Size: %v\n",
					b.cash,
					b.strategy.GetPositionSize(),
				)
				// Mark order as filled
				orders = append(orders[:i], orders[i+1:]...)
				b.DataChannel <- order
			}
		}
	}
	b.strategy.SetOrders(orders)
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
	b.data = append(b.data, historicalData...)
}

func (b *BacktestEngine[S]) GetStrategy() Strategy[S] {
	return b.strategy
}

func (b *BacktestEngine[S]) SetStrategy(strategy Strategy[S]) {
	b.strategy = strategy
}
