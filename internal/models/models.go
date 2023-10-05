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
	Symbol    string `json:"symbol"`
	Interval  string `json:"interval,omitempty"`
	OpenTime  int64  `json:"open_time,omitempty"`
	CloseTime int64  `json:"close_time,omitempty"`
}

func (kr *KlineRequest) String() string {
	var parts []string

	if kr.Symbol != "" {
		parts = append(parts, fmt.Sprintf("symbol=%s", strings.ToUpper(kr.Symbol)))
	}
	if kr.Interval != "" {
		parts = append(parts, fmt.Sprintf("interval=%s", kr.Interval))
	}
	if kr.OpenTime != 0 {
		parts = append(parts, fmt.Sprintf("startTime=%d", kr.OpenTime))
	}
	if kr.CloseTime != 0 {
		parts = append(parts, fmt.Sprintf("endTime=%d", kr.CloseTime))
	}

	return fmt.Sprintf("%s", strings.Join(parts, "&"))
}

type KlineSimple struct {
	OpenTime  int64  `json:"open_time"`
	Open      string `json:"open"`
	High      string `json:"high"`
	Low       string `json:"low"`
	Close     string `json:"close"`
	CloseTime int64  `json:"close_time"`
}

type RequestError struct {
	Err    error
	Status int
	Timer  time.Duration
}

func (e *RequestError) Error() string {
	return e.Err.Error()
}
