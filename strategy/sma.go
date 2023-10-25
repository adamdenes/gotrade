package strategy

import (
	"time"

	"github.com/adamdenes/gotrade/cmd/rest"
	"github.com/adamdenes/gotrade/internal/backtest"
	"github.com/adamdenes/gotrade/internal/logger"
	"github.com/adamdenes/gotrade/internal/models"
)

type SMAStrategy struct {
	balance      float64               // Current balance
	positionSize float64               // Position size
	asset        string                // Trading pair
	backtest     bool                  // Are we backtesting?
	orders       []*models.Order       // Pending orders
	data         []*models.KlineSimple // Price data
	shortPeriod  int                   // Short SMA period
	longPeriod   int                   // Long SMA period
	shortSMA     []float64             // Calculated short SMA values
	longSMA      []float64             // Calculated long SMA values
}

// NewSMAStrategy creates a new SMA crossover strategy with the specified short and long periods.
func NewSMAStrategy(shortPeriod, longPeriod int) backtest.Strategy[SMAStrategy] {
	return &SMAStrategy{
		shortPeriod: shortPeriod,
		longPeriod:  longPeriod,
	}
}

// Execute implements the Execute method of the BacktestStrategy interface.
func (s *SMAStrategy) Execute() {
	// Calculate SMA values
	s.calculateSMAs()
	currBar := s.data[len(s.data)-1]

	// Generate buy/sell signals based on crossover
	if len(s.longSMA) > 2 {
		if crossover(s.shortSMA, s.longSMA) {
			bo := s.Buy(s.asset, 0.01, currBar.Close)
			logger.Info.Println("Buy Signal", bo.Side, bo.Quantity, bo.Price)
			if s.backtest {
				bo.Timestamp = currBar.OpenTime.UnixMilli()
				s.orders = append(s.orders, bo)
				return
			}
			rest.TestOrder(bo)
		}
		if crossunder(s.shortSMA, s.longSMA) {
			so := s.Sell(s.asset, 0.01, currBar.Close)
			logger.Info.Println("Sell Signal", so.Side, so.Quantity, so.Price)
			if s.backtest {
				so.Timestamp = currBar.OpenTime.UnixMilli()
				s.orders = append(s.orders, so)
				return
			}
			rest.TestOrder(so)
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

func (s *SMAStrategy) Buy(asset string, quantity float64, price float64) *models.Order {
	return &models.Order{
		Symbol:      asset,
		Side:        models.BUY,
		Type:        models.LIMIT,
		TimeInForce: "GTC",
		Quantity:    quantity,
		Price:       price,
		RecvWindow:  5000,
		Timestamp:   time.Now().UnixMilli(),
	}
}

func (s *SMAStrategy) Sell(asset string, quantity float64, price float64) *models.Order {
	return &models.Order{
		Symbol:      asset,
		Side:        models.SELL,
		Type:        models.LIMIT,
		TimeInForce: "GTC",
		Quantity:    quantity,
		Price:       price,
		RecvWindow:  5000,
		Timestamp:   time.Now().UnixMilli(),
	}
}

func (s *SMAStrategy) SetOrders(orders []*models.Order) {
	s.orders = orders
}

func (s *SMAStrategy) GetOrders() []*models.Order {
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
