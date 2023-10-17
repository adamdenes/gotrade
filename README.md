# Trade Server

### TODOs
* [x] handle cleanup of connections/resources
* [x] implement database `models` with interface
* [x] implement binance api request `GET /api/v3/klines`
* [x] implement query validations
* [x] request historycal data for the correct interval before getting real-time data
* [x] need to get historical data from Database
* [x] create algo to resample/aggregate historical data (no need to download them again and again)
* [x] make in-memory symbol table
* [x] create a separate table for the symbol cache and refresh every day/runtime
* [x] improve query performance in mat view?
* [] start writing tests...
* [] make compression and aggregate queries / buttons to set them
* [] maybe make a view to see the chunks and compression (use goroutines)?
* [] implement strategies for backtester/live view
