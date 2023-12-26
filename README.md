# Trade Server

### TODOs 0.9.0
* [x] store symbol filters from exchangeInfo
* [] update strategies with the new trade filters
* [x] during Stream base and quote assests are missing
     Processed symbol in binance.symbols -> (BTCUSDT, , ), returning symbol_id: 168
* [x] revisit `RefreshContinuousAggregate` -> not updatging
* [x] if database is empty (no orders) -> query all orderds with `GetAllOrders`

## Improvement ideas
* [] rework `CalculateStopLoss` - incorporate ATR
* [] create a telegram/discord bot for instant notifications
* [] add bot commands to manage the app
* [] allow multiple symbols to be traded with one bot (i.e. top 5 volume)
