package strategy

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/adamdenes/gotrade/cmd/rest"
	"github.com/adamdenes/gotrade/internal/backtest"
	"github.com/adamdenes/gotrade/internal/logger"
	"github.com/adamdenes/gotrade/internal/models"
	"github.com/adamdenes/gotrade/internal/storage"
	"github.com/markcheno/go-talib"
)

const MAX_GRID_LEVEL int8 = 4

type GridStrategy struct {
	name                  string                // Name of strategy
	db                    storage.Storage       // Database interface
	balance               float64               // Current balance
	positionSize          float64               // Position size
	riskPercentage        float64               // Risk %
	stopLossPercentage    float64               // Invalidation point
	asset                 string                // Trading pair
	backtest              bool                  // Are we backtesting?
	orders                []models.TypeOfOrder  // Pending orders
	data                  []*models.KlineSimple // Price data
	closes                []float64             // Close prices
	highs                 []float64             // Highs
	lows                  []float64             // Lows
	swingHigh             float64               // Swing High
	swingLow              float64               // Swing Low
	gridGap               float64               // Grid Gap/Size
	gridLevelCount        int8                  // Number of grid levels reached
	orderMap              map[int64]struct{}    // Map of order IDs
	mu                    sync.Mutex            // Mutex for thread-safe access to the orderMap
	gridNextBuyLevel      float64
	gridNextSellLevel     float64
	gridPreviousBuyLevel  float64
	gridPreviousSellLevel float64
}

func NewGridStrategy(db storage.Storage) backtest.Strategy[GridStrategy] {
	return &GridStrategy{
		name:               "grid",
		db:                 db,
		riskPercentage:     0.01,
		stopLossPercentage: 0.06,
		gridGap:            0,
	}
}

func (g *GridStrategy) ATR() float64 {
	atr := talib.Atr(g.highs, g.lows, g.closes, 14)
	lastAtr := atr[len(atr)-1]
	fmt.Printf("len(atr): %v, atr: %v\n", len(atr), atr[len(atr)-1])
	return lastAtr
}

func (g *GridStrategy) Execute() {
	g.GetClosePrices()
	g.ManageOrders()
}

func (g *GridStrategy) ManageOrders() {
	// Check if the price has reached the next buy or sell level
	currentPrice := g.GetClosePrice()
	g.UpdateGridLevels(currentPrice)

	if currentPrice >= g.gridNextBuyLevel {
		// Handle buy order logic

		// Close existing buy order (filled by exchange)
		g.OpenNewOrders() // Open new buy and sell orders

		logger.Info.Printf("incrementing grid level from: %v", g.gridLevelCount)
		g.gridLevelCount++
		logger.Info.Printf("new grid level: %v", g.gridLevelCount)
	} else if currentPrice <= g.gridNextSellLevel {
		// Handle sell order logic

		// Close existing sell order (filled by exchange)
		g.OpenNewOrders() // Open new buy and sell orders

		logger.Info.Printf("decrementing grid level from: %v", g.gridLevelCount)
		g.gridLevelCount--
		logger.Info.Printf("new grid level: %v", g.gridLevelCount)
	}
}

// Logic to place new buy and sell orders at the current grid level
func (g *GridStrategy) OpenNewOrders() {
	nextBuy := g.Buy(g.asset, g.gridNextBuyLevel)
	nextSell := g.Sell(g.asset, g.gridNextSellLevel)

	g.orders = append(g.orders, nextBuy, nextSell)

	for _, order := range g.orders {
		g.PlaceOrder(order)
	}
}

// Update grid levels and count based on current price and strategy logic
func (g *GridStrategy) UpdateGridLevels(currentPrice float64) {
	g.gridGap = g.ATR()
	g.gridNextBuyLevel = currentPrice + math.Abs(currentPrice-g.gridGap)
	g.gridNextSellLevel = currentPrice - math.Abs(currentPrice-g.gridGap)
	logger.Info.Printf(
		"gridGap: %v gridLevel: %v gridNextBuyLevel: %v gridNextSellLevel: %v\n",
		g.gridGap,
		g.gridLevelCount,
		g.gridNextBuyLevel,
		g.gridNextSellLevel,
	)

	if math.Abs(float64(g.gridLevelCount)) == float64(MAX_GRID_LEVEL) {
		logger.Debug.Printf("MAX_GRID_LEVEL [%d] reached!", MAX_GRID_LEVEL)
		g.CancelAllOpenOrders()
	} else {
		g.SetNewGridLevel(g.gridNextBuyLevel, g.gridNextSellLevel)
	}
}

func (g *GridStrategy) SetNewGridLevel(newBuyLevel, newSellLevel float64) {
	// Close all open orders if the price retraces to the previous level
	if newBuyLevel == g.gridPreviousBuyLevel || newSellLevel == g.gridPreviousSellLevel {
		logger.Info.Printf(
			"%.8f==%.8f || %.8f==%.8f\n",
			newBuyLevel,
			g.gridPreviousBuyLevel,
			newSellLevel,
			g.gridPreviousSellLevel,
		)
		g.gridPreviousBuyLevel = newBuyLevel
		g.gridPreviousSellLevel = newSellLevel
		g.CancelAllOpenOrders()
	}
}

func (g *GridStrategy) AddOpenOrder(orderID int64) {
	g.mu.Lock()
	g.orderMap[orderID] = struct{}{}
	g.mu.Unlock()
}

func (g *GridStrategy) CancelOrder(orderID int64) {
	g.mu.Lock()
	delete(g.orderMap, orderID)
	g.mu.Unlock()

	if _, err := rest.CancelOrder(g.asset, orderID); err != nil {
		logger.Error.Println("failed to close order:", err)
		return
	}
	logger.Info.Printf("OrderID=%v cancelled...", orderID)
}

func (g *GridStrategy) CancelAllOpenOrders() {
	logger.Info.Println("Cancelling all open orders.")
	for orderID := range g.orderMap {
		g.CancelOrder(orderID)
	}
}

func (g *GridStrategy) PlaceOrder(o models.TypeOfOrder) {
	currBar := g.data[len(g.data)-1]

	switch order := o.(type) {
	case *models.PostOrder:
		logger.Info.Printf("Side: %s, Quantity: %f, TakeProfit: %f, StopPrice: %f\n", order.Side, order.Quantity, order.Price, order.StopPrice)
		if g.backtest {
			order.Timestamp = currBar.OpenTime.UnixMilli()
			return
		}

		order.NewOrderRespType = models.OrderRespType("RESULT")
		orderResponse, err := rest.PostOrder(order)
		if err != nil {
			logger.Error.Printf("Failed to send order: %v", err)
			return
		}

		// After successfully placing an order
		g.AddOpenOrder(orderResponse.OrderID)

		// Saving to DB
		if err := g.db.SaveOrder(g.name, orderResponse); err != nil {
			logger.Error.Printf("Error saving order: %v", err)
		}
	default:
		// Some error occured during order creation
		return
	}
}

// BUY: Limit Price < Last Price < Stop Price
func (g *GridStrategy) Buy(asset string, price float64) models.TypeOfOrder {
	// Determine entry and stop loss prices
	entryPrice, stopLossPrice := g.DetermineEntryAndStopLoss("BUY", price)

	// Calculate position size
	var err error
	g.positionSize, err = g.CalculatePositionSize(
		g.asset,
		g.riskPercentage,
		entryPrice,
		stopLossPrice,
	)
	if err != nil {
		logger.Error.Println("Error calculating position size:", err)
		return nil
	}

	quantity, stopPrice, takeProfit, stopLimitPrice, riskAmount := g.calculateParams(
		"BUY",
		entryPrice,
		stopLossPrice,
		1.2,
	)

	logger.Debug.Println(
		"price =", price,
		"quantity =", quantity,
		"tp =", takeProfit,
		"sp =", stopPrice,
		"slp =", stopLimitPrice,
		"riska =", riskAmount,
		"swingh =", g.swingHigh,
		"swingl =", g.swingLow,
	)

	return &models.PostOrder{
		Symbol:      asset,
		Side:        models.BUY,
		Type:        models.MARKET,
		Quantity:    quantity,
		Price:       entryPrice,
		TimeInForce: models.GTC,
		RecvWindow:  5000,
		Timestamp:   time.Now().UnixMilli(),
	}
}

// SELL: Limit Price > Last Price > Stop Price
func (g *GridStrategy) Sell(asset string, price float64) models.TypeOfOrder {
	// Determine entry and stop loss prices
	entryPrice, stopLossPrice := g.DetermineEntryAndStopLoss("SELL", price)

	// Calculate position size
	var err error
	g.positionSize, err = g.CalculatePositionSize(
		g.asset,
		g.riskPercentage,
		entryPrice,
		stopLossPrice,
	)
	if err != nil {
		logger.Error.Println("Error calculating position size:", err)
		return nil
	}

	quantity, stopPrice, takeProfit, stopLimitPrice, riskAmount := g.calculateParams(
		"SELL",
		entryPrice,
		stopLossPrice,
		1.2,
	)

	logger.Debug.Println(
		"price =", price,
		"quantity =", quantity,
		"tp =", takeProfit,
		"sp =", stopPrice,
		"slp =", stopLimitPrice,
		"riska =", riskAmount,
		"swingh =", g.swingHigh,
		"swingl =", g.swingLow,
	)

	return &models.PostOrder{
		Symbol:      asset,
		Side:        models.SELL,
		Type:        models.MARKET,
		Quantity:    quantity,
		Price:       entryPrice,
		TimeInForce: models.GTC,
		RecvWindow:  5000,
		Timestamp:   time.Now().UnixMilli(),
	}
}

func (g *GridStrategy) IsBacktest(b bool) {
	g.backtest = b
}

func (g *GridStrategy) SetBalance(balance float64) {
	g.balance = balance
}

func (g *GridStrategy) GetBalance() float64 {
	return g.balance
}

func (g *GridStrategy) SetOrders(orders []models.TypeOfOrder) {
	g.orders = orders
}

func (g *GridStrategy) GetOrders() []models.TypeOfOrder {
	return g.orders
}

func (g *GridStrategy) SetPositionSize(ps float64) {
	g.positionSize += ps
}

func (g *GridStrategy) GetPositionSize() float64 {
	return g.positionSize
}

func (g *GridStrategy) SetData(data []*models.KlineSimple) {
	g.data = data
}

func (g *GridStrategy) SetAsset(asset string) {
	g.asset = strings.ToUpper(asset)
}

func (g *GridStrategy) GetName() string {
	return g.name
}

func (g *GridStrategy) GetClosePrices() {
	if len(g.closes) > 0 {
		fmt.Println("len(g.closes) > 0!!! ->", len(g.closes))

		currentBar := g.data[len(g.data)-1]
		g.closes = append(g.closes, currentBar.Close)
		g.highs = append(g.highs, currentBar.High)
		g.lows = append(g.lows, currentBar.Low)

		fmt.Println("last closePrice:", g.closes[len(g.closes)-1])
		return
	}
	for _, bar := range g.data {
		g.closes = append(g.closes, bar.Close)
		g.highs = append(g.highs, bar.High)
		g.lows = append(g.lows, bar.Low)
	}
}

func (g *GridStrategy) GetRecentHigh() {
	if len(g.highs) >= 10 {
		g.highs = g.highs[len(g.highs)-10:]
		max_h := g.highs[0]
		for i := 0; i < len(g.highs); i++ {
			if g.highs[i] > max_h {
				max_h = g.highs[i]
			}
		}
		g.swingHigh = max_h
	}
	// Keep the last 10 elements
	if len(g.highs) > 10 {
		g.highs = g.highs[1:]
	}
}

func (g *GridStrategy) GetRecentLow() {
	if len(g.lows) >= 10 {
		g.lows = g.lows[len(g.lows)-10:]
		min_l := g.lows[0]
		for i := 0; i < len(g.lows); i++ {
			if g.lows[i] < min_l {
				min_l = g.lows[i]
			}
		}
		g.swingLow = min_l
	}
	// Keep the last 10 elements
	if len(g.lows) > 10 {
		g.lows = g.lows[1:]
	}
}

func (g *GridStrategy) calculateParams(
	side string,
	currentPrice, stopPrice, riskRewardRatio float64,
) (float64, float64, float64, float64, float64) {
	panic("unimplemented")
}

func (m *GridStrategy) RoundToStepSize(value, stepSize float64) float64 {
	return math.Round(value/stepSize) * stepSize
}

func (m *GridStrategy) RoundToTickSize(value, tickSize float64) float64 {
	return math.Round(value/tickSize) * tickSize
}

// For leverage trading
// Position size = (account size x maximum risk percentage / (entry price – stop loss price)) x entry price
//
// For SPOT trading
// Invalidation point (distance to stop-loss)
// position size = account size x account risk / invalidation point
func (m *GridStrategy) CalculatePositionSize(
	asset string,
	riskPercentage, entryPrice, stopLossPrice float64,
) (float64, error) {
	panic("unimplemented")
}

func (g *GridStrategy) DetermineEntryAndStopLoss(
	side string,
	currentPrice float64,
) (float64, float64) {
	panic("unimplemented")
}

func (g *GridStrategy) GetClosePrice() float64 {
	return g.closes[len(g.closes)-1]
}
