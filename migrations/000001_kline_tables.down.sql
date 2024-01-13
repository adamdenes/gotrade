BEGIN;

DROP TABLE IF EXISTS binance.price_filters CASCADE;
DROP TABLE IF EXISTS binance.lot_size_filters CASCADE;
DROP TABLE IF EXISTS binance.iceberg_parts_filters CASCADE;
DROP TABLE IF EXISTS binance.market_lot_size_filters CASCADE;
DROP TABLE IF EXISTS binance.trailing_delta_filters CASCADE;
DROP TABLE IF EXISTS binance.percent_price_by_side_filters CASCADE;
DROP TABLE IF EXISTS binance.notional_filters CASCADE;
DROP TABLE IF EXISTS binance.max_num_orders_filters CASCADE;
DROP TABLE IF EXISTS binance.max_num_algo_orders_filters CASCADE;
DROP TABLE IF EXISTS binance.max_position_filters CASCADE;
DROP TABLE IF EXISTS binance.bots CASCADE;
DROP TABLE IF EXISTS binance.trades CASCADE;
DROP TABLE IF EXISTS binance.orders CASCADE;
DROP TABLE IF EXISTS binance.symbols CASCADE;
DROP TABLE IF EXISTS binance.intervals CASCADE;
DROP TABLE IF EXISTS binance.symbols_intervals CASCADE;
DROP TABLE IF EXISTS binance.kline CASCADE;

DROP SCHEMA IF EXISTS binance;

COMMIT;
