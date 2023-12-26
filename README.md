# Trade Server

### TODOs 0.9.0
* [] rework `CalculateStopLoss` - incorporate ATR
* [x] store symbol filters from exchangeInfo
* [] update strategies with the new trade filters
* [] revisit `RefreshContinuousAggregate` -> not updatging
* [] if database is empty (no orders) -> query all orderds with `GetAllOrders`

## Improvement idea
* [] create a telegram/discord bot for instant notifications
* [] add bot commands to manage the app
* [] allow multiple symbols to be traded with one bot (i.e. top 5 volume)
