package storage

import (
	"archive/zip"
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/adamdenes/gotrade/internal/logger"
	"github.com/adamdenes/gotrade/internal/models"
	"github.com/jackc/pgtype"
	"github.com/jackc/pgx/v5"
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

func (ts *TimescaleDB) Init() error {
	if err := ts.CreateSchema(); err != nil {
		return err
	}
	if err := ts.CreateCandleTable(); err != nil {
		return err
	}

	// ERROR: operation not supported on hypertables that have compression enabled (SQLSTATE 0A000)
	// if err := ts.CreateUniqueIndex(); err != nil {
	// 	return err
	// }

	viewsAndPolicies := map[string]map[string][]string{
		"1m": {
			"1 minute": {"1 day", "1 minute", "5 minutes"},
		},
		"5m": {
			"5 minutes": {"1 day", "5 minutes", "10 minutes"},
		},
		"1h": {
			"1 hour": {"1 month", "1 hour", "1 day"},
		},
		"1d": {
			"1 day": {"1 year", "1 day", "1 week"},
		},
	}
	for table, interval := range viewsAndPolicies {
		for innerTable, values := range interval {
			if err := ts.CreateMaterializedView(table, innerTable); err != nil {
				return err
			}
			if err := ts.CreateRefreshPolicy(table, values[0], values[1], values[2]); err != nil {
				return err
			}
		}
	}

	return nil
}

func (ts *TimescaleDB) CreateSchema() error {
	_, err := ts.db.Exec("CREATE SCHEMA IF NOT EXISTS binance")
	if err != nil {
		return err
	}
	return nil
}

func (ts *TimescaleDB) CreateUniqueIndex() error {
	indexQuery := `CREATE UNIQUE INDEX IF NOT EXISTS idx_unique_opentime_symbol_interval ON binance.kline (open_time, symbol_interval_id);`
	if _, err := ts.db.Exec(indexQuery); err != nil {
		return err
	}
	return nil
}

func (ts *TimescaleDB) CreateCandleTable() error {
	// Create Postgres SQL tables
	typeQuery := `CREATE TABLE IF NOT EXISTS binance.symbol_intervals (
        symbol_interval_id SERIAL PRIMARY KEY,
        symbol TEXT NOT NULL,
        interval TEXT NOT NULL,
        UNIQUE (symbol, interval)
    );`

	tableQuery := `CREATE TABLE IF NOT EXISTS binance.kline (
        symbol_interval_id INT REFERENCES binance.symbol_intervals(symbol_interval_id),
        open_time TIMESTAMPTZ NOT NULL,
        open FLOAT NOT NULL,
        high FLOAT NOT NULL,
        low FLOAT NOT NULL,
        close FLOAT NOT NULL,
        volume FLOAT NOT NULL,
        close_time TIMESTAMPTZ NOT NULL,
        quote_volume FLOAT NOT NULL,
        count INT NOT NULL,
        taker_buy_volume FLOAT NOT NULL,
        taker_buy_quote_volume FLOAT NOT NULL
    );`
	// Check if hypertable exist, if not create it
	hypertableQuery := `
    DO $$ 
    BEGIN 
        IF NOT EXISTS (SELECT * FROM timescaledb_information.hypertables WHERE hypertable_schema = 'binance' AND hypertable_name = 'kline') THEN 
            SELECT create_hypertable('binance.kline', 'open_time');
        END IF;
    END $$;`

	if _, err := ts.db.Exec(typeQuery); err != nil {
		return err
	}
	if _, err := ts.db.Exec(tableQuery); err != nil {
		return err
	}
	if _, err := ts.db.Exec(hypertableQuery); err != nil {
		return err
	}

	return nil
}

func (ts *TimescaleDB) CreateMaterializedView(iName string, interval string) error {
	query := fmt.Sprintf(`
    CREATE MATERIALIZED VIEW IF NOT EXISTS binance.aggregate_%s WITH (timescaledb.continuous) AS
    SELECT 
        si.symbol,
        si.interval,
        time_bucket(INTERVAL '%s', open_time) as bucket,
        AVG(close) as avg_close,
        MAX(high) as max_high,
        MIN(low) as min_low,
        FIRST(open, open_time) as first_open,
        LAST(close, close_time) as last_close,
        SUM(volume) as total_volume
    FROM binance.kline AS kd
    JOIN binance.symbol_intervals AS si ON kd.symbol_interval_id = si.symbol_interval_id
    GROUP BY bucket, si.symbol, si.interval;`, iName, interval)

	_, err := ts.db.Exec(query)
	return err
}

func (ts *TimescaleDB) CreateRefreshPolicy(
	aggTable string,
	startOffset, endOffset, scheduleInterval string,
) error {
	// Add only if not already created...
	p := fmt.Sprintf(`
    DO $$
    BEGIN
        IF NOT EXISTS (
            SELECT 1
            FROM timescaledb_information.jobs j
            JOIN timescaledb_information.continuous_aggregates c ON j.hypertable_name = c.materialization_hypertable_name
            WHERE c.view_name = 'aggregate_%s' AND c.view_schema = 'binance'
        ) THEN
            SELECT add_continuous_aggregate_policy(
                'binance."aggregate_%s"',
                start_offset => INTERVAL '%s',
                end_offset => INTERVAL '%s',
                schedule_interval => INTERVAL '%s'
            );
        END IF;
    END $$;`,
		aggTable, aggTable, startOffset, endOffset, scheduleInterval)

	_, err := ts.db.Exec(p)
	return err
}

// SetAutovacuumParams sets custom autovacuum parameters for the kline table
func (ts *TimescaleDB) SetAutovacuumParams(scaleFactor float64, costLimit int) error {
	query := fmt.Sprintf(
		`ALTER TABLE binance.kline SET (autovacuum_vacuum_scale_factor = %f, autovacuum_vacuum_cost_limit = %d)`,
		scaleFactor,
		costLimit,
	)
	_, err := ts.db.Exec(query)
	return err
}

// ResetAutovacuumParams resets the autovacuum parameters for the kline table to default
func (ts *TimescaleDB) ResetAutovacuumParams() error {
	_, err := ts.db.Exec(
		`ALTER TABLE binance.kline RESET (autovacuum_vacuum_scale_factor, autovacuum_vacuum_cost_limit)`,
	)
	return err
}

// -------------------- Storage Interface --------------------

func (ts *TimescaleDB) CreateCandle(*models.Kline) error               { return nil }
func (ts *TimescaleDB) DeleteCandle(int) error                         { return nil }
func (ts *TimescaleDB) UpdateCandle(*models.Kline) error               { return nil }
func (ts *TimescaleDB) GetCandleByOpenTime(int) (*models.Kline, error) { return nil, nil }

func (ts *TimescaleDB) FetchData(
	context.Context,
	string,
	int64,
	int64,
) ([]*models.KlineSimple, error) {
	return nil, nil
}
func (ts *TimescaleDB) Copy([]byte, *string, *string) error { return nil }

func (ts *TimescaleDB) Stream(r *zip.Reader) error {
	startTime := time.Now()

	conn, err := ts.db.Conn(context.Background())
	if err != nil {
		return err
	}

	zipSlice := strings.Split(r.File[0].Name, "-") // get name and interval
	zippedFile, err := r.File[0].Open()
	if err != nil {
		return fmt.Errorf("error opening ZIP entry: %v", err)
	}
	defer zippedFile.Close()

	// Create a CSV reader
	csvReader := csv.NewReader(zippedFile)

	err = conn.Raw(func(driverConn any) error {
		conn := driverConn.(*stdlib.Conn).Conn()
		// Ensure the symbol and interval combination exists in the symbol_intervals table
		var symbolIntervalID int64
		err = conn.QueryRow(
			context.Background(),
			`
            INSERT INTO binance.symbol_intervals (symbol, interval) 
            VALUES ($1, $2) 
            ON CONFLICT (symbol, interval) 
            DO UPDATE SET 
                symbol = EXCLUDED.symbol, 
                interval = EXCLUDED.interval
            RETURNING symbol_interval_id
            `,
			zipSlice[0],
			zipSlice[1],
		).
			Scan(&symbolIntervalID)
		if err != nil {
			return fmt.Errorf("error QueryRow: %v", err)
		}

		// Create the csvCopySource
		cs := &csvCopySource{
			symbolIntervalID: symbolIntervalID,
			reader:           csvReader,
		}

		conn.CopyFrom(context.Background(),
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
            si.symbol, 
            si.interval, 
            EXTRACT(EPOCH FROM kd.open_time) AS open_time_epoch, 
            EXTRACT(EPOCH FROM kd.close_time) AS close_time_epoch
        FROM 
            binance.kline AS kd
        JOIN 
            binance.symbol_intervals AS si ON kd.symbol_interval_id = si.symbol_interval_id
        ORDER BY 
            kd.open_time DESC 
        LIMIT 1;`

	d := new(models.KlineRequest)

	var o, c string

	err := ts.db.QueryRow(query).
		Scan(&d.Symbol, &d.Interval, &o, &c)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		return nil, err
	}

	oc := strings.Split(o, ".")
	cc := strings.Split(c, ".")
	o = oc[0] + oc[1]
	c = cc[0] + cc[1]
	var tempO, tempC float64

	tempO, err = strconv.ParseFloat(o, 64)
	if err != nil {
		return nil, err
	}
	d.OpenTime = int64(tempO) / 1000

	tempC, err = strconv.ParseFloat(c, 64)
	if err != nil {
		return nil, err
	}
	d.CloseTime = int64(tempC) / 1000

	return d, nil
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
