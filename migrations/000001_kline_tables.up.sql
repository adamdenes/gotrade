BEGIN;

CREATE SCHEMA IF NOT EXISTS binance;

CREATE TABLE IF NOT EXISTS binance.symbols (
    symbol_id SERIAL PRIMARY KEY,
    symbol TEXT UNIQUE NOT NULL,
    base_asset TEXT,
    quote_asset TEXT
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
    open NUMERIC(20, 8) NOT NULL,
    high NUMERIC(20, 8) NOT NULL,
    low NUMERIC(20, 8) NOT NULL,
    close NUMERIC(20, 8) NOT NULL,
    volume NUMERIC(20, 8) NOT NULL,
    close_time TIMESTAMPTZ NOT NULL,
    quote_volume NUMERIC(20, 8) NOT NULL,
    count INT NOT NULL,
    taker_buy_volume NUMERIC(20, 8) NOT NULL,
    taker_buy_quote_volume NUMERIC(20, 8) NOT NULL
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

CREATE TABLE IF NOT EXISTS binance.orders (
    symbol VARCHAR(255) NOT NULL,
    strategy VARCHAR(255) NOT NULL,
    order_id BIGINT PRIMARY KEY,
    order_list_id BIGINT NOT NULL,
    client_order_id VARCHAR(255) NOT NULL,
    price NUMERIC(20, 8) NOT NULL,
    orig_qty NUMERIC(20, 8) NOT NULL,
    executed_qty NUMERIC(20, 8),
    cummulative_quote_qty NUMERIC(20, 8),
    status VARCHAR(255) NOT NULL,
    time_in_force VARCHAR(255) NOT NULL,
    type VARCHAR(255) NOT NULL,
    side VARCHAR(255) NOT NULL,
    stop_price NUMERIC(20, 8) NOT NULL,
    iceberg_qty NUMERIC(20, 8) NOT NULL,
    time TIMESTAMPTZ NOT NULL,
    update_time TIMESTAMPTZ NOT NULL,
    is_working BOOLEAN NOT NULL,
    working_time TIMESTAMPTZ NOT NULL,
    orig_quote_order_qty NUMERIC(20, 8),
    self_trade_prevention_mode VARCHAR(255) NOT NULL
);


CREATE TABLE IF NOT EXISTS binance.trades (
    id SERIAL PRIMARY KEY,
    strategy VARCHAR(255) NOT NULL,
    status VARCHAR(50) NOT NULL,
    symbol VARCHAR(50) NOT NULL,
    order_id BIGINT,
    order_list_id BIGINT,
    price NUMERIC(20, 8) NOT NULL,
    qty NUMERIC(20, 8),
    quote_qty NUMERIC(20, 8),
    commission NUMERIC(20, 8),
    commission_asset VARCHAR(10),
    trade_time TIMESTAMPTZ NOT NULL,
    is_buyer BOOLEAN,
    is_maker BOOLEAN,
    is_best_match BOOLEAN
);

CREATE TABLE IF NOT EXISTS binance.bots (
    id SERIAL PRIMARY KEY,
    symbol VARCHAR(50) NOT NULL,
    interval VARCHAR(10) NOT NULL,
    strategy VARCHAR(255) NOT NULL,
    status VARCHAR(50) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS binance.price_filters (
    filter_id SERIAL PRIMARY KEY,
    symbol_id INT NOT NULL,
    max_price NUMERIC(20, 8),
    min_price NUMERIC(20, 8),
    tick_size NUMERIC(20, 8),
    FOREIGN KEY (symbol_id) REFERENCES binance.symbols(symbol_id)
);

CREATE TABLE IF NOT EXISTS binance.lot_size_filters (
    filter_id SERIAL PRIMARY KEY,
    symbol_id INT NOT NULL,
    max_qty NUMERIC(20, 8),
    min_qty NUMERIC(20, 8),
    step_size NUMERIC(20, 8),
    FOREIGN KEY (symbol_id) REFERENCES binance.symbols(symbol_id)
);

CREATE TABLE IF NOT EXISTS binance.iceberg_parts_filters (
    filter_id SERIAL PRIMARY KEY,
    symbol_id INT NOT NULL,
    parts_limit INT,
    FOREIGN KEY (symbol_id) REFERENCES binance.symbols(symbol_id)
);

CREATE TABLE IF NOT EXISTS binance.market_lot_size_filters (
    filter_id SERIAL PRIMARY KEY,
    symbol_id INT NOT NULL,
    max_qty NUMERIC(20, 8),
    min_qty NUMERIC(20, 8),
    step_size NUMERIC(20, 8),
    FOREIGN KEY (symbol_id) REFERENCES binance.symbols(symbol_id)
);

CREATE TABLE IF NOT EXISTS binance.trailing_delta_filters (
    filter_id SERIAL PRIMARY KEY,
    symbol_id INT NOT NULL,
    max_trailing_above_delta INT,
    max_trailing_below_delta INT,
    min_trailing_above_delta INT,
    min_trailing_below_delta INT,
    FOREIGN KEY (symbol_id) REFERENCES binance.symbols(symbol_id)
);

CREATE TABLE IF NOT EXISTS binance.percent_price_by_side_filters (
    filter_id SERIAL PRIMARY KEY,
    symbol_id INT NOT NULL,
    ask_multiplier_down NUMERIC(20, 8),
    ask_multiplier_up NUMERIC(20, 8),
    avg_price_mins INT,
    bid_multiplier_down NUMERIC(20, 8),
    bid_multiplier_up NUMERIC(20, 8),
    FOREIGN KEY (symbol_id) REFERENCES binance.symbols(symbol_id)
);

CREATE TABLE IF NOT EXISTS binance.notional_filters (
    filter_id SERIAL PRIMARY KEY,
    symbol_id INT NOT NULL,
    apply_max_to_market BOOLEAN,
    apply_min_to_market BOOLEAN,
    max_notional NUMERIC(20, 8),
    min_notional NUMERIC(20, 8),
    FOREIGN KEY (symbol_id) REFERENCES binance.symbols(symbol_id)
);

CREATE TABLE IF NOT EXISTS binance.max_num_orders_filters (
    filter_id SERIAL PRIMARY KEY,
    symbol_id INT NOT NULL,
    max_num_orders INT,
    FOREIGN KEY (symbol_id) REFERENCES binance.symbols(symbol_id)
);

CREATE TABLE IF NOT EXISTS binance.max_num_algo_orders_filters (
    filter_id SERIAL PRIMARY KEY,
    symbol_id INT NOT NULL,
    max_num_algo_orders INT,
    FOREIGN KEY (symbol_id) REFERENCES binance.symbols(symbol_id)
);

COMMIT;
