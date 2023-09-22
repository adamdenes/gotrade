package models

type Kline struct {
	OpenTime              int64  `json:"open_time"`
	OpenPrice             string `json:"open_price"`
	HighPrice             string `json:"high_price"`
	LowPrice              string `json:"low_price"`
	ClosePrice            string `json:"close_price"`
	Volume                string `json:"volume"`
	CloseTime             int64  `json:"close_time"`
	QuoteAssetVolume      string `json:"quote_asset_volume"`
	NumberOfTrades        int    `json:"number_of_trades"`
	TakerBuyBaseAssetVol  string `json:"taker_buy_base_asset_volume"`
	TakerBuyQuoteAssetVol string `json:"taker_buy_quote_asset_volume"`
	UnusedField           string `json:"unusedField"`
}
