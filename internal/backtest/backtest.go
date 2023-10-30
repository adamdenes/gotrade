package backtest

import (
	"github.com/adamdenes/gotrade/internal/logger"
	"github.com/adamdenes/gotrade/internal/models"
)

type BacktestEngine[S any] struct {
	cash         float64
	positionSize float64
	data         []*models.KlineSimple
	strategy     Strategy[S]
	DataChannel  chan models.TypeOfOrder
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
		DataChannel: make(chan models.TypeOfOrder, 1),
	}
}

func (b *BacktestEngine[S]) Init() {
	b.strategy.SetPositionSize(1.0)
	b.strategy.SetBalance(b.cash)
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
	logger.Info.Println(
		"Order len:",
		len(orders),
		"Current balance:",
		b.cash,
		"Current Bar:",
		currBar,
	)

	for i, order := range orders {
		switch order := order.(type) {
		case *models.Order:
			if order.Side == models.BUY {
				// Calculate the cost of buying the specified quantity at the open price
				cost := currBar.Open * order.Quantity

				// Check if there is enough cash to fill the buy order
				if b.cash >= cost {
					// Fill the buy order
					b.cash -= cost
					b.strategy.SetPositionSize(order.Quantity)
					b.strategy.SetBalance(b.cash)
					logger.Debug.Printf(
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
					b.strategy.SetBalance(b.cash)
					logger.Debug.Printf("SELL filled! Cash: %v, Position Size: %v\n",
						b.cash,
						b.strategy.GetPositionSize(),
					)
					// Mark order as filled
					orders = append(orders[:i], orders[i+1:]...)
					b.DataChannel <- order
				}
			}
		case *models.OrderOCO:
			var cost float64

			if order.Side == "BUY" {
				if order.StopPrice <= currBar.Low {
					cost = order.StopLimitPrice * order.Quantity
					logger.Debug.Printf("StopPrice: %v, StopLimitPrice: %v, Cost: %v\n", order.StopPrice, order.StopLimitPrice, cost)
					if b.cash >= cost {
						// b.fillStopLimit(currBar.Low, order.StopLimitPrice)
						b.cash -= cost
						b.strategy.SetPositionSize(order.Quantity)
						b.strategy.SetBalance(b.cash)

						logger.Debug.Printf(
							"BUY - STOP LIMIT filled! Cash: %v, Position Size: %v\n",
							b.cash,
							b.strategy.GetPositionSize(),
						)
						logger.Debug.Printf("Buy filled: LIMIT: %f / LOW: %f", order.Price, currBar.Low)
						orders = append(orders[:i], orders[i+1:]...)
						b.DataChannel <- order
					}
				} else if order.Price >= currBar.Low {
					cost = currBar.Open * order.Quantity
					if b.cash >= cost {
						// b.fillTakeProfit(currBar.Open)
						b.cash -= cost
						b.strategy.SetPositionSize(order.Quantity)
						b.strategy.SetBalance(b.cash)

						logger.Debug.Printf(
							"BUY - TAKE PROFIT filled! Cash: %v, Position Size: %v\n",
							b.cash,
							b.strategy.GetPositionSize(),
						)
						logger.Debug.Printf("Buy filled: LIMIT: %f / LOW: %f", order.Price, currBar.Low)
						orders = append(orders[:i], orders[i+1:]...)
						b.DataChannel <- order
					}
				} else {
					logger.Debug.Printf("Buy NOT filled: LIMIT: %f / LOW: %f", order.Price, currBar.Low)
				}
			} else if order.Side == "SELL" {
				if order.StopPrice >= currBar.High {
					cost = order.StopLimitPrice * order.Quantity
					if b.strategy.GetPositionSize() >= order.Quantity && b.cash >= cost {
						// b.fillStopLimit(currBar.High, order.StopLimitPrice)
						b.cash += cost
						b.strategy.SetPositionSize(-order.Quantity)
						b.strategy.SetBalance(b.cash)

						logger.Debug.Printf(
							"SELL - STOP LIMIT filled! Cash: %v, Position Size: %v\n",
							b.cash,
							b.strategy.GetPositionSize(),
						)
						logger.Debug.Printf("Sell filled: LIMIT: %f / HIGH: %f", order.Price, currBar.High)
						orders = append(orders[:i], orders[i+1:]...)
						b.DataChannel <- order
					}
				} else if order.Price <= currBar.High {
					cost = currBar.Open * order.Quantity
					if b.strategy.GetPositionSize() >= order.Quantity && b.cash >= cost {
						// b.fillTakeProfit(currBar.High)
						b.cash += cost
						b.strategy.SetPositionSize(-order.Quantity)
						b.strategy.SetBalance(b.cash)

						logger.Debug.Printf(
							"SELL - TAKE PROFIT filled! Cash: %v, Position Size: %v\n",
							b.cash,
							b.strategy.GetPositionSize(),
						)
						logger.Debug.Printf("Sell filled: LIMIT: %f / HIGH: %f", order.Price, currBar.High)
						orders = append(orders[:i], orders[i+1:]...)
						b.DataChannel <- order
					}
				} else {
					logger.Debug.Printf("Sell NOT filled: LIMIT: %f / HIGH: %f", order.Price, currBar.High)
				}
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
