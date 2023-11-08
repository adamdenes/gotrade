BEGIN;

CREATE SCHEMA IF NOT EXISTS binance;

CREATE TABLE IF NOT EXISTS binance.symbols (
    symbol_id SERIAL PRIMARY KEY,
    symbol TEXT UNIQUE NOT NULL
);

CREATE TABLE IF NOT EXISTS binance.intervals (
    interval_id SERIAL PRIMARY KEY,
    interval TEXT UNIQUE NOT NULL,
    interval_duration TEXT NOT NULL,
    UNIQUE (interval, interval_duration)
);

CREATE TABLE IF NOT EXISTS binance.symbols_intervals (
    symbol_interval_id SERIAL PRIMARY KEY,
    symbol_id INT REFERENCES binance.symbols(symbol_id),
    interval_id INT REFERENCES binance.intervals(interval_id),
    UNIQUE (symbol_id, interval_id)
);

CREATE TABLE IF NOT EXISTS binance.kline (
    symbol_interval_id INT REFERENCES binance.symbols_intervals(symbol_interval_id),
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
);

CREATE UNIQUE INDEX idx_unique_opentime_symbol_interval 
ON binance.kline(symbol_interval_id, open_time);

SELECT create_hypertable('binance.kline', 'open_time');

ALTER TABLE binance.kline
SET (timescaledb.compress, timescaledb.compress_orderby = 'open_time DESC', timescaledb.compress_segmentby = 'symbol_interval_id');

SELECT add_compression_policy('binance.kline', INTERVAL '14d');

-- SELECT compress_chunk(i, if_not_compressed => true)
--     FROM show_chunks(
--         'binance.kline',
--         now()::timestamp - INTERVAL '1 week') i;

CREATE TABLE IF NOT EXISTS binance.trades (
    id SERIAL PRIMARY KEY,
    binance_trade_id BIGINT,
    symbol VARCHAR(12) NOT NULL,
    order_id BIGINT,
    order_list_id INTEGER,
    price FLOAT NOT NULL,
    qty FLOAT,
    quote_qty FLOAT,
    commission FLOAT,
    commission_asset VARCHAR(10),
    trade_time TIMESTAMPTZ NOT NULL,
    is_buyer BOOLEAN,
    is_maker BOOLEAN,
    is_best_match BOOLEAN
);

COMMIT;
