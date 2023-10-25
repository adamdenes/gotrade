package models

import (
	"encoding/json"
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

// Custom unmarshaller to bridge JS and GO time conversion
func (ks *KlineSimple) UnmarshalJSON(data []byte) error {
	var aux struct {
		OpenTime interface{} `json:"time"`
		Open     float64     `json:"open"`
		High     float64     `json:"high"`
		Low      float64     `json:"low"`
		Close    float64     `json:"close"`
		Volume   float64     `json:"volume,omitempty"`
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	switch t := aux.OpenTime.(type) {
	case string:
		ot, err := time.Parse("2006-01-02T15:04:05-07:00", t)
		if err != nil {
			return err
		}
		ks.OpenTime = ot
	case float64:
		// Assuming it's a Unix timestamp in seconds
		ks.OpenTime = time.Unix(int64(t), 0)
	case int64:
		// Assuming it's a Unix timestamp in milliseconds
		ks.OpenTime = time.Unix(0, t*int64(time.Millisecond))
	default:
		return fmt.Errorf("unsupported type for OpenTime: %T", aux.OpenTime)
	}
	ks.Open = aux.Open
	ks.High = aux.High
	ks.Low = aux.Low
	ks.Close = aux.Close
	ks.Volume = aux.Volume

	return nil
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
	Symbol                  string                  `json:"symbol"`
	Side                    OrderSide               `json:"side"`
	Type                    OderType                `json:"type"`
	TimeInForce             string                  `json:"timeInForce,omitempty"`
	Quantity                float64                 `json:"quantity,omitempty"`
	QuoteOrderQty           float64                 `json:"quoteOrderQty,omitempty"`
	Price                   float64                 `json:"price,omitempty"`
	NewClientOrderId        string                  `json:"newClientOrderId,omitempty"`
	StrategyId              int                     `json:"strategyId,omitempty"`
	StrategyType            int                     `json:"strategyType,omitempty"`
	StopPrice               float64                 `json:"stopPrice,omitempty"`
	TrailingDelta           int64                   `json:"trailingDelta,omitempty"`
	IcebergQty              float64                 `json:"icebergQty,omitempty"`
	NewOrderRespType        OrderRespType           `json:"newOrderRespType,omitempty"`
	SelfTradePreventionMode SelfTradePreventionMode `json:"selfTradePreventionMode,omitempty"`
	RecvWindow              int64                   `json:"recvWindow,omitempty"`
	Timestamp               int64                   `json:"timestamp"`
}

func (o *Order) String() string {
	var sb strings.Builder

	if o.Symbol != "" {
		sb.WriteString(fmt.Sprintf("symbol=%s", strings.ToUpper(o.Symbol)))
	}
	if o.Side != "" {
		sb.WriteString(fmt.Sprintf("&side=%s", o.Side))
	}
	if o.Type != "" {
		sb.WriteString(fmt.Sprintf("&type=%s", o.Type))
	}
	if o.TimeInForce != "" {
		sb.WriteString(fmt.Sprintf("&timeInForce=%s", o.TimeInForce))
	}
	if o.Quantity != 0.0 {
		sb.WriteString(fmt.Sprintf("&quantity=%f", o.Quantity))
	}
	if o.QuoteOrderQty != 0.0 {
		sb.WriteString(fmt.Sprintf("&quoteOrderQty=%f", o.QuoteOrderQty))
	}
	if o.Price != 0.0 {
		sb.WriteString(fmt.Sprintf("&price=%f", o.Price))
	}
	if o.NewClientOrderId != "" {
		sb.WriteString(fmt.Sprintf("&newClientOrderId=%s", o.NewClientOrderId))
	}
	if o.StrategyId != 0 {
		sb.WriteString(fmt.Sprintf("&strategyId=%d", o.StrategyId))
	}
	if o.StrategyType != 0 {
		sb.WriteString(fmt.Sprintf("&strategyType=%d", o.StrategyType))
	}
	if o.StopPrice != 0.0 {
		sb.WriteString(fmt.Sprintf("&stopPrice=%f", o.StopPrice))
	}
	if o.TrailingDelta != 0 {
		sb.WriteString(fmt.Sprintf("&trailingDelta=%d", o.TrailingDelta))
	}
	if o.IcebergQty != 0.0 {
		sb.WriteString(fmt.Sprintf("&icebergQty=%f", o.IcebergQty))
	}
	if o.NewOrderRespType != "" {
		sb.WriteString(fmt.Sprintf("&newOrderRespType=%s", o.NewOrderRespType))
	}
	if o.SelfTradePreventionMode != "" {
		sb.WriteString(fmt.Sprintf("&selfTradePreventionMode=%s", o.SelfTradePreventionMode))
	}
	if o.RecvWindow != 0 {
		sb.WriteString(fmt.Sprintf("&recvWindow=%d", o.RecvWindow))
	}
	if o.Timestamp != 0 {
		sb.WriteString(fmt.Sprintf("&timestamp=%d", o.Timestamp))
	}
	return sb.String()
}
