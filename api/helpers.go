package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/adamdenes/gotrade/internal/logger"
)

func NewTemplateCache() (map[string]*template.Template, error) {
	cache := map[string]*template.Template{}

	pages, err := filepath.Glob("./web/templates/pages/*.tmpl.html")
	if err != nil {
		return nil, err
	}

	for _, page := range pages {
		name := filepath.Base(page)

		file := []string{
			"./web/templates/base.tmpl.html",
			"./web/templates/pages/chart.tmpl.html",
			"./web/templates/partials/header.tmpl.html",
			"./web/templates/partials/script.tmpl.html",
			"./web/templates/partials/search_bar.tmpl.html",
			"./web/templates/partials/dropdown_tf.tmpl.html",
			page,
		}

		ts, err := template.ParseFiles(file...)
		if err != nil {
			return nil, err
		}

		cache[name] = ts
	}
	return cache, nil
}

func (s *Server) render(w http.ResponseWriter, status int, page string, data any) {
	ts, ok := s.templateCache[page]
	if !ok {
		s.serverError(w, fmt.Errorf("the template '%s' does not exists", page))
		return
	}

	w.WriteHeader(status)
	// Render the template
	err := ts.ExecuteTemplate(w, "base", data)
	if err != nil {
		s.serverError(w, err)
	}
}

func (s *Server) serverError(w http.ResponseWriter, err error) {
	trace := fmt.Sprintf("%s\n%s", err.Error(), debug.Stack())
	s.errorLog.Output(2, trace)

	http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
}

func (s *Server) clientError(w http.ResponseWriter, status int) {
	http.Error(w, http.StatusText(status), status)
}

func (s *Server) notFound(w http.ResponseWriter) {
	s.clientError(w, http.StatusNotFound)
}

// ----------------- MISC -----------------

// `PollHistoricalData` is tasked to periodically poll binance data-api
// for updated candels.
func PollHistoricalData() {
	// always get 1s interval data -> aggregate later
	// let's start with monthly files
	var wg sync.WaitGroup

	startYear := 2018
	currYear := time.Now().Year()
	currMonth := time.Now().Month()

	symbols := []string{"BTCUSDT", "ETHBTC", "BNBUSDT", "XRPUSDT"}

	// TODO: if file exist also skip it!!!
	for _, symbol := range symbols {
		for year := startYear; year <= currYear; year++ {
			for month := time.January; month <= time.December; month++ {
				wg.Add(1)
				if currYear == year && currMonth <= month {
					logger.Debug.Printf("Skipping: year=%v, month=%v\n", year, month)
					continue
				}
				go downloadMonthlyData(symbol, year, int(month), &wg)
			}
		}
	}
	wg.Wait()

	// Binance endpoint (daily / montly)
	// /spot/daily/klines/{SYMBOL}/{INTERVA}/{SYMBOL}-{INTERVAL}-{YEAR-MONTH-DAY}.zip
	// /spot/monthly/klines/{SYMBOL}/{INTERVA}/{SYMBOL}-{INTERVAL}-{YEAR-MONTH}.zip

	// https://data.binance.vision/data/spot/monthly/klines/BTCUSDT/1s/BTCUSDT-1s-2023-08.zip

	// 4. save into db
}

func downloadMonthlyData(symbol string, year, month int, wg *sync.WaitGroup) {
	defer wg.Done()

	sb := constructURL(symbol, year, month)
	logger.Debug.Printf("GET: %v\n", sb.String())

	if err := downloadZIP(sb.String()); err != nil {
		logger.Error.Printf("uri: %s -> err: %v\n", sb.String(), err)
	}

	sb.Reset()
}

func constructURL(symbol string, year, month int) *strings.Builder {
	const baseUri = "https://data.binance.vision/data/spot/monthly/klines/"
	sb := &strings.Builder{}

	sb.WriteString(baseUri)
	sb.WriteString(symbol)
	sb.WriteString("/1s/")
	sb.WriteString(symbol)
	sb.WriteString("-1s-")

	if month < 10 {
		sb.WriteString(fmt.Sprintf("%d-0%d.zip", year, month))
	} else {
		sb.WriteString(fmt.Sprintf("%d-%d.zip", year, month))
	}
	return sb
}

// Download kline data with 1s interval in ZIP format and reads it into memory
// for further processing (dumping into database)
func downloadZIP(url string) error {
	// Send HTTP GET request to download the ZIP file
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP request failed with status code %d", resp.StatusCode)
	}

	// Create or open the destination file for writing
	dst := "./internal/tmp/" + filepath.Base(url)
	file, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer file.Close()

	// Copy the response body to the destination file
	_, err = io.Copy(file, resp.Body)
	if err != nil {
		return err
	}

	logger.Info.Printf("Downloaded ZIP file to: %s\n", dst)
	return nil
}

func GET(url string) (*http.Response, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func WriteJSON(w http.ResponseWriter, status int, v any) error {
	w.Header().Add("Content-Type", "application/json")
	w.WriteHeader(status)

	// []byte slices are not converting correctly, so I need to type switch
	switch data := v.(type) {
	case []byte:
		_, err := w.Write(data)
		return err
	default:
		return json.NewEncoder(w).Encode(data)
	}
}

func ValidateSymbol(symbol string) error {
	symbols, err := getSymbols()
	if err != nil {
		return err
	}

	found := false
	for _, s := range symbols {
		if s == symbol {
			found = true
			break
		}
	}

	if !found {
		return errors.New("invalid symbol")
	}

	return nil
}

func ValidateTimes(start, end string) error {
	// Implement validation logic for start and end times here
	// Return an error if validation fails, or nil if they are valid
	return nil
}

func BuildURI(base string, query string) string {
	var sb strings.Builder
	sb.WriteString(base)
	sb.WriteString(query)
	return sb.String()
}

func prepareQuerString(qs string) string {
	result := strings.Split(qs, "&")
	symbolPart := strings.Split(result[0], "=")
	symbolPart[1] = strings.ToUpper(symbolPart[1])
	result[0] = strings.Join(symbolPart, "=")
	return strings.Join(result, "&")
}

// ----------------- REST -----------------
/* The base endpoint https://data-api.binance.vision can be used to access the following API endpoints that have NONE as security type:

   GET /api/v3/aggTrades
   GET /api/v3/avgPrice
   GET /api/v3/depth
   GET /api/v3/exchangeInfo
   GET /api/v3/klines
   GET /api/v3/ping
   GET /api/v3/ticker
   GET /api/v3/ticker/24hr
   GET /api/v3/ticker/bookTicker
   GET /api/v3/ticker/price
   GET /api/v3/time
   GET /api/v3/trades
   GET /api/v3/uiKlines
*/

/*
GET /api/v3/klines

	Kline/candlestick bars for a symbol.
	Klines are uniquely identified by their open time.

	symbol 		STRING 	YES
	fromId 		LONG 	NO 	id to get aggregate trades from INCLUSIVE.
	startTime 	LONG 	NO 	Timestamp in ms to get aggregate trades from INCLUSIVE.
	endTime 	LONG 	NO 	Timestamp in ms to get aggregate trades until INCLUSIVE.
	limit 		INT 	NO 	Default 500; max 1000.


    If startTime and endTime are not sent, the most recent klines are returned.
*/

func getKlines(q string) ([]byte, error) {
	uri := BuildURI("https://data-api.binance.vision/api/v3/uiKlines?", prepareQuerString(q))

	resp, err := http.Get(uri)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Check the HTTP status code
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP request failed with status code %d", resp.StatusCode)
	}

	// Parse the JSON response
	r, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return r, nil
}

/*
GET /api/v3/uiKlines

	The request is similar to klines having the same parameters and response.
	uiKlines return modified kline data, optimized for presentation of candlestick charts.

	symbol 		STRING 	YES
	fromId 		LONG 	NO 	id to get aggregate trades from INCLUSIVE.
	startTime 	LONG 	NO 	Timestamp in ms to get aggregate trades from INCLUSIVE.
	endTime 	LONG 	NO 	Timestamp in ms to get aggregate trades until INCLUSIVE.
	limit 		INT 	NO 	Default 500; max 1000.
*/

func getUiKlines(q string) ([]byte, error) {
	uri := BuildURI("https://data-api.binance.vision/api/v3/uiKlines?", prepareQuerString(q))

	resp, err := http.Get(uri)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Check the HTTP status code
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP request failed with status code %d", resp.StatusCode)
	}

	// Parse the JSON response
	r, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return r, nil
}

/*
GET /api/v3/exchangeInfo

	OPTIONS (not all of them):
	- no parameter	"https://api.binance.com/api/v3/exchangeInfo"
	- symbol		"https://api.binance.com/api/v3/exchangeInfo?symbol=BNBBTC"
	- symbols		'https://api.binance.com/api/v3/exchangeInfo?symbols=["BTCUSDT","BNBBTC"]'
	- permissions	"https://api.binance.com/api/v3/exchangeInfo?permissions=SPOT"

	Notes: If the value provided to symbol or symbols do not exist,
	the endpoint will throw an error saying the symbol is invalid.
*/

func getSymbols() ([]string, error) {
	// Send a GET request to the Binance API endpoint
	resp, err := http.Get("https://data-api.binance.vision/api/v3/exchangeInfo")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Check the HTTP status code
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP request failed with status code %d", resp.StatusCode)
	}

	// Define a struct to unmarshal the JSON response
	var exchangeInfo struct {
		Symbols []struct {
			Symbol string `json:"symbol"`
		} `json:"symbols"`
	}

	// Parse the JSON response
	err = json.NewDecoder(resp.Body).Decode(&exchangeInfo)
	if err != nil {
		return nil, err
	}

	// Extract the symbol names
	symbols := make([]string, len(exchangeInfo.Symbols))
	for i, symbol := range exchangeInfo.Symbols {
		symbols[i] = symbol.Symbol
	}

	return symbols, nil
}
