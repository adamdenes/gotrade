package storage

import (
	"archive/zip"
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/adamdenes/gotrade/internal/logger"
	"github.com/adamdenes/gotrade/internal/models"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/stdlib"
)

type TimescaleDB struct {
	db *sql.DB
}

func NewTimescaleDB(dsn string) (*TimescaleDB, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Fatal(err)
	}

	if err := db.Ping(); err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(25)
	db.SetConnMaxLifetime(5 * time.Minute)

	return &TimescaleDB{db: db}, nil
}

func (ts *TimescaleDB) Init() {
	password := os.Getenv("DB_PASSWORD")
	if password == "" {
		logger.Debug.Fatal("DB_PASSWORD environment variable is not set")
	}

	// Creating a role if it does not exist
	q := fmt.Sprintf(`
    DO
    $$
    BEGIN
        IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'web') THEN
            CREATE ROLE web WITH LOGIN PASSWORD '%s';
        END IF;
    END
    $$;`, password)

	_, err := ts.db.Exec(q)
	if err != nil {
		logger.Debug.Fatalf("failed to create database role: %v", err)
		return
	}

	q2 := "GRANT ALL PRIVILEGES ON DATABASE binance_db TO web;"
	_, err = ts.db.Exec(q2)
	if err != nil {
		logger.Debug.Fatalf("failed to set database privileges to role: %v", err)
		return
	}

	logger.Debug.Println("Database role and privileges set up successfully.")
}

func (ts *TimescaleDB) Close() {
	ts.db.Close()
}

func (ts *TimescaleDB) GetSymbol(symbol string) (int64, *models.SymbolFilter, error) {
	var (
		id         int64
		baseAsset  string
		quoteAsset string
	)

	// Adjusted query to retrieve base_asset and quote_asset as well
	q := `SELECT symbol_id, base_asset, quote_asset FROM binance.symbols WHERE symbol = $1;`
	err := ts.db.QueryRow(q, symbol).Scan(&id, &baseAsset, &quoteAsset)
	if err != nil {
		// Return nil for SymbolFilter if there's an error
		return 0, nil, err
	}

	symbolFilter := &models.SymbolFilter{
		Symbol:     symbol,
		BaseAsset:  baseAsset,
		QuoteAsset: quoteAsset,
	}

	return id, symbolFilter, nil
}

func (ts *TimescaleDB) GetSymbols() (*sql.Rows, error) {
	q := "SELECT symbol FROM binance.symbols"
	rows, err := ts.db.Query(q)
	return rows, err
}

func (ts *TimescaleDB) CreateSymbol(sf *models.SymbolFilter) (int64, error) {
	var id int64
	q := `INSERT INTO binance.symbols (symbol, base_asset, quote_asset) 
    VALUES ($1, $2, $3) ON CONFLICT (symbol) DO NOTHING
    RETURNING symbol_id;`

	err := ts.db.QueryRow(q, sf.Symbol, sf.BaseAsset, sf.QuoteAsset).Scan(&id)
	if err != nil {
		return 0, err
	}

	logger.Debug.Printf(
		"INSERT INTO binance.symbols-> (%s, %s, %s), returning symbol_id: %d",
		sf.Symbol,
		sf.BaseAsset,
		sf.QuoteAsset,
		id,
	)
	return id, nil
}

func (ts *TimescaleDB) GetInterval(interval string) (int64, error) {
	var id int64
	q := fmt.Sprintf("SELECT interval_id FROM binance.intervals WHERE interval = '%s';", interval)
	err := ts.db.QueryRow(q).Scan(&id)
	return id, err
}

func (ts *TimescaleDB) CreateInterval(interval, duration string) (int64, error) {
	var id int64
	query := `
    INSERT INTO binance.intervals (interval, interval_duration)
    VALUES ($1, $2) ON CONFLICT (interval, interval_duration) DO NOTHING
    RETURNING interval_id;`

	err := ts.db.QueryRow(query, interval, duration).Scan(&id)
	if err != nil {
		return 0, err
	}

	logger.Debug.Printf(
		"INSERT INTO binance.intervals -> (%s, %s), returning itnerval_id: %d",
		interval,
		duration,
		id,
	)
	return id, err
}

func (ts *TimescaleDB) CreateSymbolIntervalID(sid, iid int64) (int64, error) {
	var id int64
	query := `
    INSERT INTO binance.symbols_intervals (symbol_id, interval_id)
    VALUES ($1, $2) ON CONFLICT (symbol_id, interval_id) 
    DO UPDATE SET symbol_id=EXCLUDED.symbol_id 
    RETURNING symbol_interval_id;`

	err := ts.db.QueryRow(query, sid, iid).Scan(&id)
	if err != nil {
		return 0, err
	}

	logger.Debug.Printf(
		"INSERT INTO binance.symbols_intervals -> (%d, %d), returning symbol_interval_id: %d",
		sid,
		iid,
		id,
	)
	return id, err
}

func (ts *TimescaleDB) GetSymbolIntervalID(sid, iid int64) (int64, error) {
	var symbolIntervalID int64
	query := `SELECT symbol_interval_id FROM binance.symbols_intervals WHERE symbol_id = $1 AND interval_id = $2`
	err := ts.db.QueryRow(query, sid, iid).Scan(&symbolIntervalID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return -1, nil
		}
		return -1, err // Some other database error occurred
	}
	return symbolIntervalID, nil
}

func (ts *TimescaleDB) CreateSIID(s, i string) (int64, error) {
	symbolID, sf, err := ts.GetSymbol(s)
	if err != nil {
		// Shouldn't return any row on conflict!
		if errors.Is(err, sql.ErrNoRows) {
			sf = &models.SymbolFilter{
				Symbol: s,
			}
			symbolID, err = ts.CreateSymbol(sf)
			if err != nil {
				return -1, err
			}
		} else {
			return -1, fmt.Errorf("error ensuring symbol exists: %v", err)
		}
	}

	intervalID, err := ts.GetInterval(i)
	if err != nil {
		// Shouldn't return any row on conflict!
		if errors.Is(err, sql.ErrNoRows) {
			intervalID, err = ts.CreateInterval(i, ConvertInterval(i))
			if err != nil {
				return -1, err
			}
		} else {
			return -1, fmt.Errorf("error ensuring interval exists: %v", err)
		}
	}

	existingSymbolIntervalID, err := ts.GetSymbolIntervalID(symbolID, intervalID)
	if err != nil {
		return -1, fmt.Errorf("error checking symbol-interval combo: %v", err)
	}

	// SIID already exists, return it
	if existingSymbolIntervalID != -1 {
		return existingSymbolIntervalID, nil
	}

	// SIID does not exists, create one
	symbolIntervalID, err := ts.CreateSymbolIntervalID(symbolID, intervalID)
	if err != nil {
		return -1, fmt.Errorf("error ensuring symbol-interval combo exists: %v", err)
	}

	return symbolIntervalID, nil
}

func (ts *TimescaleDB) SaveFilters(symbolID int64, filters []models.Filter) error {
	for _, filter := range filters {
		if err := ts.CreateFilter(symbolID, filter); err != nil {
			return err
		}
	}
	return nil
}

func (ts *TimescaleDB) CreateFilter(symbolID int64, filter models.Filter) error {
	var (
		err error
		q   string
	)

	switch f := filter.(type) {
	case *models.PriceFilter:
		maxPrice, _ := strconv.ParseFloat(f.MaxPrice, 64)
		minPrice, _ := strconv.ParseFloat(f.MinPrice, 64)
		tickSize, _ := strconv.ParseFloat(f.TickSize, 64)
		q = `INSERT INTO binance.price_filters (symbol_id, max_price, min_price, tick_size) 
              VALUES ($1, $2, $3, $4);`
		_, err = ts.db.Exec(q, symbolID, maxPrice, minPrice, tickSize)

	case *models.LotSizeFilter:
		maxQty, _ := strconv.ParseFloat(f.MaxQty, 64)
		minQty, _ := strconv.ParseFloat(f.MinQty, 64)
		stepSize, _ := strconv.ParseFloat(f.StepSize, 64)
		q = `INSERT INTO binance.lot_size_filters (symbol_id, max_qty, min_qty, step_size) 
              VALUES ($1, $2, $3, $4);`
		_, err = ts.db.Exec(q, symbolID, maxQty, minQty, stepSize)

	case *models.IcebergPartsFilter:
		q = `INSERT INTO binance.iceberg_parts_filters (symbol_id, parts_limit) 
              VALUES ($1, $2);`
		_, err = ts.db.Exec(q, symbolID, f.Limit)

	case *models.MarketLotSizeFilter:
		maxQty, _ := strconv.ParseFloat(f.MaxQty, 64)
		minQty, _ := strconv.ParseFloat(f.MinQty, 64)
		stepSize, _ := strconv.ParseFloat(f.StepSize, 64)
		q = `INSERT INTO binance.market_lot_size_filters (symbol_id, max_qty, min_qty, step_size) 
              VALUES ($1, $2, $3, $4);`
		_, err = ts.db.Exec(q, symbolID, maxQty, minQty, stepSize)

	case *models.TrailingDeltaFilter:
		q = `INSERT INTO binance.trailing_delta_filters (symbol_id, max_trailing_above_delta, max_trailing_below_delta, min_trailing_above_delta, min_trailing_below_delta) 
              VALUES ($1, $2, $3, $4, $5);`
		_, err = ts.db.Exec(q, symbolID, f.MaxTrailingAboveDelta, f.MaxTrailingBelowDelta, f.MinTrailingAboveDelta, f.MinTrailingBelowDelta)

	case *models.PercentPriceBySideFilter:
		askMultiplierDown, _ := strconv.ParseFloat(f.AskMultiplierDown, 64)
		askMultiplierUp, _ := strconv.ParseFloat(f.AskMultiplierUp, 64)
		bidMultiplierDown, _ := strconv.ParseFloat(f.BidMultiplierDown, 64)
		bidMultiplierUp, _ := strconv.ParseFloat(f.BidMultiplierUp, 64)
		q = `INSERT INTO binance.percent_price_by_side_filters (symbol_id, ask_multiplier_down, ask_multiplier_up, avg_price_mins, bid_multiplier_down, bid_multiplier_up) 
              VALUES ($1, $2, $3, $4, $5, $6);`
		_, err = ts.db.Exec(q, symbolID, askMultiplierDown, askMultiplierUp, f.AvgPriceMins, bidMultiplierDown, bidMultiplierUp)

	case *models.NotionalFilter:
		maxNotional, _ := strconv.ParseFloat(f.MaxNotional, 64)
		minNotional, _ := strconv.ParseFloat(f.MinNotional, 64)
		q = `INSERT INTO binance.notional_filters (symbol_id, apply_max_to_market, apply_min_to_market, max_notional, min_notional) 
              VALUES ($1, $2, $3, $4, $5);`
		_, err = ts.db.Exec(q, symbolID, f.ApplyMaxToMarket, f.ApplyMinToMarket, maxNotional, minNotional)

	case *models.MaxNumOrdersFilter:
		q = `INSERT INTO binance.max_num_orders_filters (symbol_id, max_num_orders) 
              VALUES ($1, $2);`
		_, err = ts.db.Exec(q, symbolID, f.MaxNumOrders)

	case *models.MaxNumAlgoOrdersFilter:
		q = `INSERT INTO binance.max_num_algo_orders_filters (symbol_id, max_num_algo_orders) 
              VALUES ($1, $2);`
		_, err = ts.db.Exec(q, symbolID, f.MaxNumAlgoOrders)

	default:
		return fmt.Errorf("unknown filter type")
	}

	if err != nil {
		return err
	}

	return nil
}

func ConvertInterval(intervalString string) string {
	const (
		SECOND = "s"
		MINUTE = "m"
		HOUR   = "h"
		DAY    = "d"
		WEEK   = "w"
		MONTH  = "M"
	)
	var time, kind string

	r := []rune(intervalString)
	for _, v := range r {
		if unicode.IsDigit(v) {
			time += string(v)
		}
		if unicode.IsLetter(v) {
			kind += string(v)
		}
	}
	var result string
	switch kind {
	case SECOND:
		result = time + " second"
	case MINUTE:
		result = time + " minute"
	case HOUR:
		result = time + " hour"
	case DAY:
		result = time + " day"
	case WEEK:
		result = time + " week"
	case MONTH:
		result = time + " month"
	}

	if time != "1" || len(time) >= 2 {
		result += "s"
	}

	return result
}

type chunk struct {
	chunkSchema  string
	chunkName    string
	isCompressed bool
}

// Insert data into a compressed chunk
func (ts *TimescaleDB) getChunk(htable string, timestamp time.Time) (*chunk, error) {
	chunk := &chunk{}

	q := `SELECT chunk_schema, chunk_name, is_compressed FROM timescaledb_information.chunks c
          WHERE c.hypertable_name = $1 AND $2 BETWEEN c.range_start AND c.range_end;`
	err := ts.db.QueryRow(q, htable, timestamp).
		Scan(&chunk.chunkSchema, &chunk.chunkName, &chunk.isCompressed)
	if err != nil {
		return chunk, err
	}
	return chunk, nil
}

func (ts *TimescaleDB) decompressChunk(ch *chunk) error {
	if !ch.isCompressed {
		logger.Debug.Printf("Chunk is not compressed (%t), return.", ch.isCompressed)
		return nil
	}
	// true flag means "skip if not compressed"
	dcq := fmt.Sprintf("SELECT decompress_chunk('%s.%s', true);", ch.chunkSchema, ch.chunkName)
	logger.Debug.Printf(
		"executing -> SELECT decompress_chunk('%s.%s', %t);",
		ch.chunkSchema,
		ch.chunkName,
		ch.isCompressed,
	)
	_, err := ts.db.Exec(dcq)
	return err
}

// -------------------- Storage Interface --------------------

func (ts *TimescaleDB) Create(k *models.Kline) error {
	symbolIntervalID, err := ts.CreateSIID(k.Symbol, k.Interval)
	if err != nil {
		return fmt.Errorf("error creating symbolIntervalID: %v", err)
	}

	query := `
        INSERT INTO binance.kline (
            symbol_interval_id,
            open_time,
            open,
            high,
            low,
            close,
            volume,
            close_time,
            quote_volume,
            count,
            taker_buy_volume,
            taker_buy_quote_volume
        )
        VALUES (
            $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
        )`

	_, err = ts.db.Exec(
		query,
		symbolIntervalID,
		k.OpenTime,
		k.Open,
		k.High,
		k.Low,
		k.Close,
		k.Volume,
		k.CloseTime,
		k.QuoteAssetVolume,
		k.NumberOfTrades,
		k.TakerBuyBaseAssetVol,
		k.TakerBuyQuoteAssetVol,
	)
	if err != nil {
		return err
	}

	return nil
}

func (ts *TimescaleDB) Delete(int) error                               { return nil }
func (ts *TimescaleDB) Update(*models.Kline) error                     { return nil }
func (ts *TimescaleDB) GetCandleByOpenTime(int) (*models.Kline, error) { return nil, nil }

func (ts *TimescaleDB) FetchData(
	ctx context.Context,
	aggView, symbol string,
	startTime, endTime int64,
) ([]*models.KlineSimple, error) {
	start := time.Now()

	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	query := fmt.Sprintf(`
    SELECT 
        k.bucket, 
        k.first_open, 
        k.max_high, 
        k.min_low, 
        k.last_close
    FROM binance."aggregate_%s" k
    JOIN binance.symbols_intervals si ON k.siid = si.symbol_interval_id
    JOIN binance.symbols s ON si.symbol_id = s.symbol_id
    WHERE s.symbol = $1 AND k.bucket >= $2 AND k.bucket <= $3
    ORDER BY k.bucket;`,
		aggView)

	rows, err := ts.db.QueryContext(
		ctx,
		query,
		symbol,
		time.Unix(startTime/1000, 0),
		time.Unix(endTime/1000, 0),
	)
	if err != nil {
		if err.Error() == "pq: canceling statement due to user request" {
			return nil, fmt.Errorf("%w: %v", ctx.Err(), err)
		}
		return nil, err
	}
	defer rows.Close()

	var klines []*models.KlineSimple
	for rows.Next() {
		var d models.KlineSimple
		if err := rows.Scan(&d.OpenTime, &d.Open, &d.High, &d.Low, &d.Close); err != nil {
			return nil, err
		}
		klines = append(klines, &d)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	logger.Info.Printf("Finished streaming data to client, it took %v\n", time.Since(start))
	return klines, nil
}

func (ts *TimescaleDB) Copy(r []byte, name *string, interval *string) error {
	startTime := time.Now()

	ctx := context.Background()
	conn, err := ts.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	var rawData [][]interface{}
	if err := json.Unmarshal(r, &rawData); err != nil {
		return err
	}

	query := `
	    INSERT INTO binance.kline (
	           symbol_interval_id,
	           open_time,
	           open,
	           high,
	           low,
	           close,
	           volume,
	           close_time,
	           quote_volume,
	           count,
	           taker_buy_volume,
	           taker_buy_quote_volume
	    )
	    VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
        ON CONFLICT (symbol_interval_id, open_time) DO NOTHING;`

	err = conn.Raw(func(driverConn any) error {
		conn := driverConn.(*stdlib.Conn).Conn()
		defer conn.Close(ctx)

		_, err := conn.Prepare(ctx, "create_stmt", query)
		if err != nil {
			return err
		}

		symbolIntervalID, err := ts.CreateSIID(*name, *interval)
		if err != nil {
			return fmt.Errorf("error creating symbol_interval_id: %v", err)
		}

		processedChunks := make(map[string]bool)
		batch := pgx.Batch{}
		for _, record := range rawData {
			openTime := time.UnixMilli(int64(record[0].(float64)))
			// check if chunk is compressed to avoid error
			// if chunk is compressed, decompress first

			chunk, err := ts.getChunk("kline", openTime)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}

			if chunk != nil {
				chunkKey := fmt.Sprintf("%s.%s", chunk.chunkSchema, chunk.chunkName)
				if _, ok := processedChunks[chunkKey]; !ok {
					// should skip uncompressed chunks
					if err := ts.decompressChunk(chunk); err != nil {
						return err
					}
					processedChunks[chunkKey] = true
				}
			}

			batch.Queue(query,
				symbolIntervalID,
				openTime,           // openTime
				record[1].(string), // open
				record[2].(string), // high
				record[3].(string), // low
				record[4].(string), // close
				record[5].(string), // volume
				time.UnixMilli(int64(record[6].(float64))), // closeTime
				record[7].(string),                         // quoteVolume
				int(record[8].(float64)),                   // count
				record[9].(string),                         // takerBuyVolume
				record[10].(string),                        // takerBuyQuoteVolume
			)
		}

		// Send queued up queries
		br := conn.SendBatch(ctx, &batch)
		defer br.Close()

		_, err = br.Exec()
		if err != nil {
			return err
		}
		logger.Debug.Println("Total rows:", len(rawData))

		return nil
	})
	if err != nil {
		return err
	}

	logger.Info.Printf(
		"Finished inserting batch data to Postgres, it took %v\n",
		time.Since(startTime),
	)
	return nil
}

func (ts *TimescaleDB) Stream(r *zip.Reader) error {
	startTime := time.Now()

	ctx := context.Background()
	conn, err := ts.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	zipSlice := strings.Split(r.File[0].Name, "-") // get name and interval
	zippedFile, err := r.File[0].Open()
	if err != nil {
		return fmt.Errorf("error opening ZIP entry: %v", err)
	}
	defer zippedFile.Close()

	// Create a CSV reader
	csvReader := csv.NewReader(zippedFile)

	err = conn.Raw(func(driverConn interface{}) error {
		conn := driverConn.(*stdlib.Conn).Conn()

		symbolIntervalID, err := ts.CreateSIID(zipSlice[0], zipSlice[1])
		if err != nil {
			return fmt.Errorf("error ensuring symbol-interval combo exists: %v", err)
		}

		// Create the csvCopySource
		cs := &csvCopySource{
			symbolIntervalID: symbolIntervalID,
			reader:           csvReader,
		}

		ar, err := conn.CopyFrom(ctx,
			pgx.Identifier{"binance", "kline"},
			[]string{
				"symbol_interval_id",
				"open_time",
				"open",
				"high",
				"low",
				"close",
				"volume",
				"close_time",
				"quote_volume",
				"count",
				"taker_buy_volume",
				"taker_buy_quote_volume",
			},
			cs,
		)
		logger.Debug.Println("Affected rows:", ar)
		if err != nil {
			return fmt.Errorf("error CopyFrom: %v", err)
		}

		return nil
	})
	if err != nil {
		return err
	}

	logger.Info.Printf("Finished streaming data to Postgres, it took %v\n", time.Since(startTime))
	return nil
}

func (ts *TimescaleDB) QueryLastRow() (*models.KlineRequest, error) {
	query := `
    SELECT 
        s.symbol, 
        i.interval, 
        k.open_time, 
        k.close_time
    FROM 
        binance.kline AS k
    JOIN 
        binance.symbols_intervals AS si ON k.symbol_interval_id = si.symbol_interval_id
    JOIN
        binance.symbols AS s ON si.symbol_id = s.symbol_id
    JOIN
        binance.intervals AS i ON si.interval_id = i.interval_id
    ORDER BY 
        k.open_time DESC
    LIMIT 1;`

	kr := new(models.KlineRequest)

	err := ts.db.QueryRow(query).
		Scan(&kr.Symbol, &kr.Interval, &kr.OpenTime, &kr.CloseTime)
	if err != nil {
		return nil, err
	}

	return kr, nil
}

func (ts *TimescaleDB) SaveSymbols(sf map[string]*models.SymbolFilter) error {
	for _, filterData := range sf {
		symbolID, err := ts.CreateSymbol(filterData)
		if err != nil {
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}

		if err := ts.SaveFilters(symbolID, filterData.Filters); err != nil {
			return err
		}
	}
	return nil
}

func (ts *TimescaleDB) SaveTrade(t *models.Trade) error {
	stmt := `INSERT INTO binance.trades (
        strategy,
        status,
        symbol, 
        order_id, 
        order_list_id, 
        price, 
        qty, 
        quote_qty, 
        commission, 
        commission_asset, 
        trade_time, 
        is_buyer, 
        is_maker, 
        is_best_match
    ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`

	// Convert string fields to their appropriate types
	price, err := strconv.ParseFloat(t.Price, 64)
	if err != nil {
		price = 0
	}
	qty, err := strconv.ParseFloat(t.Qty, 64)
	if err != nil {
		qty = 0
	}
	quoteQty, err := strconv.ParseFloat(t.QuoteQty, 64)
	if err != nil {
		quoteQty = 0
	}
	commission, err := strconv.ParseFloat(t.Commission, 64)
	if err != nil {
		commission = 0
	}

	_, err = ts.db.Exec(stmt,
		t.Strategy,
		t.Status,
		t.Symbol,
		t.OrderID,
		t.OrderListID,
		price,
		qty,
		quoteQty,
		commission,
		t.CommissionAsset,
		t.Time,
		t.IsBuyer,
		t.IsMaker,
		t.IsBestMatch,
	)
	if err != nil {
		return fmt.Errorf("error saving trade to database: %w", err)
	}

	return nil
}

func (ts *TimescaleDB) GetTrade(id int64) (*models.Trade, error) {
	trade := &models.Trade{}
	q := "SELECT symbol, price, trade_time FROM binance.trades WHERE id = $1"

	err := ts.db.QueryRow(q, id).Scan(&trade.Symbol, &trade.Price, &trade.Time)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("no trade found with id %d", id)
		}
		return nil, fmt.Errorf("error getting trade from database: %w", err)
	}

	return trade, nil
}

func (ts *TimescaleDB) UpdateTrade(id int64, status string) error {
	var q string
	if status == "FILLED" {
		q = "UPDATE binance.trades SET status = $1 WHERE order_id = $2"
	} else {
		q = "UPDATE binance.trades SET status = $1 WHERE order_list_id = $2"
	}

	_, err := ts.db.Exec(q, status, id)
	if err != nil {
		return fmt.Errorf("error updating trade with id %d: %w", id, err)
	}

	return nil
}

func (ts *TimescaleDB) FetchTrades() ([]*models.Trade, error) {
	var trades []*models.Trade
	q := `SELECT strategy, status, symbol, order_id, order_list_id, price, qty, 
          quote_qty, commission, commission_asset, trade_time, is_buyer, 
          is_maker, is_best_match FROM binance.trades`

	rows, err := ts.db.Query(q)
	if err != nil {
		return nil, fmt.Errorf("error fetching trades from database: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var t models.Trade

		if err := rows.Scan(
			&t.Strategy,
			&t.Status,
			&t.Symbol,
			&t.OrderID,
			&t.OrderListID,
			&t.Price,
			&t.Qty,
			&t.QuoteQty,
			&t.Commission,
			&t.CommissionAsset,
			&t.Time,
			&t.IsBuyer,
			&t.IsMaker,
			&t.IsBestMatch,
		); err != nil {
			return nil, fmt.Errorf("error scanning trade from database: %w", err)
		}
		trades = append(trades, &t)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating trades from database: %w", err)
	}

	return trades, nil
}

func (ts *TimescaleDB) SaveOrder(strategy string, order *models.PostOrderResponse) error {
	q := `
		INSERT INTO binance.orders (
			symbol,
            strategy,
			order_id,
            order_list_id,
			client_order_id,
			price,
			orig_qty,
			executed_qty,
			cummulative_quote_qty,
			status,
			time_in_force,
			type,
			side,
			stop_price,
            iceberg_qty,
            time,
            update_time,
            is_working,
			working_time,
            orig_quote_order_qty,
			self_trade_prevention_mode
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21
        );
	`

	stmt, err := ts.db.Prepare(q)
	if err != nil {
		return err
	}
	defer stmt.Close()

	// Perform type conversions where needed
	var price float64
	if order.Price != "" {
		price, err = strconv.ParseFloat(order.Price, 64)
		if err != nil {
			return err
		}
	}

	var origQty float64
	if order.OrigQty != "" {
		origQty, err = strconv.ParseFloat(order.OrigQty, 64)
		if err != nil {
			return err
		}
	}

	var executedQty float64
	if order.ExecutedQty != "" {
		executedQty, err = strconv.ParseFloat(order.ExecutedQty, 64)
		if err != nil {
			return err
		}
	}

	var cummulativeQuoteQty float64
	if order.CummulativeQuoteQty != "" {
		cummulativeQuoteQty, err = strconv.ParseFloat(order.CummulativeQuoteQty, 64)
		if err != nil {
			return err
		}
	}

	var stopPrice float64
	if order.StopPrice != "" {
		stopPrice, err = strconv.ParseFloat(order.StopPrice, 64)
		if err != nil {
			return err
		}
	}

	_, err = stmt.Exec(
		order.Symbol,
		strategy,
		order.OrderID,
		order.OrderListID,
		order.ClientOrderID,
		price,
		origQty,
		executedQty,
		cummulativeQuoteQty,
		order.Status,
		order.TimeInForce,
		order.Type,
		order.Side,
		stopPrice,
		0.0, // iceberg_qty
		time.UnixMilli(order.TransactTime),
		time.UnixMilli(order.TransactTime),
		true, // is_working
		time.UnixMilli(order.TransactTime),
		0.0, // orig_quote_order_qty
		order.SelfTradePreventionMode,
	)

	if err != nil {
		return err
	}

	return nil
}

func (ts *TimescaleDB) FetchOrders() ([]*models.GetOrderResponse, error) {
	var orders []*models.GetOrderResponse

	query := `
		SELECT
			symbol,
			order_id,
            order_list_id,
			client_order_id,
			price,
			orig_qty,
			executed_qty,
			cummulative_quote_qty,
			status,
			time_in_force,
			type,
			side,
			stop_price,
            iceberg_qty,
            time,
            update_time,
            is_working,
			working_time,
            orig_quote_order_qty,
			self_trade_prevention_mode
		FROM
			binance.orders;
	`

	rows, err := ts.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			order       models.GetOrderResponse
			ttime       time.Time
			updateTime  time.Time
			workingTime time.Time
		)
		err := rows.Scan(
			&order.Symbol,
			&order.OrderID,
			&order.OrderListID,
			&order.ClientOrderID,
			&order.Price,
			&order.OrigQty,
			&order.ExecutedQty,
			&order.CummulativeQuoteQty,
			&order.Status,
			&order.TimeInForce,
			&order.Type,
			&order.Side,
			&order.StopPrice,
			&order.IcebergQty,
			&ttime,
			&updateTime,
			&order.IsWorking,
			&workingTime,
			&order.OrigQuoteOrderQty,
			&order.SelfTradePreventionMode,
		)
		if err != nil {
			return nil, err
		}

		order.Time = ttime.UnixMilli()
		order.UpdateTime = updateTime.UnixMilli()
		order.WorkingTime = workingTime.UnixMilli()
		orders = append(orders, &order)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return orders, nil
}

func (ts *TimescaleDB) UpdateOrder(order *models.GetOrderResponse) error {
	q := `
		UPDATE binance.orders
		SET
			symbol = $1,
			order_list_id = $2,
			client_order_id= $3,
			price = $4,
			orig_qty = $5,
			executed_qty = $6,
			cummulative_quote_qty = $7,
			status = $8,
			time_in_force = $9,
			type = $10,
			side = $11,
			stop_price = $12,
            iceberg_qty = $13,
            time = $14,
            update_time = $15,
            is_working = $16,
			working_time = $17,
            orig_quote_order_qty = $18,
			self_trade_prevention_mode = $19
		WHERE
			order_id = $20;
	`

	stmt, err := ts.db.Prepare(q)
	if err != nil {
		return err
	}
	defer stmt.Close()

	// Perform type conversions where needed
	var price float64
	if order.Price != "" {
		price, err = strconv.ParseFloat(order.Price, 64)
		if err != nil {
			return err
		}
	}

	var origQty float64
	if order.OrigQty != "" {
		origQty, err = strconv.ParseFloat(order.OrigQty, 64)
		if err != nil {
			return err
		}
	}

	var executedQty float64
	if order.ExecutedQty != "" {
		executedQty, err = strconv.ParseFloat(order.ExecutedQty, 64)
		if err != nil {
			return err
		}
	}

	var cummulativeQuoteQty float64
	if order.CummulativeQuoteQty != "" {
		cummulativeQuoteQty, err = strconv.ParseFloat(order.CummulativeQuoteQty, 64)
		if err != nil {
			return err
		}
	}

	var stopPrice float64
	if order.StopPrice != "" {
		stopPrice, err = strconv.ParseFloat(order.StopPrice, 64)
		if err != nil {
			return err
		}
	}

	var icebergQty float64
	if order.IcebergQty != "" {
		icebergQty, err = strconv.ParseFloat(order.IcebergQty, 64)
		if err != nil {
			return err
		}
	}

	var origQuoteOrderQty float64
	if order.OrigQuoteOrderQty != "" {
		origQuoteOrderQty, err = strconv.ParseFloat(order.OrigQuoteOrderQty, 64)
		if err != nil {
			return err
		}
	}

	_, err = stmt.Exec(
		order.Symbol,
		order.OrderListID,
		order.ClientOrderID,
		price,
		origQty,
		executedQty,
		cummulativeQuoteQty,
		order.Status,
		order.TimeInForce,
		order.Type,
		order.Side,
		stopPrice,
		icebergQty,
		time.UnixMilli(order.Time),
		time.UnixMilli(order.UpdateTime),
		order.IsWorking,
		time.UnixMilli(order.WorkingTime),
		origQuoteOrderQty,
		order.SelfTradePreventionMode,
		order.OrderID,
	)

	if err != nil {
		return err
	}

	return nil
}

func (ts *TimescaleDB) CreateBot(b *models.TradingBot) error {
	// Check if a bot with the same parameters is already running
	var count int
	err := ts.db.QueryRow("SELECT COUNT(*) FROM binance.bots WHERE symbol = $1 AND strategy = $2",
		b.Symbol, b.Strategy).
		Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		return errors.New("a bot with the same parameters is already running")
	}

	// Add the new bot to the database
	_, err = ts.db.Exec(
		"INSERT INTO binance.bots (symbol, interval, strategy, status, created_at) VALUES ($1, $2, $3, $4, $5)",
		b.Symbol,
		b.Interval,
		b.Strategy,
		b.Status,
		b.CreatedAt,
	)
	return err
}

func (ts *TimescaleDB) DeleteBot(id int) error {
	_, err := ts.db.Exec("DELETE FROM binance.bots WHERE id = $1;", id)
	return err
}

func (ts *TimescaleDB) GetBot(symbol, strategy string) (*models.TradingBot, error) {
	query := `SELECT * FROM binance.bots WHERE symbol = $1 AND strategy = $2 LIMIT 1;`

	// Variable to hold the result
	var bot models.TradingBot

	row := ts.db.QueryRow(query, symbol, strategy)
	err := row.Scan(
		&bot.ID,
		&bot.Symbol,
		&bot.Interval,
		&bot.Strategy,
		&bot.Status,
		&bot.CreatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			// No result found
			return nil, nil
		}
		// Some other error occurred
		return nil, err
	}

	return &bot, nil
}

func (ts *TimescaleDB) GetBots() ([]*models.TradingBot, error) {
	rows, err := ts.db.Query(
		"SELECT id, symbol, interval, strategy, status, created_at FROM binance.bots",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bots []*models.TradingBot
	for rows.Next() {
		var b models.TradingBot
		if err := rows.Scan(&b.ID, &b.Symbol, &b.Interval, &b.Strategy, &b.Status, &b.CreatedAt); err != nil {
			return nil, err
		}
		bots = append(bots, &b)
	}
	return bots, nil
}

func (ts *TimescaleDB) RefreshContinuousAggregate(agg string) error {
	q := fmt.Sprintf(
		"CALL refresh_continuous_aggregate('%s', now()::timestamp - INTERVAL '1 year', now()::timestamp);",
		agg,
	)
	_, err := ts.db.Exec(q)
	if err != nil {
		logger.Error.Println(err)
		return err
	}

	logger.Debug.Println(q)
	return nil
}

// Using CopyFromSource interface
type csvCopySource struct {
	symbolIntervalID int64
	reader           *csv.Reader
	lastError        error
	lastRecord       []string
	rows             int64
}

func (cs *csvCopySource) Next() bool {
	record, err := cs.reader.Read()
	if err != nil {
		cs.lastError = err
		return false
	}
	cs.lastRecord = record
	cs.rows++
	return true
}

func (cs *csvCopySource) Values() ([]interface{}, error) {
	record := cs.lastRecord
	if record == nil {
		return nil, errors.New("no current record")
	}

	var (
		symbolIntervalID    = pgtype.Int8{}
		openTime            = pgtype.Timestamptz{}
		closeTime           = pgtype.Timestamptz{}
		open                = pgtype.Float8{}
		high                = pgtype.Float8{}
		low                 = pgtype.Float8{}
		cloze               = pgtype.Float8{}
		volume              = pgtype.Float8{}
		quoteVolume         = pgtype.Float8{}
		takerBuyVolume      = pgtype.Float8{}
		takerBuyQuoteVolume = pgtype.Float8{}
		count               = pgtype.Int4{}
	)
	ot, err := strconv.ParseInt(record[0], 10, 64)
	if err != nil {
		return nil, err
	}
	ct, err := strconv.ParseInt(record[6], 10, 64)
	if err != nil {
		return nil, err
	}

	if err := symbolIntervalID.Scan(cs.symbolIntervalID); err != nil {
		return nil, err
	}

	if err := openTime.Scan(time.UnixMilli(ot)); err != nil {
		return nil, err
	}

	if err := open.Scan(record[1]); err != nil {
		return nil, err
	}

	if err := high.Scan(record[2]); err != nil {
		return nil, err
	}

	if err := low.Scan(record[3]); err != nil {
		return nil, err
	}

	if err := cloze.Scan(record[4]); err != nil {
		return nil, err
	}

	if err := volume.Scan(record[5]); err != nil {
		return nil, err
	}

	if err := closeTime.Scan(time.UnixMilli(ct)); err != nil {
		return nil, err
	}

	if err := quoteVolume.Scan(record[7]); err != nil {
		return nil, err
	}

	if err := count.Scan(record[8]); err != nil {
		return nil, err
	}

	if err := takerBuyVolume.Scan(record[9]); err != nil {
		return nil, err
	}

	if err := takerBuyQuoteVolume.Scan(record[10]); err != nil {
		return nil, err
	}

	row := []interface{}{
		symbolIntervalID,
		openTime,
		open,
		high,
		low,
		cloze,
		volume,
		closeTime,
		quoteVolume,
		count,
		takerBuyVolume,
		takerBuyQuoteVolume,
	}

	return row, nil
}

func (cs *csvCopySource) Err() error {
	return cs.lastError
}
