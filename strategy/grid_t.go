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
	"github.com/markcheno/go-talib"
)

type GridTrailingStrategy struct {
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
	atr                []float64                    // ATR slice
	swingHigh          float64                      // Swing High
	swingLow           float64                      // Swing Low
	orderInfos         []*models.OrderInfo          // Slice of orders
	mu                 sync.Mutex                   // Mutex for thread-safe access to the orderMap
	monitoring         bool                         // Monitoring started or not?
	rapidFill          bool                         // Rapid fill detection
	lastFillPrice      float64                      // Last filled orders price
	gridLevel          models.LevelType             // Number of grid levels reached
	previousGridLevel  models.Level                 // Previous grid level
	levelChange        chan models.Level            // Notification channel
	gridGap            float64                      // Grid Gap/Size
	gridNextLowerLevel float64                      // Next grid line below
	gridNextUpperLevel float64                      // Next grid line above
	cancelFuncs        map[int64]context.CancelFunc // Cancels for monitoring go routines
	orderChannels      map[int64]chan struct{}      // Event listener for order fills
	trailingDelta      int                          // 100 BIPs: Equals 1% (0.01 in decimal)
}

func NewGridTrailingStrategy(db storage.Storage) backtest.Strategy[GridTrailingStrategy] {
	g := &GridTrailingStrategy{
		name:               "grid_t",
		db:                 db,
		riskPercentage:     0.02,
		stopLossPercentage: 0.06,
		trailingDelta:      100,
		gridGap:            0,
		gridLevel:          models.LevelType{Val: models.InvalidLevel},
		previousGridLevel:  models.InvalidLevel,
		gridNextLowerLevel: -1,
		gridNextUpperLevel: -1,
		orderInfos:         make([]*models.OrderInfo, 0, 2),
		cancelFuncs:        make(map[int64]context.CancelFunc),
		orderChannels:      make(map[int64]chan struct{}),
		levelChange:        make(chan models.Level, 15),
		atr:                make([]float64, 0, 14),
	}

	go g.ProcessLevels()

	return g
}

func (g *GridTrailingStrategy) NotifyLevelChange(newLevel models.Level) {
	// logger.Debug.Printf(
	// 	"[NotifyLevelChange] -> previousGridLevel: %v, currentGridLevel: %v",
	// 	g.previousGridLevel,
	// 	newLevel,
	// )
	g.levelChange <- newLevel
}

func (g *GridTrailingStrategy) ATR() {
	if len(g.closes) > 14 {
		atr := talib.Atr(g.highs, g.lows, g.closes, 14)
		g.atr = append(g.atr, atr[len(atr)-1])
	}
	if len(g.atr) > 3 {
		g.atr = g.atr[1:]
	}
}

func (g *GridTrailingStrategy) getAtr() float64 {
	return g.atr[len(g.atr)-1]
}

func (g *GridTrailingStrategy) Execute() {
	g.GetClosePrices()
	g.ATR()
	g.ManageOrders()
}

func (g *GridTrailingStrategy) ProcessLevels() {
	for {
		select {
		case lvl := <-g.levelChange:
			currentPrice := g.GetClosePrice()
			previousPrice := g.closes[len(g.closes)-2]

			if g.previousGridLevel != lvl {
				logger.Debug.Printf(
					"[ProcessLevels] -> p/c: [%v/%v]",
					g.previousGridLevel,
					lvl,
				)

				logger.Debug.Printf("[ProcessLevels] -> %s [%d] reached!", lvl.String(), lvl)
				switch lvl {
				case models.InvalidLevel:
					g.HandleGridLevelCross(currentPrice, previousPrice)

				case models.MaxGridLevel, models.NegativeMaxGridLevel:
					g.ResetGrid()

				default:
					if g.CheckRetracement() {
						g.ResetGrid()
					} else {
						g.OpenNewOrders()
					}
				}
			}
		}
	}
}

func (g *GridTrailingStrategy) ManageOrders() {
	var previousPrice float64
	// Check if the price has reached the next buy or sell level
	currentPrice := g.GetClosePrice()
	if g.backtest {
		if len(g.atr) < 1 {
			return
		}
		previousPrice = g.closes[len(g.closes)-2]
	} else {
		previousPrice = g.closes[len(g.closes)-2]
	}
	// logger.Debug.Printf(
	// 	"[ManageOrders] -> Current Price=%.8f, Previous Price=%.8f, Balance=%.2f",
	// 	currentPrice,
	// 	previousPrice,
	// 	g.balance,
	// )

	// Reset the rapidFill flag after new close price arrives
	g.rapidFill = false
	g.lastFillPrice = 0

	g.StartOrderMonitoring()
	g.UpdateGridLevels(currentPrice, previousPrice)

	// Only check for entries here if there are no open orders
	if len(g.orderInfos) == 0 {
		g.HandleGridLevelCross(currentPrice, previousPrice)
	}
}

func (g *GridTrailingStrategy) StartOrderMonitoring() {
	if !g.monitoring {
		go g.HandleOrderChannels()
		g.monitoring = true
	}
}

func (g *GridTrailingStrategy) HandleGridLevelCross(
	currentPrice, previousPrice float64,
) {
	g.previousGridLevel = g.gridLevel.Val

	if g.CrossUnder(currentPrice, previousPrice, g.gridNextLowerLevel) {
		// BUY low move
		logger.Debug.Printf(
			"**** [CrossUnder] current price [%v] <= [%v] lower level",
			currentPrice,
			g.gridNextLowerLevel,
		)
		g.gridLevel.DecreaseLevel()
	} else if g.CrossOver(currentPrice, previousPrice, g.gridNextUpperLevel) {
		// SELL high move
		logger.Debug.Printf(
			"**** [CrossOver] current price [%v] >= [%v] upper level",
			currentPrice,
			g.gridNextUpperLevel,
		)
		g.gridLevel.IncreaseLevel()
	}

	g.NotifyLevelChange(g.gridLevel.Val)
}

// Update grid levels and count based on current price and strategy logic
func (g *GridTrailingStrategy) UpdateGridLevels(currentPrice, previousPrice float64) {
	if g.gridNextLowerLevel == -1 && g.gridNextUpperLevel == -1 {
		// This is for the first run / after reset
		g.CreateGrid(currentPrice)
	} else {
		newATR := g.getAtr()
		atrChange := math.Abs(newATR-g.gridGap) / g.gridGap

		if atrChange > atrChangeThreshold {
			g.gridGap = newATR
			// Recalculate grid levels only if ATR change is significant
			g.SetNewGridLevels(currentPrice+g.gridGap, currentPrice-g.gridGap)

			logger.Debug.Printf(
				"[UpdateGridLevels] -> ATR change -> gridGap: %v gridLevel: %v gridNextBuyLevel: %v gridNextSellLevel: %v",
				g.gridGap,
				g.gridLevel.Val,
				g.gridNextLowerLevel,
				g.gridNextUpperLevel,
			)
		}
	}
}

func (g *GridTrailingStrategy) HandleOrderChannels() {
	defer func() {
		g.monitoring = false
	}()

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

func (g *GridTrailingStrategy) GetOrderInfo(orderID int64) *models.OrderInfo {
	for _, oi := range g.orderInfos {
		if orderID == oi.ID {
			return oi
		}
	}
	return nil
}

func (g *GridTrailingStrategy) HandleFinishedOrder(orderID int64) {
	oi := g.GetOrderInfo(orderID)
	if oi == nil {
		logger.Error.Println("Order not found.", orderID)
		return
	}

	g.lastFillPrice = oi.EntryPrice

	// Open new orders after an order fill is detected
	g.previousGridLevel = g.gridLevel.Val

	if oi.Side == "BUY" {
		g.gridLevel.DecreaseLevel()
	} else {
		g.gridLevel.IncreaseLevel()
	}

	if err := g.RemoveOpenOrder(orderID); err != nil {
		logger.Error.Println("failed to remove order from slice:", err)
		return
	}

	if _, exists := g.orderChannels[orderID]; exists {
		delete(g.orderChannels, orderID)
	}

	// The order was filled in the same interval as it was placed,
	// the grid calcualtion needs to be adjusted.
	if g.GetClosePrice() == oi.CurrentPrice {
		g.rapidFill = true
	}

	g.NotifyLevelChange(g.gridLevel.Val)
}

// Logic to place new buy and sell orders at the current grid level
func (g *GridTrailingStrategy) OpenNewOrders() {
	cp := g.GetClosePrice()
	nextBuy := g.Buy(g.asset, cp)
	nextSell := g.Sell(g.asset, cp)

	g.orders = append(g.orders, nextBuy, nextSell)

	for _, order := range g.orders {
		if err := g.PlaceOrder(order); err != nil {
			logger.Error.Println("[OpenNewOrders] -> error:", err)
			return
		}
		g.orders = g.orders[1:]
	}
}

func (g *GridTrailingStrategy) CheckRetracement() bool {
	isRetracing := false

	if len(g.orderInfos) < 1 {
		logger.Debug.Println(
			"[CheckRetracement] -> Not enough orders to process retracement logic.",
		)
		return isRetracing
	}

	prevOrder := g.orderInfos[len(g.orderInfos)-1]
	prevLvl := math.Abs(float64(g.previousGridLevel))
	currLvl := math.Abs(float64(g.gridLevel.Val))

	// logger.Debug.Printf("[CheckRetracement] -> Previous Order: %v", prevOrder)

	switch prevOrder.Side {
	case "SELL":
		if prevLvl > currLvl {
			// The market has retraced to a lower level from a previous sell
			logger.Debug.Printf(
				"[CheckRetracement] -> [BUY] retracement detected, previous: %v -> %v :current",
				g.previousGridLevel,
				g.gridLevel.Val,
			)
			isRetracing = true
		} else if prevLvl < currLvl {
			logger.Debug.Printf(
				"[CheckRetracement] -> [BUY] progressing downwards, previous: %v -> %v :current",
				g.previousGridLevel,
				g.gridLevel.Val,
			)
			isRetracing = false
		} else {
			logger.Debug.Printf(
				"[CheckRetracement] -> No level change, previous: %v -> %v :current",
				g.previousGridLevel,
				g.gridLevel.Val,
			)
			isRetracing = false
		}
	case "BUY":
		if prevLvl > currLvl {
			// The market has retraced to an upper level from a previous buy
			logger.Debug.Printf(
				"[CheckRetracement] -> [SELL] retracement detected, previous: %v -> %v :current",
				g.previousGridLevel,
				g.gridLevel.Val,
			)
			isRetracing = true
		} else if prevLvl < currLvl {
			logger.Debug.Printf(
				"[CheckRetracement] -> [SELL] progressing upwards, previous: %v -> %v :current",
				g.previousGridLevel,
				g.gridLevel.Val,
			)
			isRetracing = false
		} else {
			logger.Debug.Printf(
				"[CheckRetracement] -> No level change, previous: %v -> %v :current",
				g.previousGridLevel,
				g.gridLevel.Val,
			)
			isRetracing = false
		}
	}
	return isRetracing
}

func (g *GridTrailingStrategy) SetNewGridLevels(newUpper, newLower float64) {
	g.gridNextUpperLevel = newUpper
	g.gridNextLowerLevel = newLower
	// logger.Debug.Printf(
	// 	"[SetNewGridLevels] -> Setting upper/lower -> u: %v, l: %v",
	// 	g.gridNextUpperLevel,
	// 	g.gridNextLowerLevel,
	// )
}

// MonitorOrder checks the database periodically for updates to the order.
func (g *GridTrailingStrategy) MonitorOrder(
	ctx context.Context,
	oi *models.OrderInfo,
) chan struct{} {
	ch := make(chan struct{})

	logger.Debug.Printf("[MonitorOrder] -> Starting to monitor OrderID: %v", oi.ID)
	go func(c context.Context) {
		defer close(ch)

		for {
			select {
			case <-c.Done():
				logger.Debug.Printf(
					"[MonitorOrder] -> Context cancelled for OrderID: %v",
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

				if o.Status == "FILLED" {
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

func (g *GridTrailingStrategy) CrossOver(currentPrice, previousPrice, threshold float64) bool {
	return previousPrice <= threshold && currentPrice > threshold
}

func (g *GridTrailingStrategy) CrossUnder(currentPrice, previousPrice, threshold float64) bool {
	return previousPrice >= threshold && currentPrice < threshold
}

func (g *GridTrailingStrategy) CreateGrid(currentPrice float64) {
	g.gridGap = g.getAtr()

	if g.rapidFill {
		currentPrice = g.lastFillPrice
		logger.Debug.Printf(
			"[CreateGrid] -> Rapid fill detected, using last fill price: %v",
			currentPrice,
		)
		g.SetNewGridLevels(currentPrice+g.gridGap, currentPrice-g.gridGap)
	} else {
		g.SetNewGridLevels(currentPrice+g.gridGap, currentPrice-g.gridGap)
	}

	logger.Debug.Printf(
		"[CreateGrid] -> price: %v, gridGap: %v, gridLevel: %v, gridNextLowerLevel: %v, gridNextUpperLevel: %v",
		currentPrice,
		g.gridGap,
		g.gridLevel.Val,
		g.gridNextLowerLevel,
		g.gridNextUpperLevel,
	)
}

func (g *GridTrailingStrategy) ResetGrid() {
	g.CancelAllOpenOrders()
	g.balance = 0.0
	g.positionSize = 0.0
	g.lastFillPrice = 0.0
	g.gridNextLowerLevel = -1
	g.gridNextUpperLevel = -1
	g.gridLevel = models.LevelType{Val: models.InvalidLevel}
	g.previousGridLevel = models.InvalidLevel
	g.monitoring = false
	g.rapidFill = false
	g.orderInfos = make([]*models.OrderInfo, 0, 2)
	g.orders = make([]models.TypeOfOrder, 0)
	g.cancelFuncs = make(map[int64]context.CancelFunc)
	g.orderChannels = make(map[int64]chan struct{})
	g.levelChange = make(chan models.Level, 15)
	logger.Debug.Println("[ResetGrid] -> Grid has been reset")
}

func (g *GridTrailingStrategy) AddOpenOrder(oi *models.OrderInfo) {
	g.orderInfos = append(g.orderInfos, oi)

	if _, exists := g.orderChannels[oi.ID]; !exists {
		ctx, cancel := context.WithCancel(context.Background())
		g.cancelFuncs[oi.ID] = cancel
		g.orderChannels[oi.ID] = g.MonitorOrder(ctx, oi)
	}
}

func (g *GridTrailingStrategy) RemoveOpenOrder(orderID int64) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	for i, order := range g.orderInfos {
		if order.ID == orderID {
			// logger.Debug.Printf("REMOVING order: %v", order)
			// Remove the order at index i from g.orderInfos
			g.orderInfos = append(g.orderInfos[:i], g.orderInfos[i+1:]...)
			return nil
		}
	}

	return fmt.Errorf("OrderID=%v not found!", orderID)
}

func (g *GridTrailingStrategy) CancelOrder(orderID int64) {
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
	// logger.Debug.Printf(
	// 	"[CancelOrder] -> OrderID=%v Symbol=%v Status=%v cancelled...",
	// 	co.OrderID,
	// 	co.Symbol,
	// 	co.Status,
	// )

	// Monitoring should take care of it
	_ = g.db.UpdateOrder(co.DeleteToGet())
}

func (g *GridTrailingStrategy) CancelAllOpenOrders() {
	logger.Debug.Println("[CancelAllOpenOrders] -> Cancelling all open orders.")
	for _, oi := range g.orderInfos {
		g.CancelOrder(oi.ID)
	}
}

func (g *GridTrailingStrategy) PlaceOrder(o models.TypeOfOrder) error {
	currBar := g.data[len(g.data)-1]

	switch order := o.(type) {
	case *models.PostOrder:
		logger.Debug.Printf("[PlaceOrder] -> Side: %s, Symbol: %s Quantity: %f, TakeProfit: %f, StopPrice: %f, TrailingDelta: %d",
			order.Side, order.Symbol, order.Quantity, order.Price, order.StopPrice, g.trailingDelta)
		if g.backtest {
			order.Timestamp = currBar.OpenTime.UnixMilli()
			return nil
		}

		order.NewOrderRespType = models.OrderRespType("RESULT")
		orderResponse, err := rest.PostOrder(order)
		if err != nil {
			logger.Error.Printf("Failed to send order: %v", err)
			return fmt.Errorf("Failed to send order: %v", err)
		}

		// After successfully placing an order
		orderInfo := &models.OrderInfo{
			ID:           orderResponse.OrderID,
			Symbol:       orderResponse.Symbol,
			Side:         orderResponse.Side,
			Status:       orderResponse.Status,
			Type:         orderResponse.Type,
			CurrentPrice: g.GetClosePrice(),
			EntryPrice:   order.Price,
			SellLevel:    order.StopPrice,
			GridLevel:    g.gridLevel.Val,
		}
		g.AddOpenOrder(orderInfo)

		// Saving to DB
		if err := g.db.SaveOrder(g.name, orderResponse); err != nil {
			logger.Error.Printf("Error saving order: %v", err)
			return fmt.Errorf("Error saving order: %v", err)
		}
	default:
		// Some error occured during order creation
		logger.Error.Println("Error, not placing order!")
		return fmt.Errorf("Error, not placing order!")
	}

	return nil
}

func (g *GridTrailingStrategy) Buy(asset string, price float64) models.TypeOfOrder {
	// Determine entry and stop loss prices
	limitPrice, stopPrice := g.DetermineEntryAndStopLoss("BUY", price)

	// Calculate position size
	var err error
	g.positionSize, err = g.CalculatePositionSize(
		g.asset,
		g.riskPercentage,
		limitPrice,
		stopPrice,
	)
	if err != nil {
		logger.Error.Println("error calculating position size:", err)
		return nil
	}

	quantity, limitPrice, stopPrice, td, err := g.calculateParams(
		"BUY",
		asset,
		limitPrice,
		stopPrice,
	)
	if err != nil {
		logger.Error.Println("error calculating order parameters:", err)
		return nil
	}

	return &models.PostOrder{
		Symbol:        asset,
		Side:          models.BUY,
		Type:          models.TAKE_PROFIT_LIMIT,
		Quantity:      quantity,
		Price:         limitPrice, // buy limit order placed at this price (can fill higher!)
		StopPrice:     stopPrice,  // activate trailingDelta here
		TrailingDelta: td,
		TimeInForce:   models.GTC,
		RecvWindow:    5000,
		Timestamp:     time.Now().UnixMilli(), // will be overwritten by GetServerTime
	}
}

func (g *GridTrailingStrategy) Sell(asset string, price float64) models.TypeOfOrder {
	// Determine entry and stop loss prices
	limitPrice, stopPrice := g.DetermineEntryAndStopLoss("SELL", price)

	// Calculate position size
	var err error
	g.positionSize, err = g.CalculatePositionSize(
		g.asset,
		g.riskPercentage,
		limitPrice,
		stopPrice,
	)
	if err != nil {
		logger.Error.Println("Error calculating position size:", err)
		return nil
	}

	quantity, limitPrice, stopPrice, td, err := g.calculateParams(
		"SELL",
		asset,
		limitPrice,
		stopPrice,
	)
	if err != nil {
		logger.Error.Println("error calculating order parameters:", err)
		return nil
	}

	return &models.PostOrder{
		Symbol:        asset,
		Side:          models.SELL,
		Type:          models.TAKE_PROFIT_LIMIT,
		Quantity:      quantity,
		Price:         limitPrice, // sell limit order placed at this price (can fill higher!)
		StopPrice:     stopPrice,  // activate trailingDelta here
		TrailingDelta: td,
		TimeInForce:   models.GTC,
		RecvWindow:    5000,
		Timestamp:     time.Now().UnixMilli(), // will be overwritten by GetServerTime
	}
}

func (g *GridTrailingStrategy) IsBacktest(b bool) {
	g.backtest = b
}

func (g *GridTrailingStrategy) SetBalance(balance float64) {
	g.balance = balance
}

func (g *GridTrailingStrategy) GetBalance() float64 {
	return g.balance
}

func (g *GridTrailingStrategy) SetOrders(orders []models.TypeOfOrder) {
	g.orders = orders
}

func (g *GridTrailingStrategy) GetOrders() []models.TypeOfOrder {
	return g.orders
}

func (g *GridTrailingStrategy) SetPositionSize(ps float64) {
	g.positionSize += ps
}

func (g *GridTrailingStrategy) GetPositionSize() float64 {
	return g.positionSize
}

func (g *GridTrailingStrategy) SetData(data []*models.KlineSimple) {
	g.data = data
}

func (g *GridTrailingStrategy) SetAsset(asset string) {
	g.asset = strings.ToUpper(asset)
}

func (g *GridTrailingStrategy) GetName() string {
	return g.name
}

func (g *GridTrailingStrategy) GetClosePrices() {
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

func (g *GridTrailingStrategy) GetRecentHigh() {
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

func (g *GridTrailingStrategy) GetRecentLow() {
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

// calculateDynamicTrailingDelta calculates a dynamic trailing delta to ensure
// the difference between the stop and limit price is adequate.
// It returns the adjusted trailing delta in BIPS.
func (g *GridTrailingStrategy) calculateDynamicTrailingDelta(
	limitPrice float64,
	stopPrice float64,
) int64 {
	// Calculate the desired gap between stop and limit price
	desiredGap := math.Abs(stopPrice - limitPrice)

	logger.Debug.Printf(
		"[calculateDynamicTrailingDelta] -> desiredGap: %v, stopPrice: %v, limitPrice: %v",
		desiredGap,
		stopPrice,
		limitPrice,
	)

	// Calculate the trailing delta as a percentage of the limit price
	// and convert it to BIPS
	// Decreasing the delta by 0.5% so the limit order can execute.
	g.trailingDelta = int(math.Abs((desiredGap / limitPrice) * 10000))
	logger.Debug.Printf("[calculateDynamicTrailingDelta] -> delta: %v", g.trailingDelta)

	return int64(g.trailingDelta)
}

// Adjusting trailingDelta based on order type and filter values
func (g *GridTrailingStrategy) adjustToTrailingFilter(
	side string,
	tf *models.TrailingDeltaFilter,
) int64 {
	switch side {
	case "BUY":
		if g.trailingDelta < tf.MinTrailingAboveDelta {
			g.trailingDelta = tf.MinTrailingAboveDelta
		} else if g.trailingDelta > tf.MaxTrailingAboveDelta {
			g.trailingDelta = tf.MaxTrailingAboveDelta
		}
	case "SELL":
		if g.trailingDelta < tf.MinTrailingBelowDelta {
			g.trailingDelta = tf.MinTrailingBelowDelta
		} else if g.trailingDelta > tf.MaxTrailingBelowDelta {
			g.trailingDelta = tf.MaxTrailingBelowDelta
		}
	}

	return int64(g.trailingDelta)
}

func (g *GridTrailingStrategy) adjustToNotinalFilter(
	limitPrice, stepSize, minNotional float64,
) float64 {
	quantity := g.GetPositionSize() / limitPrice

	if quantity*limitPrice < minNotional {
		// logger.Error.Println("price * quantity is too low to be a valid order for the symbol")
		quantity = minNotional/limitPrice + stepSize
		// logger.Warning.Printf(
		// 	"Adjusting Quantity to [%.8f] based on minNotional of [%0.8f]",
		// 	quantity,
		// 	minNotional,
		// )
	}
	return g.RoundToStepSize(quantity, stepSize)
}

func (g *GridTrailingStrategy) adjustToMinHighestPrice(
	side string, stopPrice, tickSize float64, trailingDelta int64,
) float64 {
	tdDecimal := float64(trailingDelta) / 10000.0 // Convert BIPS to decimal

	if side == "SELL" {
		// Calculate the minimum price to sell after trailing stop is activated
		minPriceAfterTrailing := stopPrice / (1 - tdDecimal)
		logger.Warning.Printf(
			"[%s] -> Adjusting stop price from [%0.8f] to [%0.8f] to account for [%v]%% trailing delta on SELL order",
			side,
			stopPrice,
			minPriceAfterTrailing,
			trailingDelta/100,
		)
		stopPrice = minPriceAfterTrailing
	} else if side == "BUY" {
		// Calculate the maximum price to buy after trailing stop is activated
		maxPriceAfterTrailing := stopPrice / (1 + tdDecimal)
		logger.Warning.Printf(
			"[%s] -> Adjusting limit price from [%0.8f] to [%0.8f] to account for [%v]%% trailing delta on BUY order",
			side,
			stopPrice,
			maxPriceAfterTrailing,
			trailingDelta/100,
		)
		stopPrice = maxPriceAfterTrailing
	}
	return g.RoundToTickSize(stopPrice, tickSize)
}

func (g *GridTrailingStrategy) calculateParams(
	side, asset string,
	limitPrice, stopPrice float64,
) (float64, float64, float64, int64, error) {
	filters, err := g.db.GetTradeFilters(asset)
	if err != nil {
		logger.Error.Printf("Error fetching trade filters: %v", err)
		return 0, 0, 0, 0, err
	}

	stepSize, err := strconv.ParseFloat(filters.LotSizeFilter.StepSize, 64)
	if err != nil {
		logger.Error.Printf("Error parsing step size: %v", err)
		return 0, 0, 0, 0, err
	}

	tickSize, err := strconv.ParseFloat(filters.PriceFilter.TickSize, 64)
	if err != nil {
		logger.Error.Printf("Error parsing tick size: %v", err)
		return 0, 0, 0, 0, err
	}

	minNotional, err := strconv.ParseFloat(filters.NotionalFilter.MinNotional, 64)
	if err != nil {
		logger.Error.Printf("Error parsing tick size: %v", err)
		return 0, 0, 0, 0, err
	}

	limitPrice = g.RoundToTickSize(limitPrice, tickSize)
	stopPrice = g.RoundToTickSize(stopPrice, tickSize)

	trailingDelta := g.adjustToTrailingFilter(side, &filters.TrailingDeltaFilter)
	stopPrice = g.adjustToMinHighestPrice(side, limitPrice, tickSize, trailingDelta)
	// trailingDelta = g.calculateDynamicTrailingDelta(limitPrice, stopPrice)
	quantity := g.adjustToNotinalFilter(limitPrice, stepSize, minNotional)

	return quantity, limitPrice, stopPrice, trailingDelta, nil
}

func (g *GridTrailingStrategy) RoundToStepSize(value, stepSize float64) float64 {
	return math.Round(value/stepSize) * stepSize
}

func (g *GridTrailingStrategy) RoundToTickSize(value, tickSize float64) float64 {
	return math.Round(value/tickSize) * tickSize
}

// For leverage trading
// Position size = (account size x maximum risk percentage / (entry price – stop loss price)) x entry price
//
// For SPOT trading
// Invalidation point (distance to stop-loss)
// position size = account size x account risk / invalidation point
func (g *GridTrailingStrategy) CalculatePositionSize(
	asset string,
	riskPercentage, entryPrice, stopLossPrice float64,
) (float64, error) {
	if !g.backtest {
		balance, err := rest.GetBalance(asset)
		if err != nil {
			return 0.0, err
		}
		g.balance = balance
	}

	positionSize := g.balance * riskPercentage
	logger.Debug.Printf(
		"[CalculatePositionSize] -> Position size: $%.8f, Account balance: %.2f, Risk: %.2f%%, Limit: %.8f, Stop-Loss: %.8f, TrailingDelta: %d%%",
		positionSize,
		g.balance,
		riskPercentage*100,
		entryPrice,
		stopLossPrice,
		g.trailingDelta/100,
	)
	return positionSize, nil
}

func (g *GridTrailingStrategy) DetermineEntryAndStopLoss(
	side string,
	currentPrice float64,
) (float64, float64) {
	// StopPrice is only for debugging, not used due to the nature of grid strategy
	var entryPrice, stopPrice float64

	// Recalculate the grid based on the current price.
	g.CreateGrid(currentPrice)

	if side == "SELL" {
		stopPrice = g.gridNextUpperLevel     // Stop-loss for trailing delta should be higher then limit price
		entryPrice = stopPrice - g.gridGap/2 // Limit price will be decided later
	} else if side == "BUY" {
		stopPrice = g.gridNextLowerLevel     // Stop-loss for trailing delta should be higher then limit price
		entryPrice = stopPrice + g.gridGap/2 // Limit price will be decided later
	}
	return entryPrice, stopPrice
}

func (g *GridTrailingStrategy) GetClosePrice() float64 {
	return g.closes[len(g.closes)-1]
}
