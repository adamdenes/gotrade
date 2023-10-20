package models

import (
	"fmt"
	"strings"
	"time"
)

type CandleSubsciption struct {
	Symbol   string
	Interval string
}

type Kline struct {
	Symbol                string `json:"symbol"`
	Interval              string `json:"interval"`
	OpenTime              int64  `json:"open_time"`
	Open                  string `json:"open"`
	High                  string `json:"high"`
	Low                   string `json:"low"`
	Close                 string `json:"close"`
	Volume                string `json:"volume"`
	CloseTime             int64  `json:"close_time"`
	QuoteAssetVolume      string `json:"quote_volume"`
	NumberOfTrades        int    `json:"count"`
	TakerBuyBaseAssetVol  string `json:"taker_buy_volume"`
	TakerBuyQuoteAssetVol string `json:"taker_buy_quote_volume"`
	Ignore                string `json:"-"`
}

type KlineRequest struct {
	Symbol    string    `json:"symbol"`
	Interval  string    `json:"interval,omitempty"`
	Strat     string    `json:"strategy,omitempty"`
	OpenTime  time.Time `json:"open_time,omitempty"`
	CloseTime time.Time `json:"close_time,omitempty"`
}

func (kr *KlineRequest) String() string {
	var parts []string

	if kr.Symbol != "" {
		parts = append(parts, fmt.Sprintf("symbol=%s", strings.ToUpper(kr.Symbol)))
	}
	if kr.Interval != "" {
		parts = append(parts, fmt.Sprintf("interval=%s", kr.Interval))
	}
	if kr.Strat != "" {
		parts = append(parts, fmt.Sprintf("strategy=%s", kr.Strat))
	}
	if !kr.OpenTime.IsZero() {
		parts = append(parts, fmt.Sprintf("startTime=%v", kr.OpenTime))
	}
	if !kr.CloseTime.IsZero() {
		parts = append(parts, fmt.Sprintf("endTime=%v", kr.CloseTime))
	}

	return fmt.Sprintf("%s", strings.Join(parts, "&"))
}

type KlineSimple struct {
	OpenTime time.Time `json:"open_time"`
	Open     float64   `json:"open"`
	High     float64   `json:"high"`
	Low      float64   `json:"low"`
	Close    float64   `json:"close"`
	Volume   float64   `json:"volume,omitempty"`
}

type RequestError struct {
	Err    error
	Status int
	Timer  time.Duration
}

func (e *RequestError) Error() string {
	return e.Err.Error()
}

type OrderSide string

const (
	BUY  OrderSide = "BUY"
	SELL           = "SELL"
)

type OderType string

const (
	LIMIT             OderType = "LIMIT"
	MARKET                     = "MARKET"
	STOP_LOSS                  = "STOP_LOSS"
	STOP_LOSS_LIMIT            = "STOP_LOSS_LIMIT"
	TAKE_PROFIT                = "TAKE_PROFIT"
	TAKE_PROFIT_LIMIT          = "TAKE_PROFIT_LIMIT"
	LIMIT_MAKER                = "LIMIT_MAKER"
)

type OrderRespType string

const (
	ACK    OrderRespType = "ACK"
	RESULT               = "RESULT"
	FULL                 = "FULL"
)

type SelfTradePreventionMode string

const (
	EXPIRE_TAKER SelfTradePreventionMode = "EXPIRE_TAKER"
	EXPIRE_MAKER                         = "EXPIRE_MAKER"
	EXPIRE_BOTH                          = "EXPIRE_BOTH"
	NONE                                 = "NONE"
)

type Order struct {
	Symbol              string                  `json:"symbol"`
	Side                OrderSide               `json:"side"`
	Type                OderType                `json:"type"`
	TimeInForce         string                  `json:"timeInForce,omitempty"`
	Quantity            float64                 `json:"quantity,omitempty"`
	QuoteOrderQty       float64                 `json:"quoteOrderQty,omitempty"`
	Price               float64                 `json:"price,omitempty"`
	NewClientOrderId    string                  `json:"newClientOrderId,omitempty"`
	StrategyId          int                     `json:"strategyId,omitempty"`
	StrategyType        int                     `json:"strategyType,omitempty"`
	StopPrice           string                  `json:"stopPrice,omitempty"`
	TrailingDelta       int64                   `json:"trailingDelta,omitempty"`
	IcebergQty          float64                 `json:"icebergQty,omitempty"`
	NewOrderRespType    OrderRespType           `json:"newOrderRespType,omitempty"`
	SelfTradePrevention SelfTradePreventionMode `json:"selfTradePrevention,omitempty"`
	RecvWindow          int64                   `json:"recvWindow,omitempty"`
	Timestamp           int64                   `json:"timestamp"`
}
