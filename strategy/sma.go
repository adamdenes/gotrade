package strategy

import (
	"errors"
	"fmt"

	"github.com/adamdenes/gotrade/internal/backtest"
	"github.com/adamdenes/gotrade/internal/logger"
	"github.com/adamdenes/gotrade/internal/models"
)

// SMAStrategy implements the BacktestStrategy interface.
type SMAStrategy struct {
	data      []*models.KlineSimple // Price data
	period    int                   // SMA period
	smaValues []float64             // Calculated SMA values
}

// NewSMAStrategy creates a new SMA strategy with the specified period.
func NewSMAStrategy(period int) (backtest.Strategy[SMAStrategy], error) {
	if period <= 0 {
		return nil, errors.New("period must be greater than zero")
	}
	return &SMAStrategy{
		period: period,
	}, nil
}

// Execute implements the Execute method of the BacktestStrategy interface.
func (s *SMAStrategy) Execute() {
	// Calculate SMA values
	if len(s.data) >= s.period {
		s.calculateSMA()

		// Strategy logic: Generate buy/sell signals
		currentPrice := s.data[0].Close
		previousSMA := s.smaValues[len(s.smaValues)-1]

		if currentPrice > previousSMA {
			fmt.Printf("Buy Signal - Price: %.2f, SMA: %.2f\n", currentPrice, previousSMA)
			// Implement buy logic here
		} else if currentPrice < previousSMA {
			fmt.Printf("Sell Signal - Price: %.2f, SMA: %.2f\n", currentPrice, previousSMA)
			// Implement sell logic here
		}
	}
}

// SetData appends the historical price data to the strategy's data.
func (s *SMAStrategy) SetData(data []*models.KlineSimple) {
	s.data = append(s.data, data...)
}

// calculateSMA calculates the Simple Moving Average.
func (s *SMAStrategy) calculateSMA() {
	if len(s.data) < s.period {
		logger.Debug.Printf("len(s.data) < s.period -> %v < %v", len(s.data), s.period)
		return
	}

	// Initialize SMA values slice
	s.smaValues = make([]float64, len(s.data))

	// Calculate SMA values
	for i := s.period; i <= len(s.data); i++ {
		sum := 0.0
		for j := i - s.period; j < i; j++ {
			sum += s.data[j].Close // Use the Close price from KlineSimple
		}
		s.smaValues[i-1] = sum / float64(s.period)
	}
}
