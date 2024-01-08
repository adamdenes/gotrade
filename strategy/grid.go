package strategy

import (
	"context"
	"database/sql"
	"errors"
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
	"github.com/google/uuid"
	"github.com/markcheno/go-talib"
)

// Maximum reachable grid level ranging from 0 (base) to 4 (5 levels total).
// Acts as sort of a stop stop-loss.
type level int

const (
	invalidLevel         level = 100
	negativeMaxGridLevel level = iota - 5
	negativeBreakEvenLevel
	negativeHalfRetracementLevel
	negativeFullRetracementLevel
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
	logger.Debug.Printf("incrementing grid level from: %v", l.String())

	if *l == invalidLevel {
		*l = baseLevel
	} else if *l < maxGridLevel {
		*l++
	} else {
		*l = invalidLevel // Reset to base level if it goes beyond maxGridLevel
	}

	logger.Debug.Printf("new grid level: %v", l.String())
}

func (l *level) decreaseLevel() {
	logger.Debug.Printf("decrementing grid level from: %v", l.String())

	if *l == invalidLevel {
		*l = baseLevel
	} else if *l > negativeMaxGridLevel {
		*l--
	} else {
		*l = invalidLevel // Reset to base level if it goes beyond -maxGridLevel
	}

	logger.Debug.Printf("new grid level: %v", l.String())
}

const atrChangeThreshold = 0.15 // % change

type OrderInfo struct {
	ID           int64
	Symbol       string
	Side         models.OrderSide // "BUY" or "SELL"
	Type         models.OrderType // "LIMIT", "MARKET", etc.
	Status       models.OrderStatus
	CurrentPrice float64
	EntryPrice   float64
	SellLevel    float64
	GridLevel    level
}

type GridStrategy struct {
	name               string                       // Name of strategy
	db                 storage.Storage              // Database interface
	balance            float64                      // Current balance
	positionSize       float64                      // Position size
	riskPercentage     float64                      // Risk %
	stopLossPercentage float64                      // Invalidation point
	asset              string                       // Trading pair
	backtest           bool                         // Are we backtesting?
	orders             []models.TypeOfOrder         // Pending orders
	data               []*models.KlineSimple        // Price data
	closes             []float64                    // Close prices
	highs              []float64                    // Highs
	lows               []float64                    // Lows
	swingHigh          float64                      // Swing High
	swingLow           float64                      // Swing Low
	orderInfos         []*OrderInfo                 // Slice of orders
	mu                 sync.Mutex                   // Mutex for thread-safe access to the orderMap
	monitoring         bool                         // Monitoring started or not?
	rapidFill          bool                         // Rapid fill detection
	gridLevel          level                        // Number of grid levels reached
	previousGridLevel  level                        // Previous grid level
	levelChange        chan level                   // Notification channel
	gridGap            float64                      // Grid Gap/Size
	gridNextLowerLevel float64                      // Next grid line below
	gridNextUpperLevel float64                      // Next grid line above
	cancelFuncs        map[int64]context.CancelFunc // Cancels for monitoring go routines
	orderChannels      map[int64]chan struct{}      // Event listener for order fills
}

func NewGridStrategy(db storage.Storage) backtest.Strategy[GridStrategy] {
	g := &GridStrategy{
		name:               "grid",
		db:                 db,
		riskPercentage:     0.02,
		stopLossPercentage: 0.06,
		gridGap:            0,
		gridLevel:          invalidLevel,
		previousGridLevel:  invalidLevel,
		gridNextLowerLevel: -1,
		gridNextUpperLevel: -1,
		orderInfos:         make([]*OrderInfo, 0, 2),
		cancelFuncs:        make(map[int64]context.CancelFunc),
		orderChannels:      make(map[int64]chan struct{}),
		levelChange:        make(chan level, 10),
	}

	go g.ProcessLevels()

	return g
}

func (g *GridStrategy) NotifyLevelChange(newLevel level) {
	g.mu.Lock()
	defer g.mu.Unlock()

	logger.Debug.Printf(
		"[NotifyLevelChange] -> previousGridLevel: %v, currentGridLevel: %v",
		g.previousGridLevel,
		g.gridLevel,
	)
	if g.previousGridLevel != newLevel {
		g.levelChange <- newLevel
	}
}

func (g *GridStrategy) ATR() float64 {
	atr := talib.Atr(g.highs, g.lows, g.closes, 14)
	return atr[len(atr)-1]
}

func (g *GridStrategy) Execute() {
	g.GetClosePrices()
	g.ManageOrders()
}

func (g *GridStrategy) ProcessLevels() {
	for lvl := range g.levelChange {
		currentPrice := g.GetClosePrice()
		previousPrice := g.closes[len(g.closes)-2]

		logger.Debug.Printf("[ProcessLevels] -> %s [%d] reached!", lvl.String(), lvl)
		switch lvl {
		case invalidLevel, baseLevel:
			g.HandleGridLevelCross(currentPrice, previousPrice)

		case maxGridLevel, negativeMaxGridLevel:
			g.ResetGrid()

		default:
			g.CheckRetracement(currentPrice, previousPrice)
			g.HandleGridLevelCross(currentPrice, previousPrice)
		}
	}
}

func (g *GridStrategy) ManageOrders() {
	// Check if the price has reached the next buy or sell level
	currentPrice := g.GetClosePrice()
	previousPrice := g.closes[len(g.closes)-2]
	logger.Debug.Printf(
		"[ManageOrders] -> Current Price=%.8f, Previous Price=%.8f",
		currentPrice,
		previousPrice,
	)

	g.StartOrderMonitoring()
	g.UpdateGridLevels(currentPrice, previousPrice)

	// Only check for entries here if there are no open orders
	if len(g.orderInfos) == 0 {
		g.HandleGridLevelCross(currentPrice, previousPrice)
	}
}

func (g *GridStrategy) StartOrderMonitoring() {
	g.mu.Lock()
	if !g.monitoring {
		go g.HandleOrderChannels()
		g.monitoring = true
	}
	g.mu.Unlock()
}

func (g *GridStrategy) HandleGridLevelCross(
	currentPrice, previousPrice float64,
) {
	g.previousGridLevel = g.gridLevel

	if g.CrossUnder(currentPrice, previousPrice, g.gridNextLowerLevel) {
		// BUY low move
		logger.Debug.Printf(
			"**** [CrossUnder] current price [%v] <= [%v] lower level",
			currentPrice,
			g.gridNextLowerLevel,
		)
		g.gridLevel.decreaseLevel()
		g.NotifyLevelChange(g.gridLevel)
		g.OpenNewOrders()
	} else if g.CrossOver(currentPrice, previousPrice, g.gridNextUpperLevel) {
		// SELL high move
		logger.Debug.Printf(
			"**** [CrossOver] current price [%v] >= [%v] upper level",
			currentPrice,
			g.gridNextUpperLevel,
		)
		g.gridLevel.increaseLevel()
		g.NotifyLevelChange(g.gridLevel)
		g.OpenNewOrders()
	}
}

// Update grid levels and count based on current price and strategy logic
func (g *GridStrategy) UpdateGridLevels(currentPrice, previousPrice float64) {
	if g.gridNextLowerLevel == -1 && g.gridNextUpperLevel == -1 {
		// This is for the first run / after reset
		g.CreateGrid(currentPrice)
	} else {
		newATR := g.ATR() * 2
		atrChange := math.Abs(newATR-g.gridGap) / g.gridGap

		if atrChange > atrChangeThreshold {
			g.gridGap = newATR
			// Recalculate grid levels only if ATR change is significant
			g.SetNewGridLevels(currentPrice+g.gridGap, currentPrice-g.gridGap)

			logger.Debug.Printf(
				"[UpdateGridLevels] -> ATR change -> gridGap: %v gridLevel: %v gridNextBuyLevel: %v gridNextSellLevel: %v",
				g.gridGap,
				g.gridLevel,
				g.gridNextLowerLevel,
				g.gridNextUpperLevel,
			)
		}
	}
}

func (g *GridStrategy) HandleOrderChannels() {
	defer func() {
		g.mu.Lock()
		g.monitoring = false
		g.mu.Unlock()
	}()
	logger.Debug.Println("[HandleOrderChannels] called")

	for {
		g.mu.Lock()
		activeOrders := len(g.orderChannels) > 0
		g.mu.Unlock()

		if !activeOrders {
			time.Sleep(time.Second * 5) // Wait before checking again
			continue
		}

		for orderID, ch := range g.orderChannels {
			select {
			case <-ch:
				logger.Debug.Printf("Order [%v] finished.", orderID)
				// Handle the filled order
				g.HandleFinishedOrder(orderID)
			default:
				// Non-blocking: Do nothing if the channel has no signal yet
			}
		}
		// Sleep for a short duration before checking again
		time.Sleep(time.Millisecond * 100)
	}
}

func (g *GridStrategy) GetOrderInfo(orderID int64) *OrderInfo {
	for _, oi := range g.orderInfos {
		if orderID == oi.ID {
			return oi
		}
	}
	return nil
}

func (g *GridStrategy) HandleFinishedOrder(orderID int64) {
	oi := g.GetOrderInfo(orderID)
	if oi == nil {
		logger.Error.Println("Order not found.", orderID)
		return
	}

	// Open new orders after an order fill is detected
	g.previousGridLevel = g.gridLevel
	if oi.Side == "BUY" {
		g.gridLevel.decreaseLevel()
	} else {
		g.gridLevel.increaseLevel()
	}

	if err := g.RemoveOpenOrder(orderID); err != nil {
		logger.Error.Println("failed to remove order from slice:", err)
		return
	}

	g.mu.Lock()
	if _, exists := g.orderChannels[orderID]; exists {
		delete(g.orderChannels, orderID)
	}
	g.mu.Unlock()

	// The order was filled in the same interval as it was placed,
	// the grid calcualtion needs to be adjusted.
	if g.GetClosePrice() == oi.CurrentPrice {
		g.rapidFill = true
	}

	g.NotifyLevelChange(g.gridLevel)
	g.OpenNewOrders()
}

// Logic to place new buy and sell orders at the current grid level
func (g *GridStrategy) OpenNewOrders() {
	cp := g.GetClosePrice()
	nextBuy := g.Buy(g.asset, cp)
	nextSell := g.Sell(g.asset, cp)

	g.orders = append(g.orders, nextBuy, nextSell)

	for _, order := range g.orders {
		g.PlaceOrder(order)
		g.orders = g.orders[1:]
	}
}

func (g *GridStrategy) CheckRetracement(
	currentPrice, previousPrice float64,
) {
	if len(g.orderInfos) < 2 {
		logger.Info.Println("[CheckRetracement] -> Not enough orders to process retracement logic.")
		return
	}

	previousOrder := g.orderInfos[len(g.orderInfos)-2]
	currentOrder := g.orderInfos[len(g.orderInfos)-1]
	logger.Debug.Printf("Prev Order: %v", previousOrder)
	logger.Debug.Printf("Last Order: %v", currentOrder)

	fromLowerToUpper := g.CrossOver(currentPrice, previousPrice, currentOrder.EntryPrice)
	fromUpperToLower := g.CrossUnder(currentPrice, previousPrice, previousOrder.EntryPrice)

	if fromUpperToLower || g.previousGridLevel < g.gridLevel {
		// The market has retraced to a lower level from a previous sell order
		logger.Debug.Printf(
			"[CheckRetracement] -> [%s] -> retracement detected from UPPER to LOWER: %v crossed under %v | %v <-> %v",
			currentOrder.Side,
			currentPrice,
			previousOrder.EntryPrice,
			g.previousGridLevel,
			g.gridLevel,
		)
		g.ResetGrid()
	}

	if fromLowerToUpper || g.previousGridLevel > g.gridLevel {
		// The market has retraced to a higher level from a previous buy order
		logger.Debug.Printf(
			"[CheckRetracement] -> [%s] -> retracement detected from LOWER to UPPER: %v crossed over %v | %v <-> %v",
			previousOrder.Side,
			currentPrice,
			currentOrder.EntryPrice,
			g.previousGridLevel,
			g.gridLevel,
		)
		g.ResetGrid()
	}
}

func (g *GridStrategy) SetNewGridLevels(newUpper, newLower float64) {
	g.gridNextUpperLevel = newUpper
	g.gridNextLowerLevel = newLower
	// logger.Debug.Printf(
	// 	"[SetNewGridLevels] -> Setting upper/lower -> u: %v, l: %v",
	// 	g.gridNextUpperLevel,
	// 	g.gridNextLowerLevel,
	// )
}

// MonitorOrder checks the database periodically for updates to the order.
func (g *GridStrategy) MonitorOrder(ctx context.Context, oi *OrderInfo) chan struct{} {
	ch := make(chan struct{})

	logger.Debug.Printf("[MonitorOrder] -> Starting to monitor OrderID: %v", oi.ID)
	go func(c context.Context) {
		defer close(ch)

		for {
			select {
			case <-c.Done():
				logger.Debug.Printf(
					"Context cancelled for OrderID: %v",
					oi.ID,
				)
				return
			default:
				o, err := g.db.GetOrder(oi.ID)
				if err != nil {
					if errors.Is(err, sql.ErrNoRows) {
						// Minotoring not started yet (not in DB)
						time.Sleep(5 * time.Second)
						continue

					}
					logger.Error.Println(err)
					return
				}

				if o.Status == "FILLED" || o.Status == "CANCELED" {
					logger.Debug.Printf("Order %v %v", o.OrderID, o.Status)
					g.mu.Lock()
					oi.Status = o.Status
					g.mu.Unlock()
					return
				}

				// Check the order status periodically
				time.Sleep(5 * time.Second)
			}
		}
	}(ctx)

	return ch
}

func (g *GridStrategy) CrossOver(currentPrice, previousPrice, threshold float64) bool {
	return previousPrice <= threshold && currentPrice > threshold
}

func (g *GridStrategy) CrossUnder(currentPrice, previousPrice, threshold float64) bool {
	return previousPrice >= threshold && currentPrice < threshold
}

func (g *GridStrategy) CreateGrid(currentPrice float64) {
	g.gridGap = g.ATR() * 2

	if g.rapidFill {
		g.gridGap += g.gridGap
		logger.Debug.Println("[CreateGrid] -> Rapid fill detected, grid gap doubled")
	}

	g.SetNewGridLevels(currentPrice+g.gridGap, currentPrice-g.gridGap)

	logger.Debug.Printf(
		"[CreateGrid] -> price: %v, gridGap: %v, gridLevel: %v, gridNextLowerLevel: %v, gridNextUpperLevel: %v",
		currentPrice,
		g.gridGap,
		g.gridLevel,
		g.gridNextLowerLevel,
		g.gridNextUpperLevel,
	)

	// Reset the rapidFill flag after grid creation
	g.rapidFill = false
}

func (g *GridStrategy) ResetGrid() {
	g.CancelAllOpenOrders()
	g.balance = 0.0
	g.positionSize = 0.0
	g.gridNextLowerLevel = -1
	g.gridNextUpperLevel = -1
	g.gridLevel = invalidLevel
	g.previousGridLevel = invalidLevel
	g.monitoring = false
	g.rapidFill = false
	g.orderInfos = make([]*OrderInfo, 0, 2)
	g.orders = make([]models.TypeOfOrder, 0)
	g.cancelFuncs = make(map[int64]context.CancelFunc)
	g.orderChannels = make(map[int64]chan struct{})
	g.levelChange = make(chan level, 10)
	logger.Debug.Println("[ResetGrid] -> Grid has been reset")
}

func (g *GridStrategy) AddOpenOrder(oi *OrderInfo) {
	g.mu.Lock()
	g.orderInfos = append(g.orderInfos, oi)

	if _, exists := g.orderChannels[oi.ID]; !exists {
		ctx, cancel := context.WithCancel(context.Background())
		g.cancelFuncs[oi.ID] = cancel
		g.orderChannels[oi.ID] = g.MonitorOrder(ctx, oi)
	}

	g.mu.Unlock()
}

func (g *GridStrategy) RemoveOpenOrder(orderID int64) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	for i, order := range g.orderInfos {
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
	if cancel, exists := g.cancelFuncs[orderID]; exists {
		cancel()                       // This will signal the monitoring goroutine to stop
		delete(g.cancelFuncs, orderID) // Clean up
	}

	for _, order := range g.orderInfos {
		if order.ID == orderID {
			if order.Status == "FILLED" || order.Status == "CANCELED" {
				logger.Debug.Println("[CancelOrder] -> Order already canceled.", orderID)
				return
			}
		}
	}

	co, err := rest.CancelOrder(g.asset, "", orderID)
	if err != nil {
		logger.Error.Println("failed to close order:", err)
		return
	}
	logger.Debug.Printf(
		"[CancelOrder] -> OrderID=%v Symbol=%v Status=%v cancelled...",
		co.OrderID,
		co.Symbol,
		co.Status,
	)

	// Monitoring should take care of it
	_ = g.db.UpdateOrder(co.DeleteToGet())
}

func (g *GridStrategy) CancelAllOpenOrders() {
	logger.Debug.Println("[CancelAllOpenOrders] -> Cancelling all open orders.")
	for _, oi := range g.orderInfos {
		g.CancelOrder(oi.ID)
	}
}

func (g *GridStrategy) PlaceOrder(o models.TypeOfOrder) {
	currBar := g.data[len(g.data)-1]

	switch order := o.(type) {
	case *models.PostOrder:
		logger.Debug.Printf("[PlaceOrder] -> Side: %s, Symbol: %s Quantity: %f, TakeProfit: %f, StopPrice: %f", order.Side, order.Symbol, order.Quantity, order.Price, order.StopPrice)
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
			ID:           orderResponse.OrderID,
			Symbol:       orderResponse.Symbol,
			Side:         orderResponse.Side,
			Status:       orderResponse.Status,
			Type:         orderResponse.Type,
			CurrentPrice: g.GetClosePrice(),
			EntryPrice:   order.Price,
			SellLevel:    stop,
			GridLevel:    g.gridLevel,
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

	return &models.PostOrder{
		Symbol:           asset,
		Side:             models.BUY,
		Type:             models.LIMIT,
		NewClientOrderId: uuid.NewString(),
		Quantity:         quantity,
		Price:            entryPrice,
		StopPrice:        stopPrice,
		TimeInForce:      models.GTC,
		RecvWindow:       5000,
		Timestamp:        time.Now().UnixMilli(), // will be overwritten by GetServerTime
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

	return &models.PostOrder{
		Symbol:           asset,
		Side:             models.SELL,
		Type:             models.LIMIT,
		NewClientOrderId: uuid.NewString(),
		Quantity:         quantity,
		Price:            entryPrice,
		StopPrice:        stopPrice,
		TimeInForce:      models.GTC,
		RecvWindow:       5000,
		Timestamp:        time.Now().UnixMilli(), // will be overwritten by GetServerTime
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

func (g *GridStrategy) calculateParams(
	asset string,
	entryPrice, stopPrice float64,
) (float64, float64, float64, error) {
	stepSize, tickSize, minNotional, err := g.getFilters(asset)
	if err != nil {
		return 0, 0, 0, err
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
		"[CalculatePositionSize] -> Position size: $%.8f, Account balance: %.2f, Risk: %.2f%%, Entry: %.8f, Stop-Loss: %.8f",
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

	// Recalculate the grid based on the current price.
	// If order fills too fast, the currentPrice is not updated yet,
	// hence the same grid will be created. To avoid this, increase the
	// levels with the gap size again.
	g.CreateGrid(currentPrice)

	if side == "SELL" {
		entryPrice = g.gridNextUpperLevel // Entry price for sell order
		stopPrice = g.gridNextLowerLevel  // Stop-loss for sell order
	} else if side == "BUY" {
		entryPrice = g.gridNextLowerLevel // Entry price for buy order
		stopPrice = g.gridNextUpperLevel  // Stop-loss for buy order
	}

	// logger.Debug.Printf(
	// 	"[DetermineEntryAndStopLoss] -> side: %v upper: %v lower: %v",
	// 	side,
	// 	entryPrice,
	// 	stopPrice,
	// )
	return entryPrice, stopPrice
}

func (g *GridStrategy) GetClosePrice() float64 {
	return g.closes[len(g.closes)-1]
}
