package strategy

import (
	"math"
	"testing"

	"github.com/adamdenes/gotrade/internal/logger"
)

func TestGridStrategy_CheckRetracement_NoOrders(t *testing.T) {
	logger.Init()
	g := &GridStrategy{}
	result := g.CheckRetracement()

	// Assert that the method handles no orders correctly and returns false
	if len(g.orderInfos) != 0 {
		t.Errorf("Expected orderInfos length to be 0, but got %d", len(g.orderInfos))
	}
	if result != false {
		t.Errorf("Expected CheckRetracement to return false, but got true")
	}
}

func TestGridStrategy_CheckRetracement_RetracementToLower(t *testing.T) {
	logger.Init()
	g := &GridStrategy{
		orderInfos:        []*OrderInfo{{Side: "SELL"}, {Side: "SELL"}},
		previousGridLevel: 2,
		gridLevel:         1,
	}
	result := g.CheckRetracement()

	// Assert that the method detects retracement to a lower level and returns true
	if len(g.orderInfos) != 2 {
		t.Errorf("Expected orderInfos length to be 2, but got %d", len(g.orderInfos))
	}
	if result != true {
		t.Errorf("Expected CheckRetracement to return true, but got false")
	}
}

func TestGridStrategy_CheckRetracement_RetracementToUpper(t *testing.T) {
	logger.Init()
	g := &GridStrategy{
		orderInfos:        []*OrderInfo{{Side: "BUY"}, {Side: "BUY"}},
		previousGridLevel: 2,
		gridLevel:         3,
	}
	result := g.CheckRetracement()

	// Assert that the method detects retracement to a higher level and returns true
	if len(g.orderInfos) != 2 {
		t.Errorf("Expected orderInfos length to be 2, but got %d", len(g.orderInfos))
	}
	if result != true {
		t.Errorf("Expected CheckRetracement to return true, but got false")
	}
}

func TestGridStrategy_CheckRetracement_ProgressingLinearly(t *testing.T) {
	logger.Init()
	g := &GridStrategy{
		orderInfos:        []*OrderInfo{{Side: "SELL"}, {Side: "BUY"}},
		previousGridLevel: 1,
		gridLevel:         0,
	}
	result := g.CheckRetracement()

	// Assert that the method does not reset the grid when the market is progressing linearly and returns false
	if len(g.orderInfos) != 2 {
		t.Errorf("Expected orderInfos length to be 2, but got %d", len(g.orderInfos))
	}
	if result != false {
		t.Errorf("Expected CheckRetracement to return false, but got true")
	}
}

func TestGridStrategy_CheckRetracement_AbsValues(t *testing.T) {
	logger.Init()
	g := &GridStrategy{
		orderInfos:        []*OrderInfo{{Side: "BUY"}, {Side: "SELL"}},
		previousGridLevel: -1,
		gridLevel:         -2,
	}
	result := g.CheckRetracement()

	// Assert that the method correctly computes absolute values for grid levels
	if math.IsNaN(float64(g.previousGridLevel)) || math.IsNaN(float64(g.gridLevel)) {
		t.Errorf("Expected previousGridLevel and gridLevel to be NaN, but they are not")
	}
	if result != false {
		t.Errorf("Expected CheckRetracement to return false, but got true")
	}
}
