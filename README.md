# Trade Server

### TODOs
* [x] use ema200 to determine up/down trends
* [x] only trade with the trend -> above ema200 line uptrend (only BUY), below down trend (only SELL)
* [x] don't allow trades until the stops are targets are not reached (aka prevent crosses)
* [x] OR only allow trades if we are in profit when the sma cross happens...
* [x] make sure to use go-talib
* [x] pre-fetch data for indicators to avoid long wait times before the first trade
* [x] make strategies use the pre-fetched data (macd done, sma left)

## Improvement idea
* [] create a telegram/discord bot for instant notifications
* [] add bot commands to manage the app
