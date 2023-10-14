package api

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/adamdenes/gotrade/internal/logger"
	"github.com/adamdenes/gotrade/internal/models"
	"github.com/adamdenes/gotrade/internal/storage"
	"github.com/jackc/pgx/v5"
)

// os.TempDir() ?
const tmpDir string = "./internal/tmp/"

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
			"./web/templates/pages/home.tmpl.html",
			"./web/templates/pages/chart.tmpl.html",
			"./web/templates/pages/backtest.tmpl.html",
			"./web/templates/partials/header.tmpl.html",
			"./web/templates/partials/bt.tmpl.html",
			"./web/templates/partials/script.tmpl.html",
			"./web/templates/partials/search_bar.tmpl.html",
			"./web/templates/partials/dropdown_tf.tmpl.html",
			"./web/templates/partials/dropdown_bt.tmpl.html",
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

// ----------------- MISC -----------------

// `PollHistoricalData` is tasked to periodically poll binance data-api
// for updated candels.
func PollHistoricalData(storage storage.Storage) {
	var startDate, endDate time.Time

	printCount := 0

	for {
		// Query SQL for the last close_time
		row, err := storage.QueryLastRow()
		if err != nil {
			if err == sql.ErrNoRows || err == pgx.ErrNoRows {
				logger.Info.Println("Database is empty. Full update needed.")
				// Poll one year worth of data
				startDate = time.Now().AddDate(-1, 0, 0)
				endDate = time.Now()
				update(storage, true, startDate, endDate)
			} else {
				logger.Error.Panicf("error getting last close_time: %v\n", err)
			}
		}

		epoch := row.CloseTime
		var (
			eYear    int        = epoch.Year()
			eMonth   time.Month = epoch.Month()
			eDay     int        = epoch.Day()
			year     int        = time.Now().Year()
			month    time.Month = time.Now().Month()
			firstDay int        = time.Date(year, month, 1, 0, 0, 0, 0, time.Local).Day()
		)
		// Monthly-generated data will be updated on the first day of the following month.
		// For streaming ZIP files, we could simply wait for the next month's release. However,
		// if there would be data gaps mid-month for whatever reason, streaming would'nt be able to update the DB.
		// That is why we make GET requests for 1000 data points each iteration, and gradually update the DB.
		logger.Info.Printf(
			"%v %v.%02d <-> %v %v.%02d\n",
			eYear,
			eMonth,
			eDay,
			year,
			month,
			firstDay,
		)
		if eYear <= year && eMonth < month {
			if printCount == 1 {
				logger.Info.Println("PARTIAL database update needed")
			}

			printCount++
			// Next second
			row.OpenTime = row.CloseTime.Add(1 * time.Millisecond)
			row.CloseTime = row.OpenTime.Add(999 * time.Millisecond)

			uri := BuildURI(
				"https://data-api.binance.vision/api/v3/klines?",
				"symbol=", row.Symbol,
				"&interval=", row.Interval,
				"&startTime=", fmt.Sprintf("%d", row.OpenTime.UnixMilli()),
				"&limit=1000",
			)

			b, err := Query(uri)
			if err != nil {
				if re, ok := err.(*models.RequestError); ok && err != nil {
					time.Sleep(re.Timer * time.Second)
					continue
				}
				logger.Error.Printf("Error making query: %v\n", err)
				return
			}

			if err := storage.Copy(b, &row.Symbol, &row.Interval); err != nil {
				logger.Error.Printf("Error inserting Kline data into the database: %v\n", err)
				return
			}
		} else {
			// Wait 24 hours and start again
			logger.Info.Println("Database is up to date! Polling is going to sleep...")
			time.Sleep(time.Hour * 24)
		}
	}
}

func update(s storage.Storage, isFull bool, startDate, endDate time.Time) {
	// always get 1s interval data -> aggregate later
	var wg sync.WaitGroup

	currYear := endDate.Year()
	currMonth := endDate.Month()
	startYear := startDate.Year()
	startMonth := startDate.Month()

	symbols := []string{"BTCUSDT"} //, "ETHBTC", "BNBUSDT", "XRPUSDT"}

	for _, symbol := range symbols {
		for y := startYear; y <= currYear; y++ {
			// Loop from start month to end of the year (December)
			for m := time.January; m <= time.December; m++ {
				wg.Add(1)
				if isFull {
					if y < currYear && startMonth > m || currYear == y && currMonth <= m {
						// logger.Debug.Printf("Skipping: year=%v, month=%v\n", y, m)
						continue
					}
				} else {
					if y <= currYear && startMonth > m || currYear == y && currMonth <= m {
						// logger.Debug.Printf("Skipping: year=%v, month=%v\n", y, m)
						continue
					}
				}
				go processMonthlyData(symbol, y, int(m), s, &wg)
			}
		}
	}
	wg.Wait()
}

// Concurrently make HTTP requests for given URLs (zip files), and stream them to the database
func processMonthlyData(
	symbol string,
	year, month int,
	storage storage.Storage,
	wg *sync.WaitGroup,
) {
	defer wg.Done()
	uri := BuildURI("https://data.binance.vision/data/spot/monthly/klines/",
		symbol,
		"/1s/",
		symbol,
		"-1s-",
		fmt.Sprintf("%d-%02d.zip", year, month),
	)
	logger.Info.Printf("GET: %v\n", uri)

	dtime := time.Now()
	reader, err := stream(uri)
	if err != nil {
		logger.Error.Printf("Error streaming data over HTTP: %v\n", err)
		return
	}

	logger.Info.Printf("Download time: %v", time.Since(dtime))
	if err := storage.Stream(reader); err != nil {
		logger.Error.Printf("Error inserting Kline data into the database: %v\n", err)
		return
	}
}

// Stream will return a *zip.Reader from which the zip data can be read.
// It enables seamless data streaming directly between HTTP and PostgresSQL,
// reducing memory consumption and minimizing IO operations.
func stream(url string) (*zip.Reader, error) {
	b, err := Query(url)
	if err != nil {
		return nil, err
	}
	// Read the entire response body into a buffer.
	bodyBuffer := bytes.NewBuffer(b)
	// Create a bytes.Reader from the buffer.
	bodyReader := bytes.NewReader(bodyBuffer.Bytes())
	// Get the zip reader
	zipReader, err := zip.NewReader(bodyReader, int64(bodyBuffer.Len()))
	if err != nil {
		return nil, fmt.Errorf("error creating zip reader: %v", err)
	}

	return zipReader, err
}

// Open a zip archive and return a *zip.Reader struct from it
func streamZIP(path string) (*zip.Reader, error) {
	zipFile, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	zipSize, err := zipFile.Stat()
	if err != nil {
		return nil, err
	}

	zipReader, err := zip.NewReader(zipFile, zipSize.Size())
	if err != nil {
		return nil, err
	}

	return zipReader, err
}

// Download kline data with 1s interval in ZIP format
func downloadZIP(url string) error {
	// Send HTTP GET request to download the ZIP file
	resp, err := Query(url)
	if err != nil {
		return err
	}

	// Create or open the destination file for writing
	dst := tmpDir + filepath.Base(url)
	file, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer file.Close()

	b := bytes.NewReader(resp)
	// Copy the response body to the destination file
	_, err = io.Copy(file, b)
	if err != nil {
		return err
	}

	logger.Info.Printf("Downloaded zip file to: %s\n", dst)
	return nil
}

// Unzip the files from donwloadZIP
func unzipFile(zipPath, destPath string) error {
	// Open zip file for reading
	zipReader, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zipReader.Close()

	for _, file := range zipReader.File {
		// Create new file for writing
		outFile, err := os.Create(destPath + "/" + file.Name)
		if err != nil {
			return err
		}
		defer outFile.Close()

		// Open file from archive
		zipFile, err := file.Open()
		if err != nil {
			return err
		}
		defer zipFile.Close()

		// Copy content into the new file
		_, err = io.Copy(outFile, zipFile)
		if err != nil {
			return err
		}
	}

	logger.Info.Printf("Unzipped file: %s\n", zipReader.File[0].Name)
	return nil
}

// Create a csv reader and read all rows in a single file
func readCSVFile(filePath string) ([][]string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	csvReader := csv.NewReader(file)
	records, err := csvReader.ReadAll()
	if err != nil {
		return nil, err
	}

	return records, nil
}

func WriteJSON(w http.ResponseWriter, status int, v any) error {
	// []byte slices are not converting correctly, so I need to type switch
	switch data := v.(type) {
	case []byte:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, err := w.Write(data)
		return err
	default:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		return json.NewEncoder(w).Encode(data)
	}
}

func ValidateSymbol(symbol string, symbolCache map[string]struct{}) error {
	if symbol == "" {
		return fmt.Errorf("no symbol provided: %q", symbol)
	}
	if sc, ok := symbolCache[symbol]; !ok {
		return fmt.Errorf("symbol not found: %v", sc)
	}
	return nil
}

// Return an error if time input validation fails, or nil if they are valid
func ValidateTimes(start, end interface{}) ([]time.Time, error) {
	var t []time.Time

	st, err := toTime(start)
	if err != nil {
		return nil, fmt.Errorf("invalid start time format: %v", err)
	}

	et, err := toTime(end)
	if err != nil {
		return nil, fmt.Errorf("invalid end time format: %v", err)
	}

	t = append(t, st, et)
	return t, nil
}

func toTime(v interface{}) (time.Time, error) {
	switch v := v.(type) {
	case string:
		return stringToTime(v)
	case time.Time:
		return v, nil
	default:
		return time.Time{}, fmt.Errorf("unsupported type")
	}
}

func stringToTime(str string) (time.Time, error) {
	const layout = "2006-01-02 15:04:05 -0700 MST"
	if _, err := strconv.Atoi(str); err == nil {
		// Convert string to int64
		unixTime, err := strconv.ParseInt(str, 10, 64)
		if err != nil {
			return time.Time{}, err
		}
		// Convert Unix timestamp to time.Time
		return time.UnixMilli(unixTime), nil
	}
	return time.Parse(layout, str)
}

func BuildURI(base string, query ...string) string {
	var sb strings.Builder
	sb.WriteString(base)
	for _, q := range query {
		// Check if the query string starts with "symbol="
		if strings.HasPrefix(q, "symbol=") {
			parts := strings.Split(q, "&")
			part := strings.Split(parts[0], "=")
			part[1] = strings.ToUpper(part[1])
			parts[0] = strings.Join(part, "=")
			sb.WriteString(strings.Join(parts, "&"))
		} else {
			sb.WriteString(q)
		}
	}
	return sb.String()
}

// ----------------- REST -----------------

/* IP Limits

   - Every request will contain X-MBX-USED-WEIGHT-(intervalNum)(intervalLetter) in the response headers which has the current used weight for the IP for all request rate limiters defined.
   - Each route has a weight which determines for the number of requests each endpoint counts for. Heavier endpoints and endpoints that do operations on multiple symbols will have a heavier weight.
   - When a 429 is received, it's your obligation as an API to back off and not spam the API.
   - Repeatedly violating rate limits and/or failing to back off after receiving 429s will result in an automated IP ban (HTTP status 418).
   - IP bans are tracked and scale in duration for repeat offenders, from 2 minutes to 3 days.
   - A Retry-After header is sent with a 418 or 429 responses and will give the number of seconds required to wait, in the case of a 429, to prevent a ban, or, in the case of a 418, until the ban is over.
   - The limits on the API are based on the IPs, not the API keys.
*/

// Query makes a GET request for the given query string/url with an additional backoff timer.
// Once a "Retry-After" header is received, the query mechanism will go to sleep. The caller
// has to implement retry mechanism.
func Query(qs string) ([]byte, error) {
	resp, err := http.Get(qs)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		logger.Debug.Printf("HTTP Status code: %v, X-Mbx-Used-Weight: %q, Retry-After: %q\n",
			resp.StatusCode,
			resp.Header.Get("X-Mbx-Used-Weight"),
			resp.Header.Get("Retry-After"),
		)
		logger.Error.Printf("RETRY AFTER RECEIVED: %q\n", resp.Header.Values("Retry-After"))
		// Get the backoff timer from respons body
		timer, err := strconv.ParseInt(resp.Header.Get("Retry-After"), 10, 64)
		if err != nil {
			return nil, err
		}
		logger.Error.Printf(
			"%v Retry-After received, backing off for: %d\n",
			resp.StatusCode,
			timer,
		)

		return nil, &models.RequestError{
			Err:    errors.New("ErrBackOff"),
			Timer:  time.Duration(timer),
			Status: resp.StatusCode,
		}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, err
	}

	r, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return r, nil
}

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

func getKlines(q ...string) ([]byte, error) {
	uri := BuildURI("https://data-api.binance.vision/api/v3/klines?", q...)
	resp, err := Query(uri)
	if err != nil {
		return nil, err
	}
	return resp, nil
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

func getUiKlines(q ...string) ([]byte, error) {
	uri := BuildURI("https://data-api.binance.vision/api/v3/uiKlines?", q...)
	resp, err := Query(uri)
	if err != nil {
		return nil, err
	}
	return resp, nil
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

func NewSymbolCache() (map[string]struct{}, error) {
	uri := BuildURI("https://data-api.binance.vision/api/v3/exchangeInfo")
	resp, err := Query(uri)
	if err != nil {
		return nil, err
	}

	// Define a struct to unmarshal the JSON response
	var exchangeInfo struct {
		Symbols []struct {
			Symbol string `json:"symbol"`
		} `json:"symbols"`
	}

	// Parse the JSON response
	err = json.Unmarshal(resp, &exchangeInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	cache := make(map[string]struct{}, len(exchangeInfo.Symbols))
	for _, s := range exchangeInfo.Symbols {
		cache[s.Symbol] = struct{}{}
	}

	return cache, nil
}

func getSymbols() ([]string, error) {
	uri := BuildURI("https://data-api.binance.vision/api/v3/exchangeInfo")
	resp, err := Query(uri)
	if err != nil {
		return nil, err
	}

	// Define a struct to unmarshal the JSON response
	var exchangeInfo struct {
		Symbols []struct {
			Symbol string `json:"symbol"`
		} `json:"symbols"`
	}

	// Parse the JSON response
	err = json.Unmarshal(resp, &exchangeInfo)
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
