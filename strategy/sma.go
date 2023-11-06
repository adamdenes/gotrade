package strategy

import (
	"time"

	"github.com/adamdenes/gotrade/cmd/rest"
	"github.com/adamdenes/gotrade/internal/backtest"
	"github.com/adamdenes/gotrade/internal/logger"
	"github.com/adamdenes/gotrade/internal/models"
)

type SMAStrategy struct {
	balance            float64               // Current balance
	positionSize       float64               // Position size
	riskPercentage     float64               // Risk %
	stopLossPercentage float64               // Invalidation point
	asset              string                // Trading pair
	backtest           bool                  // Are we backtesting?
	orders             []models.TypeOfOrder  // Pending orders
	data               []*models.KlineSimple // Price data
	shortPeriod        int                   // Short SMA period
	longPeriod         int                   // Long SMA period
	shortSMA           []float64             // Calculated short SMA values
	longSMA            []float64             // Calculated long SMA values
}

// NewSMAStrategy creates a new SMA crossover strategy with the specified short and long periods.
func NewSMAStrategy(shortPeriod, longPeriod int) backtest.Strategy[SMAStrategy] {
	return &SMAStrategy{
		shortPeriod:        shortPeriod,
		longPeriod:         longPeriod,
		riskPercentage:     0.01,
		stopLossPercentage: 0.15,
	}
}

// Execute implements the Execute method of the BacktestStrategy interface.
func (s *SMAStrategy) Execute() {
	// Calculate SMA values
	s.calculateSMAs()
	currBar := s.data[len(s.data)-1]

	logger.Debug.Printf("Balance: %v , Bar: %+v\n", s.balance, currBar)
	// Generate buy/sell signals based on crossover
	if len(s.longSMA) > 2 {
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
			// it is not needed, but anyways...
			// account size = position size x invalidation point / account risk
			s.balance = s.positionSize * s.stopLossPercentage / s.riskPercentage
		} else {
			s.positionSize = s.balance * s.riskPercentage / s.stopLossPercentage
		}
		// Calculate the quantity based on position size
		quantity := s.positionSize / currBar.Close

		if crossover(s.shortSMA, s.longSMA) {
			buyOrder := s.Buy(s.asset, quantity, currBar.Close)
			logger.Info.Print("Buy Signal")
			s.PlaceOrder(buyOrder)
		}
		if crossunder(s.shortSMA, s.longSMA) {
			sellOrder := s.Sell(s.asset, quantity, currBar.Close)
			logger.Info.Print("Sell Signal")
			s.PlaceOrder(sellOrder)
		}
	}
}

func (s *SMAStrategy) IsBacktest(b bool) {
	s.backtest = b
}

// SetData appends the historical price data to the strategy's data.
func (s *SMAStrategy) SetData(data []*models.KlineSimple) {
	s.data = data
}

func (s *SMAStrategy) PlaceOrder(o models.TypeOfOrder) {
	currBar := s.data[len(s.data)-1]

	switch order := o.(type) {
	case *models.Order:
		logger.Info.Printf("Side: %s, Quantity: %f, Price: %f, StopPrice: %f\n", order.Side, order.Quantity, order.Price, order.StopPrice)
		if s.backtest {
			order.Timestamp = currBar.OpenTime.UnixMilli()
			s.orders = append(s.orders, order)
			return
		}
		rest.Order(order)
	case *models.OrderOCO:
		logger.Info.Println(order.Side, order.Quantity, order.Price)
		logger.Info.Printf("Side: %s, Quantity: %f, Price: %f, StopPrice: %f, StopLimitPrice: %f\n", order.Side, order.Quantity, order.Price, order.StopPrice, order.StopLimitPrice)
		if s.backtest {
			order.Timestamp = currBar.OpenTime.UnixMilli()
			s.orders = append(s.orders, order)
			return
		}
		rest.OrderOCO(order)
	}
}

func (s *SMAStrategy) Buy(asset string, quantity float64, price float64) models.TypeOfOrder {
	// BUY: Limit Price < Last Price < Stop Price
	stopPrice := price + price*s.stopLossPercentage
	stopLimitPrice := stopPrice + stopPrice*s.stopLossPercentage

	return &models.OrderOCO{
		Symbol:               asset,
		Side:                 models.BUY,
		Quantity:             quantity,
		Price:                price,          // Price to buy
		StopPrice:            stopPrice,      // Where to start stop loss
		StopLimitPrice:       stopLimitPrice, // Lowest price you want to sell coins
		StopLimitTimeInForce: models.StopLimitTimeInForce(models.GTC),
		RecvWindow:           5000,
		Timestamp:            time.Now().UnixMilli(),
	}
}

func (s *SMAStrategy) Sell(asset string, quantity float64, price float64) models.TypeOfOrder {
	// SELL: Limit Price > Last Price > Stop Price
	stopPrice := price - price*s.stopLossPercentage
	stopLimitPrice := stopPrice - stopPrice*s.stopLossPercentage

	return &models.OrderOCO{
		Symbol:               asset,
		Side:                 models.SELL,
		Quantity:             quantity,
		Price:                price,          // Price to buy
		StopPrice:            stopPrice,      // Where to start stop loss
		StopLimitPrice:       stopLimitPrice, // Lowest price you want to sell coins
		StopLimitTimeInForce: models.StopLimitTimeInForce(models.GTC),
		RecvWindow:           5000,
		Timestamp:            time.Now().UnixMilli(),
	}
}

func (s *SMAStrategy) SetOrders(orders []models.TypeOfOrder) {
	s.orders = orders
}

func (s *SMAStrategy) GetOrders() []models.TypeOfOrder {
	return s.orders
}

func (s *SMAStrategy) SetAsset(asset string) {
	s.asset = asset
}

func (s *SMAStrategy) SetPositionSize(ps float64) {
	s.positionSize += ps
}

func (s *SMAStrategy) GetPositionSize() float64 {
	return s.positionSize
}

func (s *SMAStrategy) GetBalance() float64 {
	return s.balance
}

func (s *SMAStrategy) SetBalance(balance float64) {
	s.balance = balance
}

// calculateSMAs calculates both short and long SMAs.
func (s *SMAStrategy) calculateSMAs() {
	// Calculate short SMA
	if len(s.data) >= s.shortPeriod {
		shortSMA := calculateSMA(s.data, s.shortPeriod)
		s.shortSMA = append(s.shortSMA, shortSMA)
	}

	// Calculate long SMA
	if len(s.data) >= s.longPeriod {
		longSMA := calculateSMA(s.data, s.longPeriod)
		s.longSMA = append(s.longSMA, longSMA)
	}

	// Prevent infinitely growing SMA slices
	if len(s.shortSMA) > s.shortPeriod {
		s.shortSMA = s.shortSMA[1:]
	}
	if len(s.longSMA) > s.longPeriod {
		s.longSMA = s.longSMA[1:]
	}
}

// calculateSMA calculates the Simple Moving Average.
func calculateSMA(data []*models.KlineSimple, period int) float64 {
	sum := 0.0
	for i := len(data) - 1; i >= len(data)-period; i-- {
		sum += data[i].Close
	}
	return sum / float64(period)
}

func crossover(series1 []float64, series2 []float64) bool {
	N := len(series1)
	M := len(series2)

	if N < 3 || M < 3 {
		return false
	}

	return series1[N-2] <= series2[M-2] && series1[N-1] > series2[M-1]
}

func crossunder(series1 []float64, series2 []float64) bool {
	N := len(series1)
	M := len(series2)

	if N < 3 || M < 3 {
		return false
	}

	return series1[N-1] <= series2[M-1] && series1[N-2] > series2[M-2]
}
