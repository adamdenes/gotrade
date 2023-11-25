package strategy

import (
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/adamdenes/gotrade/cmd/rest"
	"github.com/adamdenes/gotrade/internal/backtest"
	"github.com/adamdenes/gotrade/internal/logger"
	"github.com/adamdenes/gotrade/internal/models"
	"github.com/adamdenes/gotrade/internal/storage"
	"github.com/markcheno/go-talib"
)

type SMAStrategy struct {
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
	shortPeriod        int                   // Short SMA period
	longPeriod         int                   // Long SMA period
	shortSMA           []float64             // Calculated short SMA values
	longSMA            []float64             // Calculated long SMA values
	ema200             []float64             // EMA 200 values
	closePrices        []float64
}

// NewSMAStrategy creates a new SMA crossover strategy with the specified short and long periods.
func NewSMAStrategy(
	shortPeriod, longPeriod int,
	orderLimit int,
	db storage.Storage,
) backtest.Strategy[SMAStrategy] {
	return &SMAStrategy{
		name:               "sma",
		db:                 db,
		shortPeriod:        shortPeriod,
		longPeriod:         longPeriod,
		riskPercentage:     0.01,
		stopLossPercentage: 0.15,
		orderLimit:         orderLimit,
	}
}

// Execute implements the Execute method of the BacktestStrategy interface.
func (s *SMAStrategy) Execute() {
	s.GetClosePrices()
	currBar := s.data[len(s.data)-1]
	s.closePrices = append(s.closePrices, currBar.Close)
	currentPrice := s.closePrices[len(s.closePrices)-1]

	// Calculate SMA values
	s.calculateSMAs()

	// Generate buy/sell signals based on crossover
	if len(s.longSMA) > 2 && len(s.ema200) > 0 {
		if !s.backtest {
			// Calculate the position size based on asset and risk
			var err error
			s.positionSize, err = rest.CalculatePositionSize(
				s.asset,
				s.riskPercentage,
				s.stopLossPercentage,
			)
			if err != nil {
				logger.Error.Printf("Error calculating position size: %v\n", err)
				return
			}
			// account size = position size x invalidation point / account risk
			s.balance = s.positionSize * s.stopLossPercentage / s.riskPercentage
		} else {
			s.positionSize = s.balance * s.riskPercentage / s.stopLossPercentage
		}
		// Calculate the quantity based on position size
		// PRICE_FILTER -> price % tickSize == 0 | tickSize: 0.00001
		quantity := math.Round(s.positionSize/currentPrice*100000) / 100000

		ema200 := s.ema200[len(s.ema200)-1]
		var order models.TypeOfOrder

		if currentPrice > ema200 && talib.Crossover(s.shortSMA, s.longSMA) {
			// Generate buy signal based on SMA crossover and EMA 200 condition
			order = s.Buy(s.asset, quantity, currentPrice)
			s.PlaceOrder(order)
		} else if currentPrice < ema200 && talib.Crossunder(s.shortSMA, s.longSMA) {
			// Generate sell signal based on SMA crossunder or EMA 200 condition
			order = s.Sell(s.asset, quantity, currentPrice)
			s.PlaceOrder(order)
		}
	}
}

func (s *SMAStrategy) PlaceOrder(o models.TypeOfOrder) {
	currBar := s.data[len(s.data)-1]

	switch order := o.(type) {
	case *models.Order:
		if s.orderLimit >= len(s.orders) {
			logger.Info.Printf("Side: %s, Quantity: %f, Price: %f, StopPrice: %f\n", order.Side, order.Quantity, order.Price, order.StopPrice)
			if s.backtest {
				order.Timestamp = currBar.OpenTime.UnixMilli()
				s.orders = append(s.orders, order)
				return
			}

			resp, err := rest.Order(order)
			if err != nil {
				logger.Error.Printf("Failed to send OCO order: %v", err)
				return
			}

			var orderResponse models.OrderResponse
			err = json.Unmarshal(resp, &orderResponse)
			if err != nil {
				logger.Error.Println("Error unmarshalling OCO response JSON:", err)
				return
			}

			t := &models.Trade{
				Strategy: s.name,
				Status:   string(models.NEW),
				Symbol:   orderResponse.Symbol,
				OrderID:  orderResponse.OrderID,
				Price:    fmt.Sprintf("%f", order.Price),
				Qty:      fmt.Sprintf("%f", order.Quantity),
				Time:     time.Now(),
			}
			if order.Side == models.BUY {
				t.IsBuyer = true
				t.IsMaker = false
			} else {
				t.IsBuyer = false
				t.IsMaker = true
			}

			if err := s.db.SaveTrade(t); err != nil {
				logger.Error.Printf("Error saving buy trade: %v", err)
			}
		} else {
			logger.Error.Printf("No more orders allowed! Current limit is: %v", s.orderLimit)
		}
	case *models.OrderOCO:
		if s.orderLimit >= len(s.orders) {
			logger.Info.Printf("Side: %s, Quantity: %f, Price: %f, StopPrice: %f, StopLimitPrice: %f\n", order.Side, order.Quantity, order.Price, order.StopPrice, order.StopLimitPrice)
			if s.backtest {
				order.Timestamp = currBar.OpenTime.UnixMilli()
				s.orders = append(s.orders, order)
				return
			}

			resp, err := rest.OrderOCO(order)
			if err != nil {
				logger.Error.Printf("Failed to send OCO order: %v", err)
				return
			}

			var ocoResponse models.OrderOCOResponse
			err = json.Unmarshal(resp, &ocoResponse)
			if err != nil {
				logger.Error.Println("Error unmarshalling OCO response JSON:", err)
				return
			}

			t := &models.Trade{
				Strategy:    s.name,
				Status:      string(models.EXECUTING),
				Symbol:      ocoResponse.Symbol,
				OrderListID: ocoResponse.OrderListID,
				Price:       fmt.Sprintf("%f", order.Price),
				Qty:         fmt.Sprintf("%f", order.Quantity),
				Time:        time.Now(),
			}
			if order.Side == models.BUY {
				t.IsBuyer = true
				t.IsMaker = false
			} else {
				t.IsBuyer = false
				t.IsMaker = true
			}

			if err := s.db.SaveTrade(t); err != nil {
				logger.Error.Printf("Error saving sell trade: %v", err)
			}
		} else {
			logger.Error.Printf("No more orders allowed! Current limit is: %v", s.orderLimit)
		}
	}
}

func (s *SMAStrategy) Buy(asset string, quantity float64, price float64) models.TypeOfOrder {
	takeProfit := price * 0.95
	// BUY: Limit Price < Last Price < Stop Price
	stopPrice := takeProfit + takeProfit*s.stopLossPercentage
	stopLimitPrice := stopPrice * 1.02

	return &models.OrderOCO{
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

func (s *SMAStrategy) Sell(asset string, quantity float64, price float64) models.TypeOfOrder {
	takeProfit := price * 1.05
	// SELL: Limit Price > Last Price > Stop Price
	stopPrice := takeProfit - takeProfit*s.stopLossPercentage
	stopLimitPrice := stopPrice * 0.98

	return &models.OrderOCO{
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

func (s *SMAStrategy) IsBacktest(b bool) {
	s.backtest = b
}

func (s *SMAStrategy) GetBalance() float64 {
	return s.balance
}

func (s *SMAStrategy) SetBalance(balance float64) {
	s.balance = balance
}

func (s *SMAStrategy) SetOrders(orders []models.TypeOfOrder) {
	s.orders = orders
}

func (s *SMAStrategy) GetOrders() []models.TypeOfOrder {
	return s.orders
}

func (s *SMAStrategy) SetPositionSize(ps float64) {
	s.positionSize += ps
}

func (s *SMAStrategy) GetPositionSize() float64 {
	return s.positionSize
}

func (s *SMAStrategy) SetData(data []*models.KlineSimple) {
	s.data = data
}

func (s *SMAStrategy) SetAsset(asset string) {
	s.asset = asset
}

func (s *SMAStrategy) GetClosePrices() {
	if len(s.closePrices) > 0 {
		return
	}
	for _, bar := range s.data {
		s.closePrices = append(s.closePrices, bar.Close)
	}
}

// calculateSMAs calculates both short and long SMAs.
func (s *SMAStrategy) calculateSMAs() {
	// Calculate short SMA
	if len(s.data) >= s.shortPeriod {
		shortSMA := talib.Sma(s.closePrices, s.shortPeriod)[len(s.closePrices)-1:]
		s.shortSMA = append(s.shortSMA, shortSMA...)
	}

	// Calculate long SMA
	if len(s.data) >= s.longPeriod {
		longSMA := talib.Sma(s.closePrices, s.longPeriod)[len(s.closePrices)-1:]
		s.longSMA = append(s.longSMA, longSMA...)
	}

	// Calculate EMA 200
	if len(s.data) >= 200 {
		ema200 := talib.Ema(s.closePrices, 200)[len(s.closePrices)-1:]
		s.ema200 = append(s.ema200, ema200...)
	}

	// Prevent infinitely growing SMA slices
	if len(s.shortSMA) > 3 {
		s.shortSMA = s.shortSMA[1:]
	}
	if len(s.longSMA) > 3 {
		s.longSMA = s.longSMA[1:]
	}
	if len(s.ema200) > 2 {
		s.ema200 = s.ema200[1:]
	}
}
