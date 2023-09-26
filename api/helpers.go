package api

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"path/filepath"
	"runtime/debug"
	"strings"
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
	fmt.Printf("%+v\n", cache)

	return cache, nil
}

// LOOK into template cache
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

// Download kline data with 1s interval in ZIP format and reads it into memory
// for further processing (dumping into database)
// func downloadAndReadZIP(url string) (*zip.ReadCloser, error) {
// 	// Send HTTP GET request to download the ZIP file
// 	response, err := http.Get(url)
// 	if err != nil {
// 		return nil, err
// 	}
// 	defer response.Body.Close()

// 	// Read the response body into a byte buffer
// 	buffer := new(bytes.Buffer)
// 	_, err = io.Copy(buffer, response.Body)
// 	if err != nil {
// 		return nil, err
// 	}

// 	// New reader for the in-memory ZIP archive
// 	zipReader, err := zip.NewReader(bytes.NewReader(buffer.Bytes()), int64(buffer.Len()))
// 	if err != nil {
// 		return nil, err
// 	}

// 	return &zip.ReadCloser{Reader: *zipReader}, nil
// }

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

func getKlines(s string, i string) ([]byte, error) {
	var sb strings.Builder
	sb.WriteString("https://data-api.binance.vision/api/v3/klines?")
	sb.WriteString(fmt.Sprintf("symbol=%s", s))
	sb.WriteString(fmt.Sprintf("&interval=%s", i))

	resp, err := http.Get(sb.String())
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

func getUiKlines(s string, i string) ([]byte, error) {
	var sb strings.Builder
	sb.WriteString("https://data-api.binance.vision/api/v3/uiKlines?")
	sb.WriteString(fmt.Sprintf("symbol=%s", s))
	sb.WriteString(fmt.Sprintf("&interval=%s", i))

	resp, err := http.Get(sb.String())
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
