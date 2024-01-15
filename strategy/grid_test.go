package strategy

import (
	"testing"

	"github.com/adamdenes/gotrade/internal/logger"
	"github.com/adamdenes/gotrade/internal/models"
)

func TestCheckRetracement_NotEnoughOrders(t *testing.T) {
	logger.Init()
	g := &GridStrategy{}
	g.orderInfos = []*models.OrderInfo{} // Less than one order

	result := g.CheckRetracement()

	if result != false {
		t.Errorf("Expected false for not enough orders, got %v", result)
	}
}

func TestCheckRetracement_SellOrderWithRetracement(t *testing.T) {
	logger.Init()
	g := &GridStrategy{}
	g.orderInfos = []*models.OrderInfo{
		{Side: "BUY"},
		{Side: "BUY"}, // 'BUY' order in the slice after a 'SELL' order is filled
	}
	g.previousGridLevel = 3
	g.gridLevel.Val = 2

	result := g.CheckRetracement()

	if result != true {
		t.Errorf("Expected true for a 'SELL' order with retracement, got %v", result)
	}
}

func TestCheckRetracement_SellOrderWithoutRetracement(t *testing.T) {
	logger.Init()
	g := &GridStrategy{}
	g.orderInfos = []*models.OrderInfo{
		{Side: "SELL"},
		{Side: "BUY"}, // 'BUY' order in the slice after a 'SELL' order is filled
	}
	g.previousGridLevel = 1
	g.gridLevel.Val = 3

	result := g.CheckRetracement()

	if result != false {
		t.Errorf("Expected false for no retracement after a 'SELL' order, got %v", result)
	}
}

func TestCheckRetracement_BuyOrderWithRetracement(t *testing.T) {
	logger.Init()
	g := &GridStrategy{}
	g.orderInfos = []*models.OrderInfo{
		{Side: "SELL"},
		{Side: "SELL"}, // 'SELL' order in the slice after a 'BUY' order is filled
	}
	g.previousGridLevel = 1
	g.gridLevel.Val = 0

	result := g.CheckRetracement()

	if result != true {
		t.Errorf("Expected true for a retracement after a 'BUY' order, got %v", result)
	}
}

func TestCheckRetracement_BuyOrderWithoutRetracement(t *testing.T) {
	logger.Init()
	g := &GridStrategy{}
	g.orderInfos = []*models.OrderInfo{
		{Side: "BUY"},
		{Side: "SELL"}, // 'SELL' order in the slice after a 'BUY' order is filled
	}
	g.previousGridLevel = 2
	g.gridLevel.Val = 3

	result := g.CheckRetracement()

	if result != false {
		t.Errorf("Expected false for no retracement after a 'BUY' order, got %v", result)
	}
}

func TestCheckRetracement_EqualPreviousAndCurrentLevels(t *testing.T) {
	logger.Init()
	g := &GridStrategy{}
	g.orderInfos = []*models.OrderInfo{
		{Side: "BUY"},
		{Side: "SELL"}, // Assuming a 'SELL' order in the slice
	}
	g.previousGridLevel = 0
	g.gridLevel.Val = 0

	result := g.CheckRetracement()

	if result != false {
		t.Errorf("Expected false when previous and current levels are equal, got %v", result)
	}
}

func TestCheckRetracement_EmptyOrderInfos(t *testing.T) {
	logger.Init()
	g := &GridStrategy{}
	g.orderInfos = []*models.OrderInfo{} // Empty orderInfos slice

	result := g.CheckRetracement()

	if result != false {
		t.Errorf("Expected false with empty orderInfos, got %v", result)
	}
}
