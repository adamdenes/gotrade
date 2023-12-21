package strategy

import (
	"math"
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
		stopLossPercentage: 0.05,
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
		if !m.backtest {
			// Calculate the position size based on asset and risk
			var err error
			m.positionSize, err = rest.CalculatePositionSize(
				m.asset,
				m.riskPercentage,
				m.stopLossPercentage,
			)
			if err != nil {
				logger.Error.Printf("Error calculating position size: %v\n", err)
				return
			}
			m.balance = m.positionSize * m.stopLossPercentage / m.riskPercentage
		} else {
			m.positionSize = m.balance * m.riskPercentage / m.stopLossPercentage
		}
		// Calculate the quantity based on position size
		// PRICE_FILTER -> price % tickSize == 0 | tickSize: 0.00001
		quantity := math.Round(m.positionSize/currentPrice*100000) / 100000

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
				order = m.Sell(m.asset, quantity, currentPrice)
				m.PlaceOrder(order)
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
				order = m.Buy(m.asset, quantity, currentPrice)
				m.PlaceOrder(order)
			}
		}
	}
}

// Wrapper over talib.Macd
func (m *MACDStrategy) MACD() {
	// Wait till slow period + signal period
	if len(m.closePrices) >= 34 {
		macd, macdsignal, macdhist := talib.Macd(m.closePrices, 12, 26, 9)
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
			logger.Info.Printf("Side: %s, Quantity: %f, Price: %f, StopPrice: %f\n", order.Side, order.Quantity, order.Price, order.StopPrice)
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
			logger.Info.Printf("Side: %s, Quantity: %f, Price: %f, StopPrice: %f, StopLimitPrice: %f\n", order.Side, order.Quantity, order.Price, order.StopPrice, order.StopLimitPrice)
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
	}
}

func (m *MACDStrategy) Buy(asset string, quantity float64, price float64) models.TypeOfOrder {
	// BUY: Limit Price < Last Price < Stop Price
	stopPrice, takeProfit, stopLimitPrice, riskAmount := m.calculateParams("BUY", price, 1.5)

	logger.Debug.Println(
		"price =", price,
		"tp =",
		takeProfit,
		"sp =",
		stopPrice,
		"slp =",
		stopLimitPrice,
		"riska =",
		riskAmount,
	)

	return &models.PostOrderOCO{
		Symbol:    asset,
		Side:      models.BUY,
		Quantity:  quantity,
		Price:     math.Round(takeProfit*100) / 100, // Price to buy (Take Profit)
		StopPrice: math.Round(stopPrice*100) / 100,  // Where to start stop loss
		StopLimitPrice: math.Round(
			stopLimitPrice*100,
		) / 100, // Highest price you want to sell coins
		StopLimitTimeInForce: models.StopLimitTimeInForce(models.GTC),
		RecvWindow:           5000,
		Timestamp:            time.Now().UnixMilli(),
	}
}

func (m *MACDStrategy) Sell(asset string, quantity float64, price float64) models.TypeOfOrder {
	// SELL: Limit Price > Last Price > Stop Price
	stopPrice, takeProfit, stopLimitPrice, riskAmount := m.calculateParams("SELL", price, 1.5)

	logger.Debug.Println(
		"price =", price,
		"tp =",
		takeProfit,
		"sp =",
		stopPrice,
		"slp =",
		stopLimitPrice,
		"riska =",
		riskAmount,
	)

	return &models.PostOrderOCO{
		Symbol:    asset,
		Side:      models.SELL,
		Quantity:  quantity,
		Price:     math.Round(takeProfit*100) / 100, // Price to sell (Take Profit)
		StopPrice: math.Round(stopPrice*100) / 100,  // Where to start stop loss
		StopLimitPrice: math.Round(
			stopLimitPrice*100,
		) / 100, // Lowest price you want to buy coins
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
	m.asset = asset
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
	currentPrice, riskRewardRatio float64,
) (float64, float64, float64, float64) {
	var stopPrice, takeProfit, stopLimitPrice, riskAmount float64

	if side == "SELL" {
		// SELL: Limit Price > Last Price > Stop Price
		// Calculate parameters for sell orders
		stopPrice = m.swingHigh * (1 + m.stopLossPercentage)
		if stopPrice >= currentPrice {
			stopPrice = currentPrice * (1 - m.stopLossPercentage)
		}
		riskAmount = math.Abs(currentPrice - stopPrice)
		takeProfit = currentPrice + (riskAmount * riskRewardRatio)
		if takeProfit <= currentPrice {
			takeProfit = currentPrice * (1 + m.stopLossPercentage)
		}
		stopLimitPrice = stopPrice * 0.99 // Example adjustment
	} else if side == "BUY" {

		// BUY: Limit Price < Last Price < Stop Price
		// Calculate parameters for buy orders
		stopPrice = m.swingLow * (1 - m.stopLossPercentage)
		if stopPrice <= currentPrice {
			stopPrice = currentPrice * (1 + m.stopLossPercentage)
		}
		riskAmount = math.Abs(stopPrice - currentPrice)
		takeProfit = currentPrice - (riskAmount * riskRewardRatio)
		if takeProfit >= currentPrice {
			takeProfit = currentPrice * (1 - m.stopLossPercentage)
		}
		stopLimitPrice = stopPrice * 1.01
	}

	return stopPrice, takeProfit, stopLimitPrice, riskAmount
}
