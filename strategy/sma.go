package strategy

import (
	"fmt"

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
	shortIndex  int
	longIndex   int
}

// NewSMAStrategy creates a new SMA crossover strategy with the specified short and long periods.
func NewSMAStrategy(shortPeriod, longPeriod int) backtest.Strategy[SMAStrategy] {
	return &SMAStrategy{
		shortPeriod: shortPeriod,
		longPeriod:  longPeriod,
		shortIndex:  0,
		longIndex:   0,
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
				"Crossover: %f <= %f && %f > %f -> %t\n",
				s.shortSMA[len(s.shortSMA)-2],
				s.longSMA[len(s.longSMA)-2],
				s.shortSMA[len(s.shortSMA)-1],
				s.longSMA[len(s.longSMA)-1],
				(s.shortSMA[len(s.shortSMA)-2] <= s.longSMA[len(s.longSMA)-2] && s.shortSMA[len(s.shortSMA)-1] > s.longSMA[len(s.longSMA)-1]),
			)
			logger.Info.Printf("Buy Signal")
		}
		if crossunder(s.shortSMA, s.longSMA) {
			// implement sell signal
			logger.Debug.Printf(
				"Crossover: %f <= %f && %f > %f -> %t\n",
				s.shortSMA[len(s.shortSMA)-1],
				s.longSMA[len(s.longSMA)-1],
				s.shortSMA[len(s.shortSMA)-2],
				s.longSMA[len(s.longSMA)-2],
				(s.shortSMA[len(s.shortSMA)-1] <= s.longSMA[len(s.longSMA)-1] && s.shortSMA[len(s.shortSMA)-2] > s.longSMA[len(s.longSMA)-2]),
			)
			logger.Info.Printf("Sell Signal")
		}
	}
}

// SetData appends the historical price data to the strategy's data.
func (s *SMAStrategy) SetData(data []*models.KlineSimple) {
	fmt.Println("Append data:", data[0])
	s.data = append(s.data, data...)
}

// calculateSMAs calculates both short and long SMAs.
func (s *SMAStrategy) calculateSMAs() {
	if len(s.data) >= s.shortPeriod && s.shortIndex == s.shortPeriod {
		// Calculate short SMA
		shortSMA := calculateSMA(s.data, s.shortPeriod)
		s.shortSMA = append(s.shortSMA, shortSMA)
		fmt.Println("sidx:", s.shortIndex, s.shortSMA)
		s.shortIndex = 0
	}

	if len(s.data) >= s.longPeriod && s.longIndex == s.longPeriod {
		// Calculate long SMA
		longSMA := calculateSMA(s.data, s.longPeriod)
		s.longSMA = append(s.longSMA, longSMA)
		fmt.Println("lidx:", s.longIndex, s.longSMA)
		s.longIndex = 0
	}

	s.shortIndex++
	s.longIndex++
}

// calculateSMA calculates the Simple Moving Average.
func calculateSMA(data []*models.KlineSimple, period int) float64 {
	sum := 0.0
	for i := len(data) - 1; i >= len(data)-period; i-- {
		sum += data[i].Close // Use the Close price from KlineSimple
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
