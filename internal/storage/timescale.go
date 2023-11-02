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

	return &TimescaleDB{db: db}, nil
}

func (ts *TimescaleDB) Close() {
	ts.db.Close()
}

func (ts *TimescaleDB) GetSymbol(symbol string) (int64, error) {
	var id int64
	q := fmt.Sprintf("SELECT symbol_id FROM binance.symbols WHERE symbol = '%s';", symbol)
	err := ts.db.QueryRow(q).Scan(&id)
	return id, err
}

func (ts *TimescaleDB) GetSymbols() (*sql.Rows, error) {
	q := "SELECT symbol FROM binance.symbols"
	rows, err := ts.db.Query(q)
	return rows, err
}

func (ts *TimescaleDB) CreateSymbol(symbol string) (int64, error) {
	var id int64
	q := `INSERT INTO binance.symbols (symbol) 
    VALUES ($1) ON CONFLICT (symbol) DO NOTHING
    RETURNING symbol_id;`

	err := ts.db.QueryRow(q, symbol).Scan(&id)
	if err != nil {
		return 0, err
	}

	logger.Debug.Printf("INSERT INTO binance.symbols-> (%s), returning symbol_id: %d", symbol, id)
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
	symbolID, err := ts.GetSymbol(s)
	if err != nil {
		// Shouldn't return any row on conflict!
		if errors.Is(err, sql.ErrNoRows) {
			symbolID, err = ts.CreateSymbol(s)
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

		_, err = br.Exec()
		if err != nil {
			return err
		}
		logger.Debug.Println("Total rows:", len(rawData))

		if err := conn.Close(ctx); err != nil {
			return err
		}

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

func (ts *TimescaleDB) SaveSymbols(sc map[string]struct{}) error {
	for symbol := range sc {
		if _, err := ts.CreateSymbol(symbol); err != nil {
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
	}
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
