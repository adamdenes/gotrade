package backtest

import (
	"github.com/adamdenes/gotrade/internal/logger"
	"github.com/adamdenes/gotrade/internal/models"
)

type Trade struct {
	EntryPrice float64
	ExitPrice  float64
	Quantity   float64
	Side       string // "BUY" or "SELL"
	Timestamp  int64
	ProfitLoss float64
}

type BacktestEngine[S any] struct {
	cash          float64
	symbol        string
	assetAmount   float64
	positionSize  float64
	positionValue []float64
	data          []*models.KlineSimple
	trades        []Trade
	strategy      Strategy[S]
	DataChannel   chan models.TypeOfOrder
}

func NewBacktestEngine(
	initialCash float64,
	OHLCData []*models.KlineSimple,
	symbol string,
	strategy Strategy[any],
) *BacktestEngine[any] {
	return &BacktestEngine[any]{
		cash:        initialCash,
		data:        OHLCData,
		symbol:      symbol,
		strategy:    strategy,
		DataChannel: make(chan models.TypeOfOrder, 1),
		trades:      []Trade{},
	}
}

func (b *BacktestEngine[S]) Init() {
	b.strategy.SetPositionSize(1.0)
	b.strategy.SetBalance(b.cash)
	b.strategy.IsBacktest(true)
	b.strategy.SetAsset(b.symbol)
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
		"Base amount:",
		b.assetAmount,
	)

	// Calculate and update the position value
	b.calculatePositionValue()

	for i, order := range orders {
		switch order := order.(type) {
		case *models.PostOrder:
			if order.Side == models.BUY {
				// Calculate the cost of buying the specified quantity at the open price
				cost := currBar.Open * order.Quantity

				b.assetAmount += order.Quantity
				logger.Debug.Println("AMOUNT:", b.assetAmount)
				// Check if there is enough cash to fill the buy order
				if b.cash >= cost {
					// Fill the buy order
					b.cash -= cost
					b.strategy.SetPositionSize(order.Quantity)
					b.strategy.SetBalance(b.cash)
					logger.Debug.Printf(
						"BUY filled! Cash: %v, Position Size: %v, Amount worth: %v\n",
						b.cash,
						b.strategy.GetPositionSize(),
						b.assetAmount*currBar.Close,
					)

					// Record the trade
					b.trades = append(b.trades, Trade{
						EntryPrice: currBar.Open,
						Quantity:   order.Quantity,
						Side:       "BUY",
						Timestamp:  currBar.OpenTime.UnixMilli(),
					})

					// Mark order as filled / remove from order list
					orders = append(orders[:i], orders[i+1:]...)
					b.DataChannel <- order
				}
			} else if order.Side == models.SELL {
				// Calculate the revenue from selling the specified quantity at the open price
				revenue := currBar.Open * order.Quantity

				b.assetAmount -= order.Quantity
				logger.Debug.Println("AMOUNT:", b.assetAmount)
				// Check if there are enough assets to fill the sell order
				if b.strategy.GetPositionSize() >= order.Quantity {
					// Fill the sell order
					b.cash += revenue
					b.strategy.SetPositionSize(-order.Quantity)
					b.strategy.SetBalance(b.cash)
					logger.Debug.Printf("SELL filled! Cash: %v, Position Size: %v, Amount worth: %v\n",
						b.cash,
						b.strategy.GetPositionSize(),
						b.assetAmount*currBar.Close,
					)
					// Record the trade
					b.trades = append(b.trades, Trade{
						EntryPrice: currBar.Open,
						Quantity:   order.Quantity,
						Side:       "SELL",
						Timestamp:  currBar.OpenTime.UnixMilli(),
					})

					// Mark order as filled
					orders = append(orders[:i], orders[i+1:]...)
					b.DataChannel <- order
				}
			}
		case *models.PostOrderOCO:
			var cost float64

			if order.Side == "BUY" {
				if order.StopPrice <= currBar.Low {
					cost = order.StopLimitPrice * order.Quantity
					logger.Debug.Printf("StopPrice: %v, StopLimitPrice: %v, Cost: %v\n", order.StopPrice, order.StopLimitPrice, cost)

					if b.cash >= cost {
						b.assetAmount += order.Quantity
						logger.Debug.Println("AMOUNT:", b.assetAmount)
						// b.fillStopLimit(currBar.Low, order.StopLimitPrice)
						b.cash -= cost
						b.strategy.SetPositionSize(order.Quantity)
						b.strategy.SetBalance(b.cash)

						logger.Debug.Printf(
							"BUY - STOP LIMIT filled! Order: %v, Cash: %v, Position Size: %v, Amount worth: %v\n",
							i+1,
							b.cash,
							b.strategy.GetPositionSize(),
							b.assetAmount*order.StopLimitPrice,
						)
						logger.Debug.Printf("Buy filled: STOP LIMIT: %f / LOW: %f", order.StopLimitPrice, currBar.Low)

						// Record the trade details with exit price and profit/loss
						b.trades = append(b.trades, Trade{
							EntryPrice: order.StopPrice,
							ExitPrice:  order.StopLimitPrice,
							Quantity:   order.Quantity,
							Side:       "BUY",
							Timestamp:  currBar.OpenTime.UnixMilli(),
						})

						if i >= 0 && i < len(orders) {
							orders = append(orders[:i], orders[i+1:]...)
						}
						b.DataChannel <- order
					}
				} else if order.Price >= currBar.Low {
					cost = currBar.Open * order.Quantity

					if b.cash >= cost {
						b.assetAmount += order.Quantity
						logger.Debug.Println("AMOUNT:", b.assetAmount)
						// b.fillTakeProfit(currBar.Open)
						b.cash -= cost
						b.strategy.SetPositionSize(order.Quantity)
						b.strategy.SetBalance(b.cash)

						logger.Debug.Printf(
							"BUY - TAKE PROFIT filled! Order: %v, Cash: %v, Position Size: %v, Amount worth: %v\n",
							i+1,
							b.cash,
							b.strategy.GetPositionSize(),
							b.assetAmount*order.Price,
						)
						logger.Debug.Printf("Buy filled: LIMIT: %f / LOW: %f", order.Price, currBar.Low)

						// Record the trade details with exit price and profit/loss
						b.trades = append(b.trades, Trade{
							EntryPrice: currBar.Open,
							ExitPrice:  currBar.Close,
							Quantity:   order.Quantity,
							Side:       "BUY",
							Timestamp:  currBar.OpenTime.UnixMilli(),
						})

						if i >= 0 && i < len(orders) {
							orders = append(orders[:i], orders[i+1:]...)
						}
						b.DataChannel <- order
					}
				} else {
					logger.Debug.Printf("Buy NOT filled: LIMIT: %f / LOW: %f", order.Price, currBar.Low)
				}
			} else if order.Side == "SELL" && b.assetAmount >= order.Quantity {
				if order.StopPrice >= currBar.High {
					cost = order.StopLimitPrice * order.Quantity

					if b.strategy.GetPositionSize() >= order.Quantity && b.cash >= cost {
						b.assetAmount -= order.Quantity
						logger.Debug.Println("AMOUNT:", b.assetAmount)
						// b.fillStopLimit(currBar.High, order.StopLimitPrice)
						b.cash += cost
						b.strategy.SetPositionSize(-order.Quantity)
						b.strategy.SetBalance(b.cash)

						logger.Debug.Printf(
							"SELL - STOP LIMIT filled! Order: %v, Cash: %v, Position Size: %v, Amount worth: %v\n",
							i+1,
							b.cash,
							b.strategy.GetPositionSize(),
							b.assetAmount*order.StopLimitPrice,
						)
						logger.Debug.Printf("Sell filled: STOP LIMIT: %f / HIGH: %f", order.StopLimitPrice, currBar.High)

						b.trades = append(b.trades, Trade{
							EntryPrice: order.StopPrice,
							ExitPrice:  order.StopLimitPrice,
							Quantity:   order.Quantity,
							Side:       "SELL",
							Timestamp:  currBar.OpenTime.UnixMilli(),
						})

						if i >= 0 && i < len(orders) {
							orders = append(orders[:i], orders[i+1:]...)
						}
						b.DataChannel <- order
					}
				} else if order.Price <= currBar.High {
					cost = currBar.Open * order.Quantity

					if b.strategy.GetPositionSize() >= order.Quantity && b.cash >= cost {
						b.assetAmount -= order.Quantity
						logger.Debug.Println("AMOUNT:", b.assetAmount)
						// b.fillTakeProfit(currBar.High)
						b.cash += cost
						b.strategy.SetPositionSize(-order.Quantity)
						b.strategy.SetBalance(b.cash)

						logger.Debug.Printf(
							"SELL - TAKE PROFIT filled! Order: %v, Cash: %v, Position Size: %v, Amount worth: %v\n",
							i+1,
							b.cash,
							b.strategy.GetPositionSize(),
							b.assetAmount*order.Price,
						)
						logger.Debug.Printf("Sell filled: LIMIT: %f / HIGH: %f", order.Price, currBar.High)

						b.trades = append(b.trades, Trade{
							EntryPrice: order.Price,
							ExitPrice:  currBar.Close,
							Quantity:   order.Quantity,
							Side:       "SELL",
							Timestamp:  currBar.OpenTime.UnixMilli(),
						})

						if i >= 0 && i < len(orders) {
							orders = append(orders[:i], orders[i+1:]...)
						}
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
	b.strategy.SetAsset("")
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

func (b *BacktestEngine[S]) Metrics() {
	logger.Info.Println("PositionValues overtime:", b.positionValue)
	b.calculateROI()
	b.calculatePnL()
}

func (b *BacktestEngine[S]) calculatePositionValue() {
	bar := b.data[len(b.data)-1] // Get the latest bar
	b.positionSize = b.strategy.GetPositionSize()

	// Calculate the current position value using the latest bar's close price
	currentPositionValue := b.positionSize * bar.Close

	// Append the current position value to the list
	b.positionValue = append(b.positionValue, currentPositionValue)
}

func (b *BacktestEngine[S]) calculatePnL() float64 {
	// Calculate the exit price and profit/loss for each trade
	for i := range b.trades {
		trade := &b.trades[i]
		// Calculate profit/loss
		trade.ProfitLoss = (trade.ExitPrice - trade.EntryPrice) * trade.Quantity
	}

	var totalProfitLoss float64
	for _, trade := range b.trades {
		totalProfitLoss += trade.ProfitLoss
	}
	logger.Info.Printf("Total Profit/Loss: %f\n", totalProfitLoss)

	averageProfitLoss := totalProfitLoss / float64(len(b.trades))
	logger.Info.Printf("Average Profit/Loss per Trade: %f\n", averageProfitLoss)
	return averageProfitLoss
}

func (b *BacktestEngine[S]) calculateROI() float64 {
	totalProfitOrLoss := 0.0
	initialInvestment := b.cash

	for _, trade := range b.trades {
		totalProfitOrLoss += trade.ProfitLoss
	}

	roi := (totalProfitOrLoss / initialInvestment) * 100.0

	logger.Info.Println("ROI:", roi)
	return roi
}
