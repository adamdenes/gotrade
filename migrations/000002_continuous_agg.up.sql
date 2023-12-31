BEGIN;

CREATE MATERIALIZED VIEW IF NOT EXISTS binance.aggregate_1m
WITH (timescaledb.continuous) AS 
SELECT 
    kd.symbol_interval_id as siid,
    time_bucket(INTERVAL '1 minute', kd.open_time) as bucket,
    FIRST(kd.open, kd.open_time) as first_open,
    MAX(kd.high) as max_high,
    MIN(kd.low) as min_low,
    LAST(kd.close, kd.close_time) as last_close,
    SUM(kd.volume) as total_volume
FROM binance.kline AS kd
GROUP BY bucket, kd.symbol_interval_id
WITH NO DATA;

SELECT add_continuous_aggregate_policy(
    'binance.aggregate_1m',
    start_offset => INTERVAL '1 hour',
    end_offset => INTERVAL '1 minute',
    schedule_interval => INTERVAL '5 minutes');

CREATE INDEX ON binance.aggregate_1m (bucket, siid);

ALTER MATERIALIZED VIEW binance.aggregate_1m SET (timescaledb.compress = true);
SELECT add_compression_policy('binance.aggregate_1m', compress_after => INTERVAL '7 days');

-- ###############################################################################################################

CREATE MATERIALIZED VIEW IF NOT EXISTS binance.aggregate_5m
WITH (timescaledb.continuous) AS 
SELECT 
    kd.symbol_interval_id as siid,
    time_bucket(INTERVAL '5 minutes', kd.open_time) as bucket,
    FIRST(kd.open, kd.open_time) as first_open,
    MAX(kd.high) as max_high,
    MIN(kd.low) as min_low,
    LAST(kd.close, kd.close_time) as last_close,
    SUM(kd.volume) as total_volume
FROM binance.kline AS kd
GROUP BY bucket, kd.symbol_interval_id
WITH NO DATA;

SELECT add_continuous_aggregate_policy(
    'binance.aggregate_5m',
    start_offset => INTERVAL '1 day',
    end_offset => INTERVAL '5 minutes',
    schedule_interval => INTERVAL '15 minutes');

CREATE INDEX ON binance.aggregate_5m (bucket, siid);

ALTER MATERIALIZED VIEW binance.aggregate_5m SET (timescaledb.compress = true);
SELECT add_compression_policy('binance.aggregate_5m', compress_after => INTERVAL '7 days');

-- ###############################################################################################################

CREATE MATERIALIZED VIEW IF NOT EXISTS binance.aggregate_1h
WITH (timescaledb.continuous) AS 
SELECT 
    kd.symbol_interval_id as siid,
    time_bucket(INTERVAL '1 hour', kd.open_time) as bucket,
    FIRST(kd.open, kd.open_time) as first_open,
    MAX(kd.high) as max_high,
    MIN(kd.low) as min_low,
    LAST(kd.close, kd.close_time) as last_close,
    SUM(kd.volume) as total_volume
FROM binance.kline AS kd
GROUP BY bucket, kd.symbol_interval_id
WITH NO DATA;

SELECT add_continuous_aggregate_policy(
    'binance.aggregate_1h',
    start_offset => INTERVAL '1 day',
    end_offset => INTERVAL '1 hour',
    schedule_interval => INTERVAL '2 hours');

CREATE INDEX ON binance.aggregate_1h (bucket, siid);

ALTER MATERIALIZED VIEW binance.aggregate_1h SET (timescaledb.compress = true);
SELECT add_compression_policy('binance.aggregate_1h', compress_after => INTERVAL '7 days');

-- ###############################################################################################################

CREATE MATERIALIZED VIEW IF NOT EXISTS binance.aggregate_4h
WITH (timescaledb.continuous) AS 
SELECT 
    kd.symbol_interval_id as siid,
    time_bucket(INTERVAL '4 hours', kd.open_time) as bucket,
    FIRST(kd.open, kd.open_time) as first_open,
    MAX(kd.high) as max_high,
    MIN(kd.low) as min_low,
    LAST(kd.close, kd.close_time) as last_close,
    SUM(kd.volume) as total_volume
FROM binance.kline AS kd
GROUP BY bucket, kd.symbol_interval_id
WITH NO DATA;

SELECT add_continuous_aggregate_policy(
    'binance.aggregate_4h',
    start_offset => INTERVAL '1 day',
    end_offset => INTERVAL '4 hours',
    schedule_interval => INTERVAL '8 hours');

CREATE INDEX ON binance.aggregate_4h (bucket, siid);

ALTER MATERIALIZED VIEW binance.aggregate_4h SET (timescaledb.compress = true);
SELECT add_compression_policy('binance.aggregate_4h', compress_after => INTERVAL '7 days');

-- ###############################################################################################################

CREATE MATERIALIZED VIEW IF NOT EXISTS binance.aggregate_1d
WITH (timescaledb.continuous) AS 
SELECT 
    kd.symbol_interval_id as siid,
    time_bucket(INTERVAL '1 day', kd.open_time) as bucket,
    FIRST(kd.open, kd.open_time) as first_open,
    MAX(kd.high) as max_high,
    MIN(kd.low) as min_low,
    LAST(kd.close, kd.close_time) as last_close,
    SUM(kd.volume) as total_volume
FROM binance.kline AS kd
GROUP BY bucket, kd.symbol_interval_id 
WITH NO DATA;

SELECT add_continuous_aggregate_policy(
    'binance.aggregate_1d',
    start_offset => INTERVAL '1 week',
    end_offset => INTERVAL '1 day',
    schedule_interval => INTERVAL '2 days');

CREATE INDEX ON binance.aggregate_1d (bucket, siid);

ALTER MATERIALIZED VIEW binance.aggregate_1d SET (timescaledb.compress = true);
SELECT add_compression_policy('binance.aggregate_1d', compress_after => INTERVAL '8 days');

-- ###############################################################################################################

CREATE MATERIALIZED VIEW IF NOT EXISTS binance.aggregate_1w
WITH (timescaledb.continuous) AS 
SELECT 
    kd.symbol_interval_id as siid,
    time_bucket(INTERVAL '1 week', kd.open_time) as bucket,
    FIRST(kd.open, kd.open_time) as first_open,
    MAX(kd.high) as max_high,
    MIN(kd.low) as min_low,
    LAST(kd.close, kd.close_time) as last_close,
    SUM(kd.volume) as total_volume
FROM binance.kline AS kd
GROUP BY bucket, kd.symbol_interval_id
WITH NO DATA;

SELECT add_continuous_aggregate_policy(
    'binance.aggregate_1w',
    start_offset => INTERVAL '15 days',
    end_offset => INTERVAL '1 day',
    schedule_interval => INTERVAL '1 week');

CREATE INDEX ON binance.aggregate_1w (bucket, siid);

ALTER MATERIALIZED VIEW binance.aggregate_1w SET (timescaledb.compress = true);
SELECT add_compression_policy('binance.aggregate_1w', compress_after => INTERVAL '1 months');

COMMIT;
