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
	Strategy string
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

type KlineWebSocket struct {
	Stream string `json:"stream"`
	Data   struct {
		EventType string `json:"e"` // Event type
		EventTime int64  `json:"E"` // Event time
		Symbol    string `json:"s"` // Symbol
		Kline     struct {
			StartTime                int64  `json:"t"`           // Kline start time
			CloseTime                int64  `json:"T"`           // Kline close time
			Symbol                   string `json:"s"`           // Symbol
			Interval                 string `json:"i"`           // Interval
			FirstTradeID             int    `json:"f"`           // First trade ID
			LastTradeID              int    `json:"L"`           // Last trade ID
			OpenPrice                string `json:"o"`           // Open price
			ClosePrice               string `json:"c"`           // Close price
			HighPrice                string `json:"h"`           // High price
			LowPrice                 string `json:"l"`           // Low price
			BaseAssetVolume          string `json:"v"`           // Base asset volume
			NumberOfTrades           int    `json:"n"`           // Number of trades
			IsKlineClosed            bool   `json:"x"`           // Is this kline closed?
			QuoteAssetVolume         string `json:"q"`           // Quote asset volume
			TakerBuyBaseAssetVolume  string `json:"V"`           // Taker buy base asset volume
			TakerBuyQuoteAssetVolume string `json:"Q"`           // Taker buy quote asset volume
			Ignore                   string `json:"B,omitempty"` // Ignore
		} `json:"k"`
	} `json:"data"`
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

type TypeOfOrder interface {
	String() string
}

type OrderSide string

const (
	BUY  OrderSide = "BUY"
	SELL           = "SELL"
)

type OrderType string

const (
	LIMIT             OrderType = "LIMIT"
	MARKET                      = "MARKET"
	STOP_LOSS                   = "STOP_LOSS"
	STOP_LOSS_LIMIT             = "STOP_LOSS_LIMIT"
	TAKE_PROFIT                 = "TAKE_PROFIT"
	TAKE_PROFIT_LIMIT           = "TAKE_PROFIT_LIMIT"
	LIMIT_MAKER                 = "LIMIT_MAKER"
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

type TimeInForce string

const (
	GTC TimeInForce = "GTC"
	FOK             = "FOK"
	IOC             = "IOC"
)

type PostOrder struct {
	Symbol                  string                  `json:"symbol"`
	Side                    OrderSide               `json:"side"`
	Type                    OrderType               `json:"type"`
	TimeInForce             TimeInForce             `json:"timeInForce,omitempty"`
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

func (o *PostOrder) String() string {
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

type OrderStatus string

const (
	NEW              OrderStatus = "NEW"
	PARTIALLY_FILLED             = "PARTIALLY_FILLED"
	FILLED                       = "FILLED"
	CANCELED                     = "CANCELED"
	PENDING_CANCEL               = "PENDING_CANCEL"
	REJECTED                     = "REJECT"
	EXPIRED                      = "EXPIRED"
	EXPIRED_IN_MATCH             = "EXPIRED_IN_MATCH"
)

type GetOrderResponse struct {
	Symbol                  string                  `json:"symbol"`
	OrderID                 int64                   `json:"orderId"`
	OrderListID             int64                   `json:"orderListId"`
	ClientOrderID           string                  `json:"clientOrderId"`
	Price                   string                  `json:"price"`
	OrigQty                 string                  `json:"origQty"`
	ExecutedQty             string                  `json:"executedQty"`
	CummulativeQuoteQty     string                  `json:"cummulativeQuoteQty"`
	Status                  OrderStatus             `json:"status"`
	TimeInForce             TimeInForce             `json:"timeInForce"`
	Type                    OrderType               `json:"type"`
	Side                    OrderSide               `json:"side"`
	StopPrice               string                  `json:"stopPrice"`
	IcebergQty              string                  `json:"icebergQty"`
	Time                    int64                   `json:"time"`
	UpdateTime              int64                   `json:"updateTime"`
	IsWorking               bool                    `json:"isWorking"`
	WorkingTime             int64                   `json:"workingTime"`
	OrigQuoteOrderQty       string                  `json:"origQuoteOrderQty"`
	SelfTradePreventionMode SelfTradePreventionMode `json:"selfTradePreventionMode"`
}

func (gor *GetOrderResponse) ToPostOrderResponse() *PostOrderResponse {
	return &PostOrderResponse{
		Symbol:                  gor.Symbol,
		OrderID:                 gor.OrderID,
		OrderListID:             gor.OrderListID,
		ClientOrderID:           gor.ClientOrderID,
		TransactTime:            gor.UpdateTime,
		Price:                   gor.Price,
		OrigQty:                 gor.OrigQty,
		ExecutedQty:             gor.ExecutedQty,
		CummulativeQuoteQty:     gor.CummulativeQuoteQty,
		Status:                  gor.Status,
		TimeInForce:             gor.TimeInForce,
		Type:                    gor.Type,
		Side:                    gor.Side,
		StopPrice:               gor.StopPrice,
		WorkingTime:             gor.WorkingTime,
		SelfTradePreventionMode: gor.SelfTradePreventionMode,
	}
}

type PostOrderResponse struct {
	Symbol                  string                  `json:"symbol"`
	OrderID                 int64                   `json:"orderId"`
	OrderListID             int64                   `json:"orderListId"`
	ClientOrderID           string                  `json:"clientOrderId"`
	TransactTime            int64                   `json:"transactTime"`
	Price                   string                  `json:"price"`
	OrigQty                 string                  `json:"origQty"`
	ExecutedQty             string                  `json:"executedQty"`
	CummulativeQuoteQty     string                  `json:"cummulativeQuoteQty"`
	Status                  OrderStatus             `json:"status"`
	TimeInForce             TimeInForce             `json:"timeInForce"`
	Type                    OrderType               `json:"type"`
	Side                    OrderSide               `json:"side"`
	StopPrice               string                  `json:"stopPrice,omitempty"` // Maybe?
	WorkingTime             int64                   `json:"workingTime"`
	SelfTradePreventionMode SelfTradePreventionMode `json:"selfTradePreventionMode"`
}

type StopLimitTimeInForce TimeInForce

type PostOrderOCO struct {
	Symbol                  string                  `json:"symbol"`
	ListClientOrderId       string                  `json:"listClientOrderId,omitempty"`
	Side                    OrderSide               `json:"side"`
	Quantity                float64                 `json:"quantity"`
	LimitClientOrderId      string                  `json:"limitClientOrderId"`
	LimitStrategyId         int64                   `json:"limitStrategyId,omitempty"`
	LimitStrategyType       int64                   `json:"limitStrategyType,omitempty"`
	Price                   float64                 `json:"price"`
	LimitIcebergQty         float64                 `json:"limitIcebergQty,omitempty"`
	TrailingDelta           int64                   `json:"trailingDelta,omitempty"`
	StopClientOrderId       string                  `json:"stopClientOrderId,omitempty"`
	StopPrice               float64                 `json:"stopPrice"`
	StopStrategyId          int64                   `json:"stopStrategyId,omitempty"`
	StopStrategyType        int64                   `json:"stopStrategyType,omitempty"`
	StopLimitPrice          float64                 `json:"stopLimitPrice,omitempty"`
	StopIcebergQty          float64                 `json:"stopIcebergQty,omitempty"`
	StopLimitTimeInForce    StopLimitTimeInForce    `json:"stopLimitTimeInForce,omitempty"`
	NewOrderRespType        OrderRespType           `json:"newOrderRespType,omitempty"`
	SelfTradePreventionMode SelfTradePreventionMode `json:"selfTradePreventionMode,omitempty"`
	RecvWindow              int64                   `json:"recvWindow,omitempty"`
	Timestamp               int64                   `json:"timestamp"`
}

func (o *PostOrderOCO) String() string {
	var sb strings.Builder

	if o.Symbol != "" {
		sb.WriteString(fmt.Sprintf("symbol=%s", strings.ToUpper(o.Symbol)))
	}
	if o.ListClientOrderId != "" {
		sb.WriteString(fmt.Sprintf("&listClientOrderId=%s", o.ListClientOrderId))
	}
	if o.Side != "" {
		sb.WriteString(fmt.Sprintf("&side=%s", o.Side))
	}
	if o.Quantity != 0.0 {
		sb.WriteString(fmt.Sprintf("&quantity=%f", o.Quantity))
	}
	if o.LimitClientOrderId != "" {
		sb.WriteString(fmt.Sprintf("&limitClientOrderId=%s", o.LimitClientOrderId))
	}
	if o.LimitStrategyId != 0 {
		sb.WriteString(fmt.Sprintf("&limitStrategyId=%d", o.LimitStrategyId))
	}
	if o.LimitStrategyType != 0 {
		sb.WriteString(fmt.Sprintf("&limitStrategyType=%d", o.LimitStrategyType))
	}
	if o.Price != 0.0 {
		sb.WriteString(fmt.Sprintf("&price=%f", o.Price))
	}
	if o.LimitIcebergQty != 0.0 {
		sb.WriteString(fmt.Sprintf("&limitIcebergQty=%f", o.LimitIcebergQty))
	}
	if o.TrailingDelta != 0 {
		sb.WriteString(fmt.Sprintf("&trailingDelta=%d", o.TrailingDelta))
	}
	if o.StopClientOrderId != "" {
		sb.WriteString(fmt.Sprintf("&stopClientOrderId=%s", o.StopClientOrderId))
	}
	if o.StopPrice != 0.0 {
		sb.WriteString(fmt.Sprintf("&stopPrice=%f", o.StopPrice))
	}
	if o.StopStrategyId != 0 {
		sb.WriteString(fmt.Sprintf("&stopStrategyId=%d", o.StopStrategyId))
	}
	if o.StopStrategyType != 0 {
		sb.WriteString(fmt.Sprintf("&stopStrategyType=%d", o.StopStrategyType))
	}
	if o.StopLimitPrice != 0.0 {
		sb.WriteString(fmt.Sprintf("&stopLimitPrice=%f", o.StopLimitPrice))
	}
	if o.StopIcebergQty != 0.0 {
		sb.WriteString(fmt.Sprintf("&stopIcebergQty=%f", o.StopIcebergQty))
	}
	if o.StopLimitTimeInForce != "" {
		sb.WriteString(fmt.Sprintf("&stopLimitTimeInForce=%s", o.StopLimitTimeInForce))
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

type OCOStatus string

const (
	RESPONSE     OCOStatus = "RESPONSE"
	EXEC_STARTED           = "EXEC_STARTED"
	ALL_DONE               = "ALL_DONE"
)

type OCOOrderStatus string

const (
	EXECUTING OCOOrderStatus = "EXECUTING"
	ALL_DONE2                = "ALL_DONE"
	REJECT                   = "REJECT"
)

type PostOrderOCOResponse struct {
	OrderListID       int64             `json:"orderListId"`
	ContingencyType   string            `json:"contingencyType"`
	ListStatusType    OCOStatus         `json:"listStatusType"`
	ListOrderStatus   OCOOrderStatus    `json:"listOrderStatus"`
	ListClientOrderID string            `json:"listClientOrderId"`
	TransactionTime   int64             `json:"transactionTime"`
	Symbol            string            `json:"symbol"`
	Orders            json.RawMessage   `json:"-"`
	OrderReports      []PostOrderReport `json:"orderReports"`
}

type PostOrderReport = PostOrderResponse

type CancelRestrictions string

const (
	ONLY_NEW              CancelRestrictions = "ONLY_NEW"
	ONLY_PARTIALLY_FILLED                    = "ONLY_PARTIALLY_FILLED"
)

type DeleteOrder struct {
	Symbol             string             `json:"symbol"`
	OrderID            int64              `json:"orderId,omitempty"`
	OrigClientOrderID  string             `json:"origClientOrderId,omitempty"`
	NewClientOrderID   string             `json:"newClientOrderId,omitempty"`
	CancelRestrictions CancelRestrictions `json:"cancelRestrictions,omitempty"`
	RecvWindow         int64              `json:"recvWindow,omitempty"`
	Timestamp          int64              `json:"timestamp"`
}

type DeleteOrderResponse struct {
	Symbol                  string                  `json:"symbol"`
	OrigClientOrderID       string                  `json:"origClientOrderId"`
	OrderID                 int64                   `json:"orderId"`
	OrderListID             int64                   `json:"orderListId"`
	ClientOrderID           string                  `json:"clientOrderId"`
	TransactTime            int64                   `json:"transactTime"`
	Price                   string                  `json:"price"`
	OrigQty                 string                  `json:"origQty"`
	ExecutedQty             string                  `json:"executedQty"`
	CummulativeQuoteQty     string                  `json:"cummulativeQuoteQty"`
	Status                  OrderStatus             `json:"status"`
	TimeInForce             string                  `json:"timeInForce"`
	Type                    OrderType               `json:"type"`
	Side                    OrderSide               `json:"side"`
	SelfTradePreventionMode SelfTradePreventionMode `json:"selfTradePreventionMode"`
}

func (dor *DeleteOrderResponse) DeleteToGet() *GetOrderResponse {
	return &GetOrderResponse{
		Symbol:                  dor.Symbol,
		OrderID:                 dor.OrderID,
		OrderListID:             dor.OrderListID,
		ClientOrderID:           dor.ClientOrderID,
		Price:                   dor.Price,
		OrigQty:                 dor.OrigQty,
		ExecutedQty:             dor.ExecutedQty,
		CummulativeQuoteQty:     dor.CummulativeQuoteQty,
		Status:                  dor.Status,
		TimeInForce:             TimeInForce(dor.TimeInForce),
		Time:                    dor.TransactTime,
		Type:                    OrderType(dor.Type),
		Side:                    OrderSide(dor.Side),
		SelfTradePreventionMode: dor.SelfTradePreventionMode,
	}
}

type Trade struct {
	Strategy        string    `json:"strategy_name,omitempty"`
	Status          string    `json:"status,omitempty"`
	Symbol          string    `json:"symbol"`
	OrderID         int64     `json:"orderId,omitempty"`
	OrderListID     int64     `json:"orderListId,omitempty"`
	Price           string    `json:"price"`
	Qty             string    `json:"qty,omitempty"`
	QuoteQty        string    `json:"quoteQty,omitempty"`
	Commission      string    `json:"commission,omitempty"`
	CommissionAsset string    `json:"commissionAsset,omitempty"`
	Time            time.Time `json:"time"`
	IsBuyer         bool      `json:"isBuyer,omitempty"`
	IsMaker         bool      `json:"isMaker,omitempty"`
	IsBestMatch     bool      `json:"isBestMatch,omitempty"`
}

type BotStatus string

const (
	ACTIVE   BotStatus = "ACTIVE"
	DISABLED           = "DISABLED"
)

type TradingBot struct {
	ID        int       `json:"id"`
	Symbol    string    `json:"symbol"`
	Interval  string    `json:"interval"`
	Strategy  string    `json:"strategy"`
	Status    BotStatus `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type FilterType string

const (
	PRICE_FILTER          FilterType = "PRICE_FILTER"
	LOT_SIZE                         = "LOT_SIZE"
	ICEBERG_PARTS                    = "ICEBERG_PARTS"
	MARKET_LOT_SIZE                  = "MARKET_LOT_SIZE"
	TRAILING_DELTA                   = "TRAILING_DELTA"
	PERCENT_PRICE_BY_SIDE            = "PERCENT_PRICE_BY_SIDE"
	NOTIONAL                         = "NOTIONAL"
	MAX_NUM_ORDERS                   = "MAX_NUM_ORDERS"
	MAX_NUM_ALGO_ORDERS              = "MAX_NUM_ALGO_ORDERS"
)

type Filter interface {
	GetFilterType() FilterType
}

type BaseFilter struct {
	FilterType FilterType `json:"filterType"`
}

func (f *BaseFilter) GetFilterType() FilterType {
	return f.FilterType
}

type PriceFilter struct {
	BaseFilter
	MaxPrice string `json:"maxPrice"`
	MinPrice string `json:"minPrice"`
	TickSize string `json:"tickSize"`
}

type LotSizeFilter struct {
	BaseFilter
	MaxQty   string `json:"maxQty"`
	MinQty   string `json:"minQty"`
	StepSize string `json:"stepSize"`
}

type IcebergPartsFilter struct {
	BaseFilter
	Limit int `json:"limit"`
}

type MarketLotSizeFilter struct {
	BaseFilter
	MaxQty   string `json:"maxQty"`
	MinQty   string `json:"minQty"`
	StepSize string `json:"stepSize"`
}

type TrailingDeltaFilter struct {
	BaseFilter
	MaxTrailingAboveDelta int `json:"maxTrailingAboveDelta"`
	MaxTrailingBelowDelta int `json:"maxTrailingBelowDelta"`
	MinTrailingAboveDelta int `json:"minTrailingAboveDelta"`
	MinTrailingBelowDelta int `json:"minTrailingBelowDelta"`
}

type PercentPriceBySideFilter struct {
	BaseFilter
	AskMultiplierDown string `json:"askMultiplierDown"`
	AskMultiplierUp   string `json:"askMultiplierUp"`
	AvgPriceMins      int    `json:"avgPriceMins"`
	BidMultiplierDown string `json:"bidMultiplierDown"`
	BidMultiplierUp   string `json:"bidMultiplierUp"`
}

type NotionalFilter struct {
	BaseFilter
	ApplyMaxToMarket bool   `json:"applyMaxToMarket"`
	ApplyMinToMarket bool   `json:"applyMinToMarket"`
	MaxNotional      string `json:"maxNotional"`
	MinNotional      string `json:"minNotional"`
}

type MaxNumOrdersFilter struct {
	BaseFilter
	MaxNumOrders int `json:"maxNumOrders"`
}

type MaxNumAlgoOrdersFilter struct {
	BaseFilter
	MaxNumAlgoOrders int `json:"maxNumAlgoOrders"`
}

type SymbolFilter struct {
	Symbol     string   `json:"symbol"`
	BaseAsset  string   `json:"baseAsset"`
	QuoteAsset string   `json:"quoteAsset"`
	Filters    []Filter `json:"filters"`
}

func (sf *SymbolFilter) UnmarshalJSON(data []byte) error {
	var temp struct {
		Symbol     string            `json:"symbol"`
		BaseAsset  string            `json:"baseAsset"`
		QuoteAsset string            `json:"quoteAsset"`
		RawFilters []json.RawMessage `json:"filters"`
	}

	if err := json.Unmarshal(data, &temp); err != nil {
		return err
	}

	sf.Symbol = temp.Symbol
	sf.BaseAsset = temp.BaseAsset
	sf.QuoteAsset = temp.QuoteAsset

	for _, rawFilter := range temp.RawFilters {
		var baseFilter BaseFilter
		if err := json.Unmarshal(rawFilter, &baseFilter); err != nil {
			return err
		}

		var filter Filter
		switch baseFilter.GetFilterType() {
		case PRICE_FILTER:
			filter = &PriceFilter{}
		case LOT_SIZE:
			filter = &LotSizeFilter{}
		case ICEBERG_PARTS:
			filter = &IcebergPartsFilter{}
		case MARKET_LOT_SIZE:
			filter = &MarketLotSizeFilter{}
		case TRAILING_DELTA:
			filter = &TrailingDeltaFilter{}
		case PERCENT_PRICE_BY_SIDE:
			filter = &PercentPriceBySideFilter{}
		case NOTIONAL:
			filter = &NotionalFilter{}
		case MAX_NUM_ORDERS:
			filter = &MaxNumOrdersFilter{}
		case MAX_NUM_ALGO_ORDERS:
			filter = &MaxNumAlgoOrdersFilter{}
		}

		if err := json.Unmarshal(rawFilter, filter); err != nil {
			return err
		}

		sf.Filters = append(sf.Filters, filter)
	}

	return nil
}

type TradeFilters struct {
	PriceFilter
	LotSizeFilter
	NotionalFilter
	TrailingDeltaFilter
}

type MaterializedViewConfig struct {
	Name             string
	Interval         string
	StartOffset      string
	EndOffset        string
	ScheduleInterval string
	CompressAfter    string
}

func (mvc *MaterializedViewConfig) CreateQuery() string {
	return fmt.Sprintf(`
        CREATE MATERIALIZED VIEW binance.%s
        WITH (timescaledb.continuous) AS 
        SELECT 
            kd.symbol_interval_id as siid,
            time_bucket(INTERVAL '%s', kd.open_time) as bucket,
            FIRST(kd.open, kd.open_time) as first_open,
            MAX(kd.high) as max_high,
            MIN(kd.low) as min_low,
            LAST(kd.close, kd.close_time) as last_close,
            SUM(kd.volume) as total_volume
        FROM binance.kline AS kd
        GROUP BY bucket, kd.symbol_interval_id
        WITH NO DATA;`, mvc.Name, mvc.Interval)
}

func (mvc *MaterializedViewConfig) CreateContAggPolicyQuery() string {
	return fmt.Sprintf(`
        SELECT add_continuous_aggregate_policy(
            'binance.%s',
            start_offset => INTERVAL '%s',
            end_offset => INTERVAL '%s',
            schedule_interval => INTERVAL '%s');`,
		mvc.Name, mvc.StartOffset, mvc.EndOffset, mvc.ScheduleInterval)
}

func (mvc *MaterializedViewConfig) CreateIndexQuery() string {
	return fmt.Sprintf(`CREATE INDEX ON binance.%s (bucket, siid);`, mvc.Name)
}

func (mvc *MaterializedViewConfig) CreateAddCompPolicyQuery() string {
	return fmt.Sprintf(`
        ALTER MATERIALIZED VIEW binance.%s SET (timescaledb.compress = true);
        SELECT add_compression_policy('binance.%s', compress_after => INTERVAL '%s');`,
		mvc.Name, mvc.Name, mvc.CompressAfter)
}

type MaterializedViewExistsError struct {
	ViewName string
}

func (e *MaterializedViewExistsError) Error() string {
	return fmt.Sprintf("materialized view %s already exists", e.ViewName)
}

type OrderInfo struct {
	ID           int64
	Symbol       string
	Side         OrderSide // "BUY" or "SELL"
	Type         OrderType // "LIMIT", "MARKET", etc.
	Status       OrderStatus
	CurrentPrice float64
	EntryPrice   float64
	SellLevel    float64
	GridLevel    Level
}

// Maximum reachable grid level ranging from 0 (base) to 4 (5 levels total).
// Acts as sort of a stop stop-loss.
type Level int

const (
	InvalidLevel         Level = 100
	NegativeMaxGridLevel Level = iota - 5
	NegativeBreakEvenLevel
	NegativeHalfRetracementLevel
	NegativeFullRetracementLevel
	BaseLevel
	FullRetracementLevel
	HalfRetracementLevel
	BreakEvenLevel
	MaxGridLevel
)

func (l Level) String() string {
	switch l {
	case NegativeMaxGridLevel:
		return "-MAX_GRID_LEVEL"
	case NegativeBreakEvenLevel:
		return "-BREAK_EVEN_LEVEL"
	case NegativeHalfRetracementLevel:
		return "-HALF_RETRACEMENT_LEVEL"
	case NegativeFullRetracementLevel:
		return "-FULL_RETRACEMENT_LEVEL"
	case BaseLevel:
		return "BASE_LEVEL"
	case FullRetracementLevel:
		return "FULL_RETRACEMENT_LEVEL"
	case HalfRetracementLevel:
		return "HALF_RETRACEMENT_LEVEL"
	case BreakEvenLevel:
		return "BREAK_EVEN_LEVEL"
	case MaxGridLevel:
		return "MAX_GRID_LEVEL"
	case InvalidLevel:
		return "INVALID_LEVEL"
	default:
		return fmt.Sprintf("Unknown Level [%d]", l)
	}
}

func (l *Level) IncreaseLevel() {
	if *l == InvalidLevel {
		*l = BaseLevel
	} else if *l < MaxGridLevel {
		*l++
	} else {
		*l = InvalidLevel // Reset to base level if it goes beyond maxGridLevel
	}
}

func (l *Level) DecreaseLevel() {
	if *l == InvalidLevel {
		*l = BaseLevel
	} else if *l > NegativeMaxGridLevel {
		*l--
	} else {
		*l = InvalidLevel // Reset to base level if it goes beyond -maxGridLevel
	}
}
