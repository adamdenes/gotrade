package api

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/adamdenes/gotrade/cmd/rest"
	"github.com/adamdenes/gotrade/internal/backtest"
	"github.com/adamdenes/gotrade/internal/logger"
	"github.com/adamdenes/gotrade/internal/models"
	"github.com/adamdenes/gotrade/internal/storage"
	"github.com/adamdenes/gotrade/strategy"
	"github.com/jackc/pgx/v5"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

const (
	dataEndpoint   string = "https://data-api.binance.vision/api/v3/"
	historicalData string = "https://data.binance.vision/data/spot/monthly/klines/"
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
			"./web/templates/pages/home.tmpl.html",
			"./web/templates/pages/chart.tmpl.html",
			"./web/templates/pages/backtest.tmpl.html",
			"./web/templates/partials/header.tmpl.html",
			"./web/templates/partials/bt.tmpl.html",
			"./web/templates/partials/script.tmpl.html",
			"./web/templates/partials/search_bar.tmpl.html",
			"./web/templates/partials/dropdown_tf.tmpl.html",
			"./web/templates/partials/dropdown_bt.tmpl.html",
			"./web/templates/partials/dropdown_strats.tmpl.html",
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

				// Create and Update timescale policies
				if err := refreshAggregates(storage); err != nil {
					logger.Error.Fatal("Failed to refresh aggregates:", err)
				}

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

			uri := rest.BuildURI(
				dataEndpoint+"klines?",
				"symbol=", row.Symbol,
				"&interval=", row.Interval,
				"&startTime=", fmt.Sprintf("%d", row.OpenTime.UnixMilli()),
				"&limit=1000",
			)

			b, err := rest.Query("GET", uri, "application/json", nil)
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

func generateMatViewConfigs() []*models.MaterializedViewConfig {
	viewConfigs := map[string]map[string]string{
		"aggregate_1m": {
			"Interval":         "1 minute",
			"StartOffset":      "1 hour",
			"EndOffset":        "1 minute",
			"ScheduleInterval": "5 minutes",
			"CompressAfter":    "7 days",
		},
		"aggregate_5m": {
			"Interval":         "5 minutes",
			"StartOffset":      "1 day",
			"EndOffset":        "5 minutes",
			"ScheduleInterval": "15 minutes",
			"CompressAfter":    "7 days",
		},
		"aggregate_1h": {
			"Interval":         "1 hour",
			"StartOffset":      "1 day",
			"EndOffset":        "1 hour",
			"ScheduleInterval": "2 hours",
			"CompressAfter":    "7 days",
		},
		"aggregate_4h": {
			"Interval":         "4 hours",
			"StartOffset":      "1 day",
			"EndOffset":        "4 hours",
			"ScheduleInterval": "8 hours",
			"CompressAfter":    "7 days",
		},
		"aggregate_1d": {
			"Interval":         "1 day",
			"StartOffset":      "1 week",
			"EndOffset":        "1 day",
			"ScheduleInterval": "2 days",
			"CompressAfter":    "8 days",
		},
		"aggregate_1w": {
			"Interval":         "1 week",
			"StartOffset":      "15 days",
			"EndOffset":        "1 day",
			"ScheduleInterval": "1 week",
			"CompressAfter":    "1 month",
		},
	}

	var materializedViews []*models.MaterializedViewConfig

	for name, config := range viewConfigs {
		mvc := &models.MaterializedViewConfig{
			Name:             name,
			Interval:         config["Interval"],
			StartOffset:      config["StartOffset"],
			EndOffset:        config["EndOffset"],
			ScheduleInterval: config["ScheduleInterval"],
			CompressAfter:    config["CompressAfter"],
		}
		materializedViews = append(materializedViews, mvc)
	}
	return materializedViews
}

func createMatView(s storage.Storage) error {
	mvcs := generateMatViewConfigs()

	for _, mvc := range mvcs {
		// Execute the creation logic for each materialized view
		err := s.ExecuteMaterializedViewCreation(mvc)
		if err != nil {
			return err
		}
		logger.Info.Printf("Materialized view %s created successfully.\n", mvc.Name)
	}
	return nil
}

func refreshAggregates(s storage.Storage) error {
	if err := createMatView(s); err != nil {
		if _, ok := err.(*models.MaterializedViewExistsError); ok {
			logger.Info.Println(err)
			return nil
		}
		logger.Error.Println("Failed to create materialized views:", err)
		return err
	}

	aggregates := []string{
		"binance.aggregate_1w",
		"binance.aggregate_1d",
		"binance.aggregate_4h",
		"binance.aggregate_1h",
		"binance.aggregate_5m",
		"binance.aggregate_1m",
	}
	for _, agg := range aggregates {
		logger.Info.Printf("Refreshing aggregate %s", agg)
		err := s.RefreshContinuousAggregate(agg)
		if err != nil {
			logger.Debug.Fatalf("failed to refresh continuous aggregate %s: %v", agg, err)
			return err
		}
	}
	logger.Info.Println("Finished refreshing aggregates!")
	return nil
}

// Concurrently make HTTP requests for given URLs (zip files), and stream them to the database
func processMonthlyData(
	symbol string,
	year, month int,
	storage storage.Storage,
	wg *sync.WaitGroup,
) {
	defer wg.Done()
	uri := rest.BuildURI(historicalData,
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
func stream(uri string) (*zip.Reader, error) {
	b, err := rest.Query("GET", uri, "application/json", nil)
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
func downloadZIP(uri string) error {
	// Send HTTP GET request to download the ZIP file
	resp, err := rest.Query("GET", uri, "application/json", nil)
	if err != nil {
		return err
	}

	// Create or open the destination file for writing
	dst := "./internal/tmp/" + filepath.Base(uri)
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

func ValidateSymbol(symbol string, symbolCache map[string]*models.SymbolFilter) error {
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

func inbound[T any](in *T) <-chan *T {
	out := make(chan *T)
	go func() {
		out <- in
		close(out)
	}()
	return out
}

func receiver[T ~string | ~[]byte](
	ctx context.Context,
	in chan T,
	conn *websocket.Conn,
) {
	// Process data received from the data channel
	for data := range in {
		// Write the data to the WebSocket connection
		if err := wsjson.Write(ctx, conn, string(data)); err != nil {
			logger.Error.Printf("Error writing data to WebSocket: %v\n", err)
			continue
		}
	}
}

func connectExchange(
	ctx context.Context,
	db storage.Storage,
	symbol, interval, strategy string,
) *Binance {
	cs := &models.CandleSubsciption{
		Symbol:   strings.ToLower(symbol),
		Interval: interval,
		Strategy: strategy,
	}
	return NewBinance(ctx, db, inbound(cs))
}

func getStrategy(strat string, db storage.Storage) (backtest.Strategy[any], error) {
	strategies := map[string]backtest.Strategy[any]{
		"sma":  strategy.NewSMAStrategy(12, 24, 5, db),
		"macd": strategy.NewMACDStrategy(5, db),
	}
	strategy, found := strategies[strat]
	if !found {
		return nil, fmt.Errorf("Error, startegy not found!")
	}
	return strategy, nil
}

func processBars(
	symbol, interval string,
	strategy backtest.Strategy[any],
	dc chan []byte,
) {
	// Prefetch data here for talib calculations
	bars, err := convertKlines(symbol, interval)
	strategy.SetData(bars)
	strategy.SetAsset(symbol)

	for data := range dc {
		// Unmarshal bar data
		var (
			bar = &models.KlineSimple{}
			kws = &models.KlineWebSocket{}
		)
		err = json.Unmarshal(data, &kws)
		if err != nil {
			logger.Error.Println(err)
			return
		}

		// Only evaluate closed bars
		if kws.Data.Kline.IsKlineClosed {
			// logger.Info.Println(kws)
			bar.OpenTime = time.UnixMilli(kws.Data.Kline.StartTime)
			bar.Open, _ = strconv.ParseFloat(kws.Data.Kline.OpenPrice, 64)
			bar.High, _ = strconv.ParseFloat(kws.Data.Kline.HighPrice, 64)
			bar.Low, _ = strconv.ParseFloat(kws.Data.Kline.LowPrice, 64)
			bar.Close, _ = strconv.ParseFloat(kws.Data.Kline.ClosePrice, 64)
			bars = append(bars, bar)

			strategy.SetData(bars)
			strategy.Execute()
		}
	}
}

func convertKlines(symbol, interval string) ([]*models.KlineSimple, error) {
	bars := []*models.KlineSimple{}

	// Prefetch data here for talib calculations?
	resp, err := rest.GetKlines(
		fmt.Sprintf("symbol=%s&interval=%s", strings.ToUpper(symbol), interval),
	)
	if err != nil {
		return nil, fmt.Errorf("error getting klines: %w", err)
	}

	var klines [][]interface{}
	err = json.Unmarshal(resp, &klines)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling klines: %w", err)
	}

	for _, k := range klines {
		open, _ := strconv.ParseFloat(k[1].(string), 64)
		high, _ := strconv.ParseFloat(k[2].(string), 64)
		low, _ := strconv.ParseFloat(k[3].(string), 64)
		cloze, _ := strconv.ParseFloat(k[4].(string), 64)
		volume, _ := strconv.ParseFloat(k[5].(string), 64)
		b := &models.KlineSimple{
			OpenTime: time.UnixMilli(int64(k[0].(float64))),
			Open:     open,
			High:     high,
			Low:      low,
			Close:    cloze,
			Volume:   volume,
		}
		bars = append(bars, b)
	}
	return bars, nil
}
