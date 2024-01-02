package strategy

import (
	"fmt"
	"math"
	"strconv"
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

// Maximum reachable grid level ranging from 0 (base) to 4 (5 levels total).
// Acts as sort of a stop stop-loss.
type level int

const (
    invalidLevel level = 100
	negativeMaxGridLevel= iota - 5
    negativeBreakEvenLevel
    negativeFullRetracementLevel
    negativeHalfRetracementLevel
	baseLevel
	fullRetracementLevel
	halfRetracementLevel
	breakEvenLevel
	maxGridLevel
)

func (l level) String() string {
	switch l {
	case negativeMaxGridLevel:
		return "-MAX_GRID_LEVEL"
	case negativeBreakEvenLevel:
		return "-BREAK_EVEN_LEVEL"
	case negativeHalfRetracementLevel:
		return "-HALF_RETRACEMENT_LEVEL"
	case negativeFullRetracementLevel:
		return "-FULL_RETRACEMENT_LEVEL"
	case baseLevel:
		return "BASE_LEVEL"
	case fullRetracementLevel:
		return "FULL_RETRACEMENT_LEVEL"
	case halfRetracementLevel:
		return "HALF_RETRACEMENT_LEVEL"
	case breakEvenLevel:
		return "BREAK_EVEN_LEVEL"
	case maxGridLevel:
		return "MAX_GRID_LEVEL"
	case invalidLevel:
		return "INVALID_LEVEL"
    default:
        return fmt.Sprintf("Unknown Level [%d]", l)
    }
}

func (l *level) increaseLevel() {
    if *l == invalidLevel {
        *l = baseLevel
    } else if *l < maxGridLevel {
        *l++
    } else {
        *l = invalidLevel // Reset to base level if it goes beyond maxGridLevel
    }
}

func (l *level) decreaseLevel() {
    if *l == invalidLevel {
        *l = baseLevel
    } else if *l > negativeMaxGridLevel {
        *l--
    } else {
        *l = invalidLevel // Reset to base level if it goes beyond -maxGridLevel
    }
}


const atrChangeThreshold = 0.15 // % change

type OrderInfo struct {
	ID             int64
	Symbol         string
	Side           models.OrderSide // "BUY" or "SELL"
	Type           models.OrderType // "LIMIT", "MARKET", etc.
    Status         models.OrderStatus
	EntryPrice     float64
	SellLevel      float64
	GridLevelCount level
}

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
	orderInfos            []*OrderInfo          // Slice of orders
	mu                    sync.Mutex            // Mutex for thread-safe access to the orderMap
	gridGap               float64               // Grid Gap/Size
	gridLevelCount        level                 // Number of grid levels reached
	gridNextBuyLevel      float64               // Next grid line above
	gridNextSellLevel     float64               // Next grid line below
}

func NewGridStrategy(db storage.Storage) backtest.Strategy[GridStrategy] {
	return &GridStrategy{
		name:                  "grid",
		db:                    db,
		riskPercentage:        0.02,
		stopLossPercentage:    0.06,
		gridGap:               0,
        gridLevelCount: invalidLevel,
		gridNextBuyLevel:      -1,
		gridNextSellLevel:     -1,
		orderInfos:            make([]*OrderInfo, 0, 2),
	}
}

func (g *GridStrategy) ATR() float64 {
	atr := talib.Atr(g.highs, g.lows, g.closes, 14)
	return atr[len(atr)-1]
}

func (g *GridStrategy) Execute() {
	g.GetClosePrices()
    g.ProcessLevels()
}

func (g *GridStrategy) ProcessLevels() {
	lvl := level(math.Abs(float64(g.gridLevelCount)))
	switch lvl {
	case maxGridLevel:
		logger.Warning.Printf("%s [%d] reached!", level(maxGridLevel).String(), maxGridLevel)
		g.ResetGrid()
	default:
		logger.Debug.Printf("%s [%d] reached!", lvl.String(), lvl)
        g.ManageOrders()
	}
}

func (g *GridStrategy) ManageOrders() {
	// Check if the price has reached the next buy or sell level
	currentPrice := g.GetClosePrice()
	logger.Debug.Printf("Current Price=%.8f", currentPrice)
	g.UpdateGridLevels(currentPrice)

	if currentPrice <= g.gridNextBuyLevel {
        logger.Debug.Printf("**** current price [%v] <= [%v] lower level", currentPrice, g.gridNextBuyLevel)
		// Handle buy order logic
		// Close existing buy order (filled by exchange)
		g.OpenNewOrders()

		logger.Debug.Printf("decrementing grid level from: %v", g.gridLevelCount)
        g.gridLevelCount.decreaseLevel()
		logger.Debug.Printf("new grid level: %v", g.gridLevelCount)
	} else if currentPrice >= g.gridNextSellLevel {
        logger.Debug.Printf("**** current price [%v] >= [%v] upper level", currentPrice, g.gridNextBuyLevel)
		// Handle sell order logic
		// Close existing sell order (filled by exchange)
		g.OpenNewOrders()

		logger.Debug.Printf("incrementing grid level from: %v", g.gridLevelCount)
        g.gridLevelCount.increaseLevel()		
		logger.Debug.Printf("new grid level: %v", g.gridLevelCount)
	}
}

// Logic to place new buy and sell orders at the current grid level
func (g *GridStrategy) OpenNewOrders() {
    cp := g.GetClosePrice()
	nextBuy := g.Buy(g.asset, cp)
	nextSell := g.Sell(g.asset, cp)

	g.orders = append(g.orders, nextBuy, nextSell)

	for i, order := range g.orders {
		g.PlaceOrder(order)
        // Prevent sending same order multiple times
        fmt.Println("removing:", g.orders[i:], "i:", i)
        g.orders = g.orders[i:]
	}
}

// Update grid levels and count based on current price and strategy logic
func (g *GridStrategy) UpdateGridLevels(currentPrice float64) {
	if g.gridNextBuyLevel == -1 && g.gridNextSellLevel == -1 {
        // This is for the first run / after reset
		g.CreateGrid(currentPrice)
	} else {
		newATR := g.ATR() * 2
		atrChange := math.Abs(newATR-g.gridGap) / g.gridGap

		if atrChange > atrChangeThreshold {
			g.gridGap = newATR
			// Recalculate grid levels only if ATR change is significant
			g.gridNextBuyLevel = currentPrice + g.gridGap
			g.gridNextSellLevel = currentPrice - g.gridGap

			logger.Debug.Printf(
				"ATR change -> gridGap: %v gridLevel: %v gridNextBuyLevel: %v gridNextSellLevel: %v",
				g.gridGap,
				g.gridLevelCount,
				g.gridNextBuyLevel,
				g.gridNextSellLevel,
			)
		}
	}

	g.CheckRetracement(g.gridNextBuyLevel, g.gridNextSellLevel)
}

func (g *GridStrategy) CheckRetracement(newBuyLevel, newSellLevel float64) {
	// 2 orders are created at each level
	if len(g.orderInfos) >= 2 {
		// Last orders
		prevBuyOrder := g.orderInfos[len(g.orderInfos)-2]
		prevSellOrder := g.orderInfos[len(g.orderInfos)-1]
		logger.Debug.Println("Last Buy    (buy low):", prevBuyOrder)
		logger.Debug.Println("Last Sell (sell high):", prevSellOrder)

		// Check for retracement to previous levels
		// if (prevSellOrder.Side == "SELL" && newSellLevel == prevSellOrder.EntryPrice) ||
		// 	(prevBuyOrder.Side == "BUY" && newBuyLevel == prevBuyOrder.EntryPrice) {
		// 	logger.Debug.Printf("Retracement detected: %.8f==%.8f || %.8f==%.8f",
		// 		newBuyLevel, prevSellOrder.EntryPrice, newSellLevel, prevBuyOrder.EntryPrice)
		// 	g.ResetGrid()
		// }

        // Last close prices
        currentPrice := g.GetClosePrice()
        previousPrice := g.closes[len(g.closes)-1]

        buyCrossover := previousPrice >= prevBuyOrder.EntryPrice && currentPrice < prevBuyOrder.EntryPrice
        sellCrossover := previousPrice <= prevSellOrder.EntryPrice && currentPrice > prevSellOrder.EntryPrice

        if (prevSellOrder.Side == "SELL" && sellCrossover) || (prevBuyOrder.Side == "BUY" && buyCrossover) {
            logger.Debug.Printf("Retracement detected near: %.8f or %.8f", prevBuyOrder.EntryPrice, prevSellOrder.EntryPrice)
            g.ResetGrid()
        }
	} 
}

func (g *GridStrategy) CreateGrid(currentPrice float64) {
	g.gridGap = g.ATR() * 2
	g.gridNextBuyLevel = currentPrice - g.gridGap // buy low
	g.gridNextSellLevel = currentPrice + g.gridGap // sell high
	logger.Debug.Printf(
        "price: %v gridGap: %v gridLevel: %v gridNextBuyLevel: %v gridNextSellLevel: %v",
        currentPrice,
		g.gridGap,
		g.gridLevelCount,
		g.gridNextBuyLevel,
		g.gridNextSellLevel,
	)
}

func (g *GridStrategy) ResetGrid() {
	g.CancelAllOpenOrders()
	g.balance = 0.0
	g.positionSize = 0.0
	g.gridNextBuyLevel = -1
	g.gridNextSellLevel = -1
	g.gridLevelCount = invalidLevel
	g.orderInfos = make([]*OrderInfo, 0, 2)
	logger.Debug.Println("Grid has been reset")
}

func (g *GridStrategy) AddOpenOrder(oi *OrderInfo) {
	g.mu.Lock()
	g.orderInfos = append(g.orderInfos, oi)
	g.mu.Unlock()
}

func (g *GridStrategy) RemoveOpenOrder(orderID int64) error {
    g.mu.Lock()
    defer g.mu.Unlock()

    for i, order := range g.orderInfos {
        logger.Debug.Printf("ORDER: %v", order)
        if order.ID == orderID {
            logger.Debug.Printf("REMOVING order: %v", order)
            // Remove the order at index i from g.orderInfos
            g.orderInfos = append(g.orderInfos[:i], g.orderInfos[i+1:]...)
            return nil
        }
    }
    
    return fmt.Errorf("OrderID=%v not found!", orderID)
}

func (g *GridStrategy) CancelOrder(orderID int64) {
    if err := g.RemoveOpenOrder(orderID); err != nil {
		logger.Error.Println("failed to remove order from slice:", err)
		return
    }

	co, err := rest.CancelOrder(g.asset, orderID)
	if err != nil {
		logger.Error.Println("failed to close order:", err)
		return
	}
	logger.Debug.Printf(
		"OrderID=%v Symbol=%v Status=%v cancelled...",
		co.OrderID,
		co.Symbol,
		co.Status,
	)

    // Monitoring should take care of it
    // _ = g.db.UpdateOrder(co.DeleteToGet())
}

func (g *GridStrategy) CancelAllOpenOrders() {
	logger.Debug.Println("Cancelling all open orders.")
	for _, oi := range g.orderInfos {
		g.CancelOrder(oi.ID)
	}
}

func (g *GridStrategy) PlaceOrder(o models.TypeOfOrder) {
	currBar := g.data[len(g.data)-1]

	switch order := o.(type) {
	case *models.PostOrder:
		logger.Debug.Printf("Side: %s, Symbol: %s Quantity: %f, TakeProfit: %f, StopPrice: %f", order.Side, order.Symbol, order.Quantity, order.Price, order.StopPrice)
		if g.backtest {
			order.Timestamp = currBar.OpenTime.UnixMilli()
			return
		}

        // Limit order can't have stop price
        stop := order.StopPrice
        order.StopPrice = 0.0

		order.NewOrderRespType = models.OrderRespType("RESULT")
		orderResponse, err := rest.PostOrder(order)
		if err != nil {
			logger.Error.Printf("Failed to send order: %v", err)
			return
		}

		// After successfully placing an order
		orderInfo := &OrderInfo{
			ID:             orderResponse.OrderID,
			Symbol:         orderResponse.Symbol,
			Side:           orderResponse.Side,
            Status:         orderResponse.Status,
			Type:           orderResponse.Type,
			EntryPrice:     order.Price,
			SellLevel:      stop,
			GridLevelCount: g.gridLevelCount,
		}
		g.AddOpenOrder(orderInfo)

		// Saving to DB
		if err := g.db.SaveOrder(g.name, orderResponse); err != nil {
			logger.Error.Printf("Error saving order: %v", err)
		}
	default:
		// Some error occured during order creation
		logger.Error.Println("Error, not placing order!")
		return
	}
}

func (g *GridStrategy) Buy(asset string, price float64) models.TypeOfOrder {
	// Determine entry and stop loss prices
	entryPrice, stopPrice := g.DetermineEntryAndStopLoss("BUY", price)

	// Calculate position size
	var err error
	g.positionSize, err = g.CalculatePositionSize(
		g.asset,
		g.riskPercentage,
		entryPrice,
		stopPrice,
	)
	if err != nil {
		logger.Error.Println("error calculating position size:", err)
		return nil
	}

	quantity, entryPrice, stopPrice, err := g.calculateParams(asset, entryPrice, stopPrice)
	if err != nil {
		logger.Error.Println("error calculating order parameters:", err)
		return nil
	}

	logger.Debug.Println(
		"price =", price,
		"quantity =", quantity,
		"tp =", entryPrice,
		"sp =", stopPrice,
		"riska =", math.Abs(entryPrice-stopPrice),
	)

	return &models.PostOrder{
		Symbol:      asset,
		Side:        models.BUY,
		Type:        models.LIMIT,
		Quantity:    quantity,
		Price:       entryPrice,
        StopPrice:   stopPrice,
		TimeInForce: models.GTC,
		RecvWindow:  5000,
		Timestamp:   time.Now().UnixMilli(), // will be overwritten by GetServerTime
	}
}

func (g *GridStrategy) Sell(asset string, price float64) models.TypeOfOrder {
	// Determine entry and stop loss prices
	entryPrice, stopPrice := g.DetermineEntryAndStopLoss("SELL", price)

	// Calculate position size
	var err error
	g.positionSize, err = g.CalculatePositionSize(
		g.asset,
		g.riskPercentage,
		entryPrice,
		stopPrice,
	)
	if err != nil {
		logger.Error.Println("Error calculating position size:", err)
		return nil
	}

	quantity, entryPrice, stopPrice, err := g.calculateParams(asset, entryPrice, stopPrice)
	if err != nil {
		logger.Error.Println("error calculating order parameters:", err)
		return nil
	}

	logger.Debug.Println(
		"price =", price,
		"quantity =", quantity,
		"tp =", entryPrice,
		"sp =", stopPrice,
		"riska =", math.Abs(entryPrice-stopPrice),
	)

	return &models.PostOrder{
		Symbol:      asset,
		Side:        models.SELL,
		Type:        models.LIMIT,
		Quantity:    quantity,
		Price:       entryPrice,
        StopPrice:   stopPrice,
		TimeInForce: models.GTC,
		RecvWindow:  5000,
		Timestamp:   time.Now().UnixMilli(), // will be overwritten by GetServerTime
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
		currentBar := g.data[len(g.data)-1]
		g.closes = append(g.closes, currentBar.Close)
		g.highs = append(g.highs, currentBar.High)
		g.lows = append(g.lows, currentBar.Low)

		if len(g.closes) > 365 {
			g.closes = g.closes[len(g.closes)-365:]
			g.highs = g.highs[len(g.highs)-365:]
			g.lows = g.lows[len(g.lows)-365:]
		}
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

func (g *GridStrategy) getFilters(asset string) (float64, float64, float64, error) {
	filters, err := g.db.GetTradeFilters(asset)
	if err != nil {
		logger.Error.Printf("Error fetching trade filters: %v", err)
		return 0, 0, 0, err
	}

	stepSize, _ := strconv.ParseFloat(filters.LotSizeFilter.StepSize, 64)
	tickSize, _ := strconv.ParseFloat(filters.PriceFilter.TickSize, 64)
	minNotional, _ := strconv.ParseFloat(filters.NotionalFilter.MinNotional, 64)

    return stepSize, tickSize, minNotional, nil
}

func (g *GridStrategy) calculateParams(asset string, entryPrice, stopPrice float64) (float64, float64, float64, error) {
    stepSize, tickSize, minNotional, err := g.getFilters(asset)
    if err != nil {
        return 0,0,0, err
    }

	quantity := g.GetPositionSize() / entryPrice
	quantity = g.RoundToStepSize(quantity, stepSize)
	entryPrice = g.RoundToTickSize(entryPrice, tickSize)
	stopPrice = g.RoundToTickSize(stopPrice, tickSize)

	if quantity*entryPrice < minNotional {
		logger.Error.Println("price * quantity is too low to be a valid order for the symbol")
		quantity = minNotional / entryPrice
		quantity = g.RoundToStepSize(quantity, stepSize)
		logger.Warning.Printf(
			"Adjusting Quantity to [%.8f] based on minNotional of [%0.8f]",
			quantity,
			minNotional,
		)
	}

	return quantity, entryPrice, stopPrice, nil
}

func (g *GridStrategy) RoundToStepSize(value, stepSize float64) float64 {
	return math.Round(value/stepSize) * stepSize
}

func (g *GridStrategy) RoundToTickSize(value, tickSize float64) float64 {
	return math.Round(value/tickSize) * tickSize
}

// For leverage trading
// Position size = (account size x maximum risk percentage / (entry price – stop loss price)) x entry price
//
// For SPOT trading
// Invalidation point (distance to stop-loss)
// position size = account size x account risk / invalidation point
func (g *GridStrategy) CalculatePositionSize(
	asset string,
	riskPercentage, entryPrice, stopLossPrice float64,
) (float64, error) {
	if !g.backtest {
		var err error
		g.balance, err = rest.GetBalance(asset)
		if err != nil {
			return 0.0, err
		}
	}

	positionSize := g.balance * riskPercentage
	logger.Debug.Printf(
		"Position size: $%.8f, Account balance: %.2f, Risk: %.2f%%, Entry: %.8f, Stop-Loss: %.8f",
		positionSize,
		g.balance,
		riskPercentage*100,
		entryPrice,
		stopLossPrice,
	)
	return positionSize, nil
}

func (g *GridStrategy) DetermineEntryAndStopLoss(
	side string,
	currentPrice float64,
) (float64, float64) {
	// StopPrice is only for debugging, not used due to the nature of grid strategy
	var entryPrice, stopPrice float64

	// Recalculate the grid based on the current price
	g.CreateGrid(currentPrice)

	if side == "SELL" {
		entryPrice = g.gridNextSellLevel// Entry price for sell order
		stopPrice = g.gridNextBuyLevel // Stop-loss for sell order
	} else if side == "BUY" {
		entryPrice = g.gridNextBuyLevel // Entry price for buy order
		stopPrice = g.gridNextSellLevel   // Stop-loss for buy order
	}

	return entryPrice, stopPrice
}

func (g *GridStrategy) GetClosePrice() float64 {
	return g.closes[len(g.closes)-1]
}
