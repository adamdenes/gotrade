package strategy

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/adamdenes/gotrade/cmd/rest"
	"github.com/adamdenes/gotrade/internal/backtest"
	"github.com/adamdenes/gotrade/internal/logger"
	"github.com/adamdenes/gotrade/internal/models"
	"github.com/adamdenes/gotrade/internal/storage"
	"github.com/markcheno/go-talib"
)

type MACDStrategy struct {
	name               string                // Name of strategy
	db                 storage.Storage       // Database interface
	balance            float64               // Current balance
	positionSize       float64               // Position size
	riskPercentage     float64               // Risk %
	orderLimit         int                   // Number of allowed trades
	stopLossPercentage float64               // Invalidation point
	asset              string                // Trading pair
	backtest           bool                  // Are we backtesting?
	orders             []models.TypeOfOrder  // Pending orders
	data               []*models.KlineSimple // Price data
	closePrices        []float64
	macd               []float64
	macdsignal         []float64
	macdhist           []float64
	ema200             []float64
	highs              []float64
	lows               []float64
	swingHigh          float64
	swingLow           float64
}

func NewMACDStrategy(orderLimit int, db storage.Storage) backtest.Strategy[MACDStrategy] {
	return &MACDStrategy{
		name:               "macd",
		db:                 db,
		riskPercentage:     0.01,
		stopLossPercentage: 0.1,
		orderLimit:         orderLimit,
	}
}

func (m *MACDStrategy) Execute() {
	m.GetClosePrices()

	currBar := m.data[len(m.data)-1]
	m.highs = append(m.highs, currBar.High)
	m.lows = append(m.lows, currBar.Low)
	m.closePrices = append(m.closePrices, currBar.Close)
	currentPrice := m.closePrices[len(m.closePrices)-1]

	// Get swing high and low from the nearest 10 bar highs and lows
	m.GetRecentHigh()
	m.GetRecentLow()
	// Get entry based on MACD & EMA200
	m.EMA()
	m.MACD()

	if len(m.ema200) > 0 {
		ema200 := m.ema200[len(m.ema200)-1]
		var order models.TypeOfOrder

		if currentPrice > ema200 {
			// Buy condition:
			// - current price is above the EMA 200
			// - macd line crosses over signal line
			// - histogram is "green" (above the zero line)
			// - macd crossover happens under the zero line
			if talib.Crossover(m.macd, m.macdsignal) && m.macdhist[len(m.macdhist)-1] > 0 &&
				m.macd[len(m.macd)-1] < 0 {
				// Generate a "SELL" signal
				order = m.Sell(m.asset, currentPrice)
			}
		} else if currentPrice < ema200 {
			// Sell condition:
			// - current price is below the EMA 200
			// - macd line crosses under signal line
			// - histogram is "red" (below the zero line)
			// - macd crossover happens above zero line
			if talib.Crossunder(m.macd, m.macdsignal) && m.macdhist[len(m.macdhist)-1] < 0 &&
				m.macd[len(m.macd)-1] > 0 {
				// Generate a "BUY" signal
				order = m.Buy(m.asset, currentPrice)
			}
		}
		m.PlaceOrder(order)
	}
}

// Wrapper over talib.Macd
func (m *MACDStrategy) MACD() {
	// Wait till slow period + signal period
	if len(m.closePrices) >= 34 {
		macd, macdsignal, macdhist := talib.Macd(m.closePrices, 8, 13, 5)
		m.macd = append(m.macd, macd[len(macd)-1])
		m.macdsignal = append(m.macdsignal, macdsignal[len(macdsignal)-1])
		m.macdhist = append(m.macdhist, macdhist[len(macdhist)-1])
	}
	if len(m.macd) > 3 {
		m.macd = m.macd[1:]
		m.macdsignal = m.macdsignal[1:]
		m.macdhist = m.macdhist[1:]
	}
}

// Wrapper over talib.EMA for 200 ema
func (m *MACDStrategy) EMA() {
	// Calculate EMA 200
	if len(m.closePrices) >= 200 {
		// Calculate EMA 200 and truncate to have exactly 200 elements
		ema200 := talib.Ema(m.closePrices, 200)[len(m.closePrices)-1:]
		m.ema200 = append(m.ema200, ema200...)
	}
	if len(m.ema200) > 2 {
		m.ema200 = m.ema200[1:]
	}
}

func (m *MACDStrategy) PlaceOrder(o models.TypeOfOrder) {
	currBar := m.data[len(m.data)-1]

	switch order := o.(type) {
	case *models.PostOrder:
		if m.orderLimit >= len(m.orders) {
			logger.Info.Printf("Side: %s, Quantity: %f, TakeProfit: %f, StopPrice: %f\n", order.Side, order.Quantity, order.Price, order.StopPrice)
			if m.backtest {
				order.Timestamp = currBar.OpenTime.UnixMilli()
				m.orders = append(m.orders, order)
				return
			}

			order.NewOrderRespType = models.OrderRespType("RESULT")
			orderResponse, err := rest.PostOrder(order)
			if err != nil {
				logger.Error.Printf("Failed to send order: %v", err)
				return
			}
			if err := m.db.SaveOrder(m.name, orderResponse); err != nil {
				logger.Error.Printf("Error saving order: %v", err)
			}
		} else {
			logger.Error.Printf("No more orders allowed! Current limit is: %v", m.orderLimit)
		}
	case *models.PostOrderOCO:
		if m.orderLimit >= len(m.orders) {
			logger.Info.Printf("Side: %s, Quantity: %f, TakeProfit: %f, StopPrice: %f, StopLimitPrice: %f\n", order.Side, order.Quantity, order.Price, order.StopPrice, order.StopLimitPrice)
			if m.backtest {
				order.Timestamp = currBar.OpenTime.UnixMilli()
				m.orders = append(m.orders, order)
				return
			}

			ocoResponse, err := rest.PostOrderOCO(order)
			if err != nil {
				logger.Error.Printf("Failed to send OCO order: %v", err)
				return
			}

			for _, resp := range ocoResponse.OrderReports {
				if err := m.db.SaveOrder(m.name, &resp); err != nil {
					logger.Error.Printf("Error saving OCO order: %v", err)
				}
			}
		} else {
			logger.Error.Printf("No more orders allowed! Current limit is: %v", m.orderLimit)
		}
	default:
		// Some error occured during order creation
		return
	}
}

// BUY: Limit Price < Last Price < Stop Price
func (m *MACDStrategy) Buy(asset string, price float64) models.TypeOfOrder {
	// Determine entry and stop loss prices
	entryPrice, stopLossPrice := m.DetermineEntryAndStopLoss("BUY", price)

	// Calculate position size
	var err error
	m.positionSize, err = m.CalculatePositionSize(
		m.asset,
		m.riskPercentage,
		entryPrice,
		stopLossPrice,
	)
	if err != nil {
		logger.Error.Println("Error calculating position size:", err)
		return nil
	}

	quantity, stopPrice, takeProfit, stopLimitPrice, riskAmount := m.calculateParams(
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
		"swingh =", m.swingHigh,
		"swingl =", m.swingLow,
	)

	return &models.PostOrderOCO{
		Symbol:               asset,
		Side:                 models.BUY,
		Quantity:             quantity,
		Price:                takeProfit,     // Price to buy (Take Profit)
		StopPrice:            stopPrice,      // Where to start stop loss
		StopLimitPrice:       stopLimitPrice, // Highest price you want to sell coins
		StopLimitTimeInForce: models.StopLimitTimeInForce(models.GTC),
		RecvWindow:           5000,
		Timestamp:            time.Now().UnixMilli(),
	}
}

// SELL: Limit Price > Last Price > Stop Price
func (m *MACDStrategy) Sell(asset string, price float64) models.TypeOfOrder {
	// Determine entry and stop loss prices
	entryPrice, stopLossPrice := m.DetermineEntryAndStopLoss("SELL", price)

	// Calculate position size
	var err error
	m.positionSize, err = m.CalculatePositionSize(
		m.asset,
		m.riskPercentage,
		entryPrice,
		stopLossPrice,
	)
	if err != nil {
		logger.Error.Println("Error calculating position size:", err)
		return nil
	}

	quantity, stopPrice, takeProfit, stopLimitPrice, riskAmount := m.calculateParams(
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
		"swingh =", m.swingHigh,
		"swingl =", m.swingLow,
	)

	return &models.PostOrderOCO{
		Symbol:               asset,
		Side:                 models.SELL,
		Quantity:             quantity,
		Price:                takeProfit,     // Price to sell (Take Profit)
		StopPrice:            stopPrice,      // Where to start stop loss
		StopLimitPrice:       stopLimitPrice, // Lowest price you want to buy coins
		StopLimitTimeInForce: models.StopLimitTimeInForce(models.GTC),
		RecvWindow:           5000,
		Timestamp:            time.Now().UnixMilli(),
	}
}

func (m *MACDStrategy) IsBacktest(b bool) {
	m.backtest = b
}

func (m *MACDStrategy) SetBalance(balance float64) {
	m.balance = balance
}

func (m *MACDStrategy) GetBalance() float64 {
	return m.balance
}

func (m *MACDStrategy) SetOrders(orders []models.TypeOfOrder) {
	m.orders = orders
}

func (m *MACDStrategy) GetOrders() []models.TypeOfOrder {
	return m.orders
}

func (m *MACDStrategy) SetPositionSize(ps float64) {
	m.positionSize += ps
}

func (m *MACDStrategy) GetPositionSize() float64 {
	return m.positionSize
}

func (m *MACDStrategy) SetData(data []*models.KlineSimple) {
	m.data = data
}

func (m *MACDStrategy) SetAsset(asset string) {
	m.asset = strings.ToUpper(asset)
}

func (m *MACDStrategy) GetName() string {
	return m.name
}

func (m *MACDStrategy) GetClosePrices() {
	if len(m.closePrices) > 0 {
		return
	}
	for _, bar := range m.data {
		m.closePrices = append(m.closePrices, bar.Close)
		m.highs = append(m.highs, bar.High)
		m.lows = append(m.lows, bar.Low)
	}
}

func (m *MACDStrategy) GetRecentHigh() {
	if len(m.highs) >= 10 {
		m.highs = m.highs[len(m.highs)-10:]
		max_h := m.highs[0]
		for i := 0; i < len(m.highs); i++ {
			if m.highs[i] > max_h {
				max_h = m.highs[i]
			}
		}
		m.swingHigh = max_h
	}
	// Keep the last 10 elements
	if len(m.highs) > 10 {
		m.highs = m.highs[1:]
	}
}

func (m *MACDStrategy) GetRecentLow() {
	if len(m.lows) >= 10 {
		m.lows = m.lows[len(m.lows)-10:]
		min_l := m.lows[0]
		for i := 0; i < len(m.lows); i++ {
			if m.lows[i] < min_l {
				min_l = m.lows[i]
			}
		}
		m.swingLow = min_l
	}
	// Keep the last 10 elements
	if len(m.lows) > 10 {
		m.lows = m.lows[1:]
	}
}

func (m *MACDStrategy) calculateParams(
	side string,
	currentPrice, stopPrice, riskRewardRatio float64,
) (float64, float64, float64, float64, float64) {
	var quantity, takeProfit, stopLimitPrice, riskAmount float64

	// Get trade filters
	filters, err := m.db.GetTradeFilters(m.asset)
	if err != nil {
		logger.Error.Printf("Error fetching trade filters: %v", err)
		return 0, 0, 0, 0, 0
	}

	// Calculate quantity based on position size
	stepSize, _ := strconv.ParseFloat(filters.LotSizeFilter.StepSize, 64)
	quantity = m.GetPositionSize() / currentPrice
	quantity = m.RoundToStepSize(quantity, stepSize)

	// Calculate risk amount, takeProfit, and stopLimitPrice based on strategy
	riskAmount = math.Abs(currentPrice - stopPrice)

	if side == "SELL" {
		// SELL: Limit Price > Last Price > Stop Price
		takeProfit = currentPrice + (riskAmount * riskRewardRatio)
		if takeProfit <= currentPrice {
			takeProfit = currentPrice * (1 + m.stopLossPercentage)
		}
		stopLimitPrice = stopPrice * 0.995
	} else if side == "BUY" {
		// BUY: Limit Price < Last Price < Stop Price
		takeProfit = currentPrice - (riskAmount * riskRewardRatio)
		if takeProfit >= currentPrice {
			takeProfit = currentPrice * (1 - m.stopLossPercentage)
		}
		stopLimitPrice = stopPrice * 1.005
	}

	tickSize, _ := strconv.ParseFloat(filters.PriceFilter.TickSize, 64)
	// Adjust stopPrice, takeProfit, and stopLimitPrice to comply with tick size
	stopPrice = m.RoundToTickSize(stopPrice, tickSize)
	takeProfit = m.RoundToTickSize(takeProfit, tickSize)
	stopLimitPrice = m.RoundToTickSize(stopLimitPrice, tickSize)

	minNotional, _ := strconv.ParseFloat(filters.NotionalFilter.MinNotional, 64)
	if quantity*currentPrice < minNotional {
		logger.Error.Println("price * quantity is too low to be a valid order for the symbol")
	}

	return quantity, stopPrice, takeProfit, stopLimitPrice, riskAmount
}

func (m *MACDStrategy) RoundToStepSize(value, stepSize float64) float64 {
	return math.Round(value/stepSize) * stepSize
}

func (m *MACDStrategy) RoundToTickSize(value, tickSize float64) float64 {
	return math.Round(value/tickSize) * tickSize
}

// For leverage trading
// Position size = (account size x maximum risk percentage / (entry price – stop loss price)) x entry price
//
// For SPOT trading
// Invalidation point (distance to stop-loss)
// position size = account size x account risk / invalidation point
func (m *MACDStrategy) CalculatePositionSize(
	asset string,
	riskPercentage, entryPrice, stopLossPrice float64,
) (float64, error) {
	// During backtest the balance adjusted by the engine
	if !m.backtest {
		var err error
		m.balance, err = rest.GetBalance(asset)
		if err != nil {
			return 0.0, err
		}
	}

	positionSize := m.balance * riskPercentage / m.stopLossPercentage
	logger.Debug.Printf(
		"Position size: $%.8f, Account balance: %.2f, Risk: %.2f%%, Entry: %.8f, Stop-Loss: %.8f, Invalidation Point: %.2f%%",
		positionSize,
		m.balance,
		riskPercentage*100,
		entryPrice,
		stopLossPrice,
		m.stopLossPercentage*100,
	)
	return positionSize, nil
}

func (m *MACDStrategy) DetermineEntryAndStopLoss(
	side string,
	currentPrice float64,
) (float64, float64) {
	var stopPrice float64

	if side == "SELL" {
		stopPrice = m.swingLow
		if stopPrice >= currentPrice {
			stopPrice = currentPrice * (1 - m.stopLossPercentage)
		}
	} else if side == "BUY" {
		stopPrice = m.swingHigh
		if stopPrice <= currentPrice {
			stopPrice = currentPrice * (1 + m.stopLossPercentage)
		}
	}

	return currentPrice, stopPrice
}
