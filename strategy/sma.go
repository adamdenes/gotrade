package strategy

import (
	"github.com/adamdenes/gotrade/internal/backtest"
	"github.com/adamdenes/gotrade/internal/logger"
	"github.com/adamdenes/gotrade/internal/models"
)

type SMAStrategy struct {
	data        []*models.KlineSimple // Price data
	shortPeriod int                   // Short SMA period
	longPeriod  int                   // Long SMA period
	shortSMA    []float64             // Calculated short SMA values
	longSMA     []float64             // Calculated long SMA values
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

	// Generate buy/sell signals based on crossover
	// Check if there are enough previous SMA values available
	if len(s.longSMA) > 2 {
		if crossover(s.shortSMA, s.longSMA) {
			// implement buy signal
			logger.Debug.Printf(
				"Crossover: %.12f <= %.12f && %.12f > %.12f -> %t\n",
				s.shortSMA[len(s.shortSMA)-2],
				s.longSMA[len(s.longSMA)-2],
				s.shortSMA[len(s.shortSMA)-1],
				s.longSMA[len(s.longSMA)-1],
				(s.shortSMA[len(s.shortSMA)-2] <= s.longSMA[len(s.longSMA)-2] && s.shortSMA[len(s.shortSMA)-1] > s.longSMA[len(s.longSMA)-1]),
			)
			logger.Info.Println("Buy Signal")
		}
		if crossunder(s.shortSMA, s.longSMA) {
			// implement sell signal
			logger.Debug.Printf(
				"Crossunder: %.12f <= %.12f && %.12f > %.12f -> %t\n",
				s.shortSMA[len(s.shortSMA)-1],
				s.longSMA[len(s.longSMA)-1],
				s.shortSMA[len(s.shortSMA)-2],
				s.longSMA[len(s.longSMA)-2],
				(s.shortSMA[len(s.shortSMA)-1] <= s.longSMA[len(s.longSMA)-1] && s.shortSMA[len(s.shortSMA)-2] > s.longSMA[len(s.longSMA)-2]),
			)
			logger.Info.Println("Sell Signal")
		}
	}
}

// SetData appends the historical price data to the strategy's data.
func (s *SMAStrategy) SetData(data []*models.KlineSimple) {
	s.data = append(s.data, data...)
}

// calculateSMAs calculates both short and long SMAs.
func (s *SMAStrategy) calculateSMAs() {
	// Calculate short SMA
	if len(s.data) >= s.shortPeriod {
		shortSMA := calculateSMA(s.data, s.shortPeriod)
		s.shortSMA = append(s.shortSMA, shortSMA)

		// Start removing old data from slice
		if len(s.shortSMA) == s.shortPeriod {
			s.shortSMA = s.shortSMA[1:]
		}
	}

	// Calculate long SMA
	if len(s.data) >= s.longPeriod {
		longSMA := calculateSMA(s.data, s.longPeriod)
		s.longSMA = append(s.longSMA, longSMA)

		// Start removing old data from slice
		if len(s.longSMA) == s.longPeriod {
			s.longSMA = s.longSMA[1:]
		}
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
