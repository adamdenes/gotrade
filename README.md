# Trade Server

### TODOs
* [x] handle cleanup of connections/resources
* [x] implement database `models` with interface
* [x] implement binance api request `GET /api/v3/klines`
* [x] implement query validations
* [x] request historycal data for the correct interval before getting real-time data
* [x] need to get historical data from Database
* [x] create algo to resample/aggregate historical data (no need to download them again and again)
* [] start writing tests...
* [] make compression and aggregate queries / buttons to set them
* [] maybe make a view to see the chunks and compression (use goroutines)?
* [] make in-memory symbol table, eventually create a separate table for it that is refreshed every day or so
