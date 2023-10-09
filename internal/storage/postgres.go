package storage

import (
	"archive/zip"
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"github.com/adamdenes/gotrade/internal/logger"
	"github.com/adamdenes/gotrade/internal/models"
	"github.com/lib/pq"
)

type PostgresDB struct {
	db *sql.DB
}

func NewPostgresDB(dsn string) (*PostgresDB, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatal(err)
	}

	if err := db.Ping(); err != nil {
		return nil, err
	}

	return &PostgresDB{db: db}, nil
}

func (p *PostgresDB) Init() error {
	return p.CreateCandleTable()
}

func (p *PostgresDB) CreateCandleTable() error {
	_, err := p.db.Exec("CREATE SCHEMA IF NOT EXISTS binance")
	if err != nil {
		return err
	}

	query := `CREATE TABLE IF NOT EXISTS binance.kline_data (
		id serial PRIMARY KEY,
		symbol VARCHAR(10) NOT NULL,
		interval VARCHAR(3) NOT NULL,
		open_time bigint NOT NULL UNIQUE,
		open NUMERIC(18, 8) NOT NULL,
		high NUMERIC(18, 8) NOT NULL,
		low NUMERIC(18, 8) NOT NULL,
		close NUMERIC(18, 8) NOT NULL,
		volume NUMERIC(18, 8) NOT NULL,
		close_time bigint NOT NULL,
		quote_volume NUMERIC(18, 8) NOT NULL,
		count INT NOT NULL,
		taker_buy_volume NUMERIC(18, 8) NOT NULL,
		taker_buy_quote_volume NUMERIC(18, 8) NOT NULL
	);`

	_, err = p.db.Exec(query)
	return err
}

func (p *PostgresDB) Close() {
	p.db.Close()
}

func (p *PostgresDB) CreateCandle(k *models.Kline) error {
	query := `
        INSERT INTO binance.kline_data (
			symbol,
			interval,
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
            $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
        )`

	stmt, err := p.db.Prepare(query)
	if err != nil {
		return err
	}
	defer stmt.Close()

	_, err = stmt.Exec(
		k.Symbol,
		k.Interval,
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

func (p *PostgresDB) DeleteCandle(int) error           { return nil }
func (p *PostgresDB) UpdateCandle(*models.Kline) error { return nil }

func (p *PostgresDB) GetCandleByOpenTime(time int) (*models.Kline, error) {
	query := `SELECT 
    symbol, 
    interval, 
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
    FROM binance.kline_data 
    where open_time = $1`

	d := new(models.Kline)

	fmt.Println(time)
	err := p.db.QueryRow(query, time).
		Scan(&d.Symbol, &d.Interval, &d.OpenTime, &d.Open, &d.High, &d.Low,
			&d.Close, &d.Volume, &d.CloseTime, &d.QuoteAssetVolume,
			&d.NumberOfTrades, &d.TakerBuyBaseAssetVol, &d.TakerBuyQuoteAssetVol)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		return nil, err
	}

	return d, nil
}

func (p *PostgresDB) Copy(r []byte, name *string, interval *string) error {
	startTime := time.Now()

	tx, err := p.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(
		pq.CopyInSchema(
			"binance",
			"kline_data",
			"symbol",
			"interval",
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
		),
	)
	if err != nil {
		return err
	}
	defer stmt.Close()

	// Unmarshal the data into a slice of slices
	var rawData [][]interface{}
	if err := json.Unmarshal(r, &rawData); err != nil {
		return err
	}

	// Iterate over the records
	for _, record := range rawData {
		// Process each record
		openTime := int64(record[0].(float64))
		open := record[1].(string)
		high := record[2].(string)
		low := record[3].(string)
		close := record[4].(string)
		volume := record[5].(string)
		closeTime := int64(record[6].(float64))
		quoteVolume := record[7].(string)
		count := int(record[8].(float64))
		takerBuyVolume := record[9].(string)
		takerBuyQuoteVolume := record[10].(string)

		_, err = stmt.Exec(
			name,
			interval,
			openTime,
			open,
			high,
			low,
			close,
			volume,
			closeTime,
			quoteVolume,
			count,
			takerBuyVolume,
			takerBuyQuoteVolume,
		)
		if err != nil {
			return err
		}
	}

	_, err = stmt.Exec()
	if err != nil {
		return err
	}

	err = tx.Commit()
	if err != nil {
		return err
	}

	logger.Info.Printf("Finished copying data to Postgres, it took %v\n", time.Since(startTime))
	return nil
}

func (p *PostgresDB) Stream(r *zip.Reader) error {
	startTime := time.Now()

	tx, err := p.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// only 1 file is in the zip archive, no need to loop over
	zipSlice := strings.Split(r.File[0].Name, "-") // get name and interval
	zippedFile, err := r.File[0].Open()
	if err != nil {
		return fmt.Errorf("error opening ZIP entry: %v", err)
	}
	defer zippedFile.Close()

	stmt, err := tx.Prepare(
		pq.CopyInSchema(
			"binance",
			"kline_data",
			"symbol",
			"interval",
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
		),
	)
	if err != nil {
		return err
	}
	defer stmt.Close()

	// Read the csv here
	csvReader := csv.NewReader(zippedFile)
	for {
		row, err := csvReader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("error reading CSV record: %v", err)
		}

		_, err = stmt.Exec(
			zipSlice[0],
			zipSlice[1],
			row[0],
			row[1],
			row[2],
			row[3],
			row[4],
			row[5],
			row[6],
			row[7],
			row[8],
			row[9],
			row[10],
		)
		if err != nil {
			return fmt.Errorf("error executing SQL query: %v", err)
		}
	}

	// Execute the COPY command
	_, err = stmt.Exec()
	if err != nil {
		return fmt.Errorf("error executing COPY command: %v", err)
	}

	if err = tx.Commit(); err != nil {
		return err
	}

	logger.Info.Printf("Finished streaming data to Postgres, it took %v\n", time.Since(startTime))
	return nil
}

func (p *PostgresDB) QueryLastRow() (*models.KlineRequest, error) {
	query := "SELECT symbol, interval, open_time, close_time FROM binance.kline_data ORDER BY open_time DESC LIMIT 1"
	d := new(models.KlineRequest)

	err := p.db.QueryRow(query).Scan(&d.Symbol, &d.Interval, &d.OpenTime, &d.CloseTime)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		return nil, err
	}

	return d, nil
}

func (p *PostgresDB) FetchData(
	ctx context.Context,
	symbol string,
	startTime, endTime int64,
) ([]*models.KlineSimple, error) {
	start := time.Now()

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Only query what is used to reduce overhead
	query := `SELECT 
        open_time, 
        open, 
        high, 
        low, 
        close, 
        close_time 
        FROM binance.kline_data 
        WHERE symbol = $1 AND open_time >= $2 AND close_time <= $3
        ORDER BY open_time ASC`

	rows, err := p.db.QueryContext(ctx, query, symbol, startTime, endTime)
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
		if err := rows.Scan(&d.OpenTime, &d.Open, &d.High, &d.Low, &d.Close, &d.CloseTime); err != nil {
			fmt.Println(err)
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
