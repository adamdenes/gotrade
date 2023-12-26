package rest

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/adamdenes/gotrade/internal/logger"
	"github.com/adamdenes/gotrade/internal/models"
)

const (
	apiEndpoint = "https://testnet.binance.vision/api/v3/"
	apiKey      = "APCA_API_KEY_ID"
	apiSecret   = "APCA_API_SECRET_KEY"
)

func BuildURI(base string, query ...string) string {
	var sb strings.Builder
	sb.WriteString(base)
	for _, q := range query {
		// Check if the query string starts with "symbol="
		if strings.HasPrefix(q, "symbol=") {
			parts := strings.Split(q, "&")
			part := strings.Split(parts[0], "=")
			part[1] = strings.ToUpper(part[1])
			parts[0] = strings.Join(part, "=")
			sb.WriteString(strings.Join(parts, "&"))
		} else {
			sb.WriteString(q)
		}
	}
	return sb.String()
}

// Sign creates the signature for the requests that require authentication
func Sign(secret []byte, query string) (string, error) {
	// Create an HMAC-SHA256 hasher
	hmacHash := hmac.New(sha256.New, secret)

	// Write the query string to the hasher
	_, err := hmacHash.Write([]byte(query))
	if err != nil {
		return "", err
	}

	// Get the raw bytes of the HMAC hash
	rawSignature := hmacHash.Sum(nil)

	// Encode the raw signature to a hexadecimal string
	signature := hex.EncodeToString(rawSignature)

	return signature, nil
}

func CalculatePositionSize(asset string, risk float64, invalidation float64) (float64, error) {
	// Account size – $5000
	// Account risk – 1%
	// Invalidation point (distance to stop-loss) – 5%
	// position size = account size x account risk / invalidation point

	// The bigger the invalidation point the less the position size going to be
	// $1000 = $5000 x 0.01 / 0.05
	// $500 = $5000 x 0.01 / 0.1

	freeBalance, err := getBalance(asset)
	if err != nil {
		return 0.0, err
	}
	// 4. Calculate the position size
	posSize := freeBalance * risk / invalidation
	logger.Debug.Printf(
		"posSize = %f, freeBalance = %f, risk = %.2f%%, invalidation = %.2f%%",
		posSize,
		freeBalance,
		risk*100,
		invalidation*100,
	)
	return posSize, nil
}

func getBalance(asset string) (float64, error) {
	// 1. Get balances from account endpoint
	acc, err := GetAccount()
	if err != nil {
		return 0.0, fmt.Errorf("error calculating position size: %w", err)
	}

	var balances struct {
		Balances []struct {
			Asset  string `json:"asset"`
			Free   string `json:"free"`
			Locked string `json:"locked"`
		} `json:"balances"`
	}

	if err := json.Unmarshal(acc, &balances); err != nil {
		logger.Debug.Println("\t", string(acc))
		logger.Debug.Println("\n\n\t", balances.Balances)
		return 0.0, fmt.Errorf("error marshalling account data: %w", err)
	}

	// 2. Find the free
	var (
		freeBalance float64
		found       bool // Flag to check if the asset is found
	)

	for _, b := range balances.Balances {
		if strings.Contains(asset, b.Asset) && b.Asset == "USDT" {
			// 3. Get the available/free asset balance
			asset = b.Asset
			free, err := strconv.ParseFloat(b.Free, 64)
			if err != nil {
				return 0.0, err
			}
			freeBalance = free
			found = true

			// logger.Info.Printf("Balance: %+v", b)
			break
		}
	}

	if !found {
		return 0.0, fmt.Errorf("asset %q is not available in account balance", asset)
	}

	return freeBalance, nil
}

func validateOrder(order *models.PostOrder) error {
	switch order.Type {
	case models.LIMIT:
		if order.TimeInForce == "" || order.Quantity == 0.0 || order.Price == 0.0 {
			return fmt.Errorf("limit order requires timeInForce, quantity, and price")
		}
	case models.MARKET:
		if order.Quantity == 0.0 && order.QuoteOrderQty == 0.0 {
			return fmt.Errorf("market order requires either quantity or quoteOrderQty")
		}
	case models.STOP_LOSS:
		if order.Quantity == 0.0 || order.StopPrice == 0.0 || order.TrailingDelta == 0 {
			return fmt.Errorf("stop-loss order requires quantity, stopPrice and trailingDelta")
		}
	case models.STOP_LOSS_LIMIT:
		if order.TimeInForce == "" || order.Quantity == 0.0 || order.Price == 0.0 ||
			order.StopPrice == 0.0 || order.TrailingDelta == 0 {
			return fmt.Errorf(
				"stop-loss limit order requires timeInForce, quantity, price, stopPrice and trailingDelta",
			)
		}
	case models.TAKE_PROFIT:
		if order.Quantity == 0.0 || order.StopPrice == 0.0 || order.TrailingDelta == 0 {
			return fmt.Errorf("take-profit order requires quantity, stopPrice and trailingDelta")
		}
	case models.TAKE_PROFIT_LIMIT:
		if order.TimeInForce == "" || order.Quantity == 0.0 || order.Price == 0.0 ||
			order.StopPrice == 0.0 || order.TrailingDelta == 0 {
			return fmt.Errorf(
				"take-profit limit order requires timeInForce, quantity, price, stopPrice and trailingDelta",
			)
		}
	case models.LIMIT_MAKER:
		if order.Quantity == 0.0 || order.Price == 0.0 {
			return fmt.Errorf("limit-maker order requires quantity and price")
		}
	default:
		return fmt.Errorf("unsupported order type: %s", order.Type)
	}

	return nil
}

// ----------------- REST -----------------

/* IP Limits

   - Every request will contain X-MBX-USED-WEIGHT-(intervalNum)(intervalLetter) in the response headers which has the current used weight for the IP for all request rate limiters defined.
   - Each route has a weight which determines for the number of requests each endpoint counts for. Heavier endpoints and endpoints that do operations on multiple symbols will have a heavier weight.
   - When a 429 is received, it's your obligation as an API to back off and not spam the API.
   - Repeatedly violating rate limits and/or failing to back off after receiving 429s will result in an automated IP ban (HTTP status 418).
   - IP bans are tracked and scale in duration for repeat offenders, from 2 minutes to 3 days.
   - A Retry-After header is sent with a 418 or 429 responses and will give the number of seconds required to wait, in the case of a 429, to prevent a ban, or, in the case of a 418, until the ban is over.
   - The limits on the API are based on the IPs, not the API keys.
*/

// Query makes a GET request for the given query string/url with an additional backoff timer.
// Once a "Retry-After" header is received, the query mechanism will go to sleep. The caller
// has to implement retry mechanism.
func Query(rt, qs string, contType string, jsonBody []byte) ([]byte, error) {
	req, err := http.NewRequest(rt, qs, bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, err
	}

	req.Header.Add("X-MBX-APIKEY", os.Getenv(apiKey))
	req.Header.Add("Content-Type", contType)

	client := &http.Client{}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		logger.Debug.Printf("HTTP Status code: %v, X-Mbx-Used-Weight: %q, Retry-After: %q\n",
			resp.StatusCode,
			resp.Header.Get("X-Mbx-Used-Weight"),
			resp.Header.Get("Retry-After"),
		)
		logger.Error.Printf("RETRY AFTER RECEIVED: %q\n", resp.Header.Values("Retry-After"))
		// Get the backoff timer from respons body
		timer, err := strconv.ParseInt(resp.Header.Get("Retry-After"), 10, 64)
		if err != nil {
			return nil, err
		}
		logger.Error.Printf(
			"%v Retry-After received, backing off for: %d\n",
			resp.StatusCode,
			timer,
		)

		return nil, &models.RequestError{
			Err:    errors.New("ErrBackOff"),
			Timer:  time.Duration(timer),
			Status: resp.StatusCode,
		}
	}

	r, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP Status: %s\n\tResponse Body: %s", resp.Status, r)
	}

	return r, nil
}

/* The base endpoint https://data-api.binance.vision can be used to access the following API endpoints that have NONE as security type:

   GET /api/v3/aggTrades
   GET /api/v3/avgPrice
   GET /api/v3/depth
   GET /api/v3/exchangeInfo
   GET /api/v3/klines
   GET /api/v3/ping
   GET /api/v3/ticker
   GET /api/v3/ticker/24hr
   GET /api/v3/ticker/bookTicker
   GET /api/v3/ticker/price
   GET /api/v3/time
   GET /api/v3/trades
   GET /api/v3/uiKlines
*/

/*
GET /api/v3/klines

		Kline/candlestick bars for a symbol.
		Klines are uniquely identified by their open time.

		symbol 		STRING 	YES
		fromId 		LONG 	NO 	id to get aggregate trades from INCLUSIVE.
		startTime 	LONG 	NO 	Timestamp in ms to get aggregate trades from INCLUSIVE.
		endTime 	LONG 	NO 	Timestamp in ms to get aggregate trades until INCLUSIVE.
		limit 		INT 	NO 	Default 500; max 1000.


	    If startTime and endTime are not sent, the most recent klines are returned.
*/
func GetKlines(q ...string) ([]byte, error) {
	uri := BuildURI(apiEndpoint+"klines?", q...)
	resp, err := Query("GET", uri, "application/json", nil)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

/*
GET /api/v3/uiKlines

	The request is similar to klines having the same parameters and response.
	uiKlines return modified kline data, optimized for presentation of candlestick charts.

	symbol 		STRING 	YES
	fromId 		LONG 	NO 	id to get aggregate trades from INCLUSIVE.
	startTime 	LONG 	NO 	Timestamp in ms to get aggregate trades from INCLUSIVE.
	endTime 	LONG 	NO 	Timestamp in ms to get aggregate trades until INCLUSIVE.
	limit 		INT 	NO 	Default 500; max 1000.
*/
func GetUiKlines(q ...string) ([]byte, error) {
	uri := BuildURI(apiEndpoint+"uiKlines?", q...)
	resp, err := Query("GET", uri, "application/json", nil)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

/*
GET /api/v3/exchangeInfo

	OPTIONS (not all of them):
	- no parameter	"https://api.binance.com/api/v3/exchangeInfo"
	- symbol		"https://api.binance.com/api/v3/exchangeInfo?symbol=BNBBTC"
	- symbols		'https://api.binance.com/api/v3/exchangeInfo?symbols=["BTCUSDT","BNBBTC"]'
	- permissions	"https://api.binance.com/api/v3/exchangeInfo?permissions=SPOT"

	Notes: If the value provided to symbol or symbols do not exist,
	the endpoint will throw an error saying the symbol is invalid.
*/
func NewSymbolCache() (map[string]*models.SymbolFilter, error) {
	uri := BuildURI(apiEndpoint + "exchangeInfo")
	resp, err := Query("GET", uri, "application/json", nil)
	if err != nil {
		return nil, err
	}

	// Define a struct to unmarshal the JSON response
	var exchangeInfo struct {
		Symbols []models.SymbolFilter `json:"symbols"`
	}

	err = json.Unmarshal(resp, &exchangeInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	cache := make(map[string]*models.SymbolFilter, len(exchangeInfo.Symbols))
	for i := range exchangeInfo.Symbols {
		symbolFilter := &exchangeInfo.Symbols[i]
		cache[symbolFilter.Symbol] = symbolFilter
	}

	return cache, nil
}

/*
Spot Trading Endpoints

Test New Order (TRADE)

	Response: {}

POST /api/v3/order/test
Test new order creation and signature/recvWindow long. Creates and validates a new order but does not send it into the matching engine.
*/
func PostTestOrder(order *models.PostOrder) ([]byte, error) {
	if err := validateOrder(order); err != nil {
		return nil, err
	}
	signedQuery, err := Sign([]byte(os.Getenv(apiSecret)), order.String())
	if err != nil {
		return nil, err
	}

	uri := BuildURI(apiEndpoint + "order/test")
	jb := []byte(order.String() + "&signature=" + signedQuery)
	resp, err := Query("POST", uri, "application/json", jb)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

/*
POST /api/v3/order

Send in a new order.

Additional mandatory parameters based on type:

	Type 	            Additional mandatory parameters
	LIMIT 	            timeInForce, quantity, price
	MARKET 	            quantity or quoteOrderQty
	STOP_LOSS 	        quantity, stopPrice or trailingDelta
	STOP_LOSS_LIMIT 	    timeInForce, quantity, price, stopPrice or trailingDelta
	TAKE_PROFIT 	        quantity, stopPrice or trailingDelta
	TAKE_PROFIT_LIMIT 	timeInForce, quantity, price, stopPrice or trailingDelta
	LIMIT_MAKER 	        quantity, price
*/
func PostOrder(order *models.PostOrder) (*models.PostOrderResponse, error) {
	if err := validateOrder(order); err != nil {
		return nil, err
	}
	signedQuery, err := Sign([]byte(os.Getenv(apiSecret)), order.String())
	if err != nil {
		return nil, err
	}

	uri := BuildURI(apiEndpoint + "order")
	jb := []byte(order.String() + "&signature=" + signedQuery)
	resp, err := Query("POST", uri, "application/json", jb)
	if err != nil {
		return nil, err
	}

	var orderResponse models.PostOrderResponse
	err = json.Unmarshal(resp, &orderResponse)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling response JSON: %v", err)
	}

	return &orderResponse, nil
}

/*
POST /api/v3/order/oco

# Send in a new OCO

Weight(UID): 2 Weight(IP): 1

Price Restrictions:

	SELL: Limit Price > Last Price > Stop Price
	BUY: Limit Price < Last Price < Stop Price

Quantity Restrictions:

	Both legs must have the same quantity
	ICEBERG quantities however do not have to be the same.

Order Rate Limit

	OCO counts as 2 orders against the order rate limit
*/
func PostOrderOCO(oco *models.PostOrderOCO) (*models.PostOrderOCOResponse, error) {
	signedQuery, err := Sign([]byte(os.Getenv(apiSecret)), oco.String())
	if err != nil {
		return nil, err
	}

	uri := BuildURI(apiEndpoint + "order/oco")
	jb := []byte(oco.String() + "&signature=" + signedQuery)
	resp, err := Query("POST", uri, "application/json", jb)
	if err != nil {
		return nil, err
	}

	var ocoResponse models.PostOrderOCOResponse
	err = json.Unmarshal(resp, &ocoResponse)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling response JSON: %v", err)
	}

	return &ocoResponse, nil
}

/*
Check Server Time

	Response: { "serverTime": 1499827319559	}

GET /api/v3/time

Test connectivity to the Rest API and get the current server time.

Weight(IP): 1
*/
func GetServerTime() (int64, error) {
	uri := BuildURI(apiEndpoint + "time")
	resp, err := Query("GET", uri, "application/json", nil)
	if err != nil {
		return 0, err
	}

	var st struct {
		ServerTime int64 `json:"serverTime"`
	}

	if err := json.Unmarshal(resp, &st); err != nil {
		return 0, err
	}
	return st.ServerTime, nil
}

/*
GET /api/v3/account

Get current account information.

Weight(IP): 20

Parameters:

	Name 	    Type 	Mandatory 	Description
	recvWindow 	LONG 	NO 	        The value cannot be greater than 60000
	timestamp 	LONG 	YES
*/
func GetAccount() ([]byte, error) {
	// To avoid timestamp mismatch with server
	st, err := GetServerTime()
	if err != nil {
		return nil, err
	}

	q := fmt.Sprintf("recvWindow=%d&timestamp=%d", 5000, st)
	signedQuery, err := Sign([]byte(os.Getenv(apiSecret)), q)
	if err != nil {
		return nil, err
	}

	uri := BuildURI(apiEndpoint+"account?", q, "&signature=", signedQuery)
	resp, err := Query("GET", uri, "application/json", nil)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

/*
Query Current Order Count Usage (TRADE)

GET /api/v3/rateLimit/order

Displays the user's current order count usage for all intervals.

Weight(IP): 40

Parameters:

	Name 	    Type 	Mandatory 	Description
	recvWindow 	LONG 	NO 	        The value cannot be greater than 60000
	timestamp 	LONG 	YES
*/
func GetOrderCountUsage() ([]byte, error) {
	st, err := GetServerTime()
	if err != nil {
		return nil, err
	}

	q := fmt.Sprintf("recvWindow=%d&timestamp=%d", 5000, st)
	signedQuery, err := Sign([]byte(os.Getenv(apiSecret)), q)
	if err != nil {
		return nil, err
	}

	uri := BuildURI(apiEndpoint+"rateLimit/order?", q, "&signature=", signedQuery)
	resp, err := Query("GET", uri, "application/json", nil)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

/*
All Orders (USER_DATA)

GET /api/v3/allOrders

Get all account orders; active, canceled, or filled.

Weight(IP): 20

Parameters:

	Name 	    Type 	Mandatory 	Description
	symbol 	    STRING 	YES
	orderId 	LONG 	NO
	startTime 	LONG 	NO
	endTime 	LONG 	NO
	limit 	    INT 	NO 	Default 500; max 1000.
	recvWindow 	LONG 	NO 	The value cannot be greater than 60000
	timestamp 	LONG 	YES

Notes:

  - If orderId is set, it will get orders >= that orderId. Otherwise most recent orders are returned.

  - For some historical orders cummulativeQuoteQty will be < 0, meaning the data is not available at this time.

  - If startTime and/or endTime provided, orderId is not required.

  - The payload sample does not show all fields that can appear. Please refer to Conditional fields in Order Responses.
*/
func GetAllOrders(symbol string) ([]*models.GetOrderResponse, error) {
	st, err := GetServerTime()
	if err != nil {
		return nil, err
	}

	q := fmt.Sprintf("symbol=%s&recvWindow=%d&timestamp=%d", symbol, 5000, st)
	signedQuery, err := Sign([]byte(os.Getenv(apiSecret)), q)
	if err != nil {
		return nil, err
	}

	uri := BuildURI(apiEndpoint+"allOrders?", q, "&signature=", signedQuery)
	resp, err := Query("GET", uri, "application/json", nil)
	if err != nil {
		return nil, err
	}

	var or []*models.GetOrderResponse

	err = json.Unmarshal(resp, &or)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling all orders: %w", err)
	}

	return or, nil
}

/*
Current Open Orders (USER_DATA)

GET /api/v3/openOrders

Get all open orders on a symbol. Careful when accessing this with no symbol.

Weight(IP): 6 for a single symbol; 80 when the symbol parameter is omitted;

Parameters:

	Name 	    Type 	Mandatory 	Description
	symbol 	    STRING 	NO
	recvWindow 	LONG 	NO 	        The value cannot be greater than 60000
	timestamp 	LONG 	YES

If the symbol is not sent, orders for all symbols will be returned in an array.
*/
func GetOpenOrders(symbol string) ([]*models.GetOrderResponse, error) {
	st, err := GetServerTime()
	if err != nil {
		return nil, err
	}

	var q string
	if symbol == "" {
		q = fmt.Sprintf("recvWindow=%d&timestamp=%d", 5000, st)
	} else {
		q = fmt.Sprintf("symbol=%s&recvWindow=%d&timestamp=%d", symbol, 5000, st)
	}

	signedQuery, err := Sign([]byte(os.Getenv(apiSecret)), q)
	if err != nil {
		return nil, err
	}

	uri := BuildURI(apiEndpoint+"openOrders?", q, "&signature=", signedQuery)
	resp, err := Query("GET", uri, "application/json", nil)
	if err != nil {
		return nil, err
	}

	var orders []*models.GetOrderResponse

	err = json.Unmarshal(resp, &orders)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling open orders: %w", err)
	}

	return orders, nil
}

/*
Query OCO (USER_DATA)

GET /api/v3/orderList

# Retrieves a specific OCO based on provided optional parameters

Weight(IP): 4

Parameters:

	Name 	            Type 	Mandatory 	Description
	orderListId 	    LONG 	NO 	Either orderListId or origClientOrderId must be provided
	origClientOrderId 	STRING 	NO 	Either orderListId or origClientOrderId must be provided
	recvWindow 	        LONG 	NO 	The value cannot be greater than 60000
	timestamp 	        LONG 	YES
*/
func GetOCOOrder(id int64) (*models.PostOrderOCOResponse, error) {
	st, err := GetServerTime()
	if err != nil {
		return nil, err
	}

	q := fmt.Sprintf("orderListId=%d&recvWindow=%d&timestamp=%d", id, 5000, st)
	signedQuery, err := Sign([]byte(os.Getenv(apiSecret)), q)
	if err != nil {
		return nil, err
	}

	uri := BuildURI(apiEndpoint+"orderList?", q, "&signature=", signedQuery)
	resp, err := Query("GET", uri, "application/json", nil)
	if err != nil {
		return nil, err
	}

	if len(resp) == 0 {
		return nil, fmt.Errorf("empty response body")
	}

	order := new(models.PostOrderOCOResponse)
	err = json.Unmarshal(resp, order)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling OCO order: %w", err)
	}

	return order, nil
}

/*
Query Order (USER_DATA)

GET /api/v3/order

Check an order's status.

Weight(IP): 4

Parameters:

	Name 	            Type 	Mandatory 	Description
	symbol 	            STRING 	YES
	orderId 	        LONG 	NO
	origClientOrderId 	STRING 	NO
	recvWindow 	        LONG 	NO 	The value cannot be greater than 60000
	timestamp 	        LONG 	YES
*/
func GetOrder(symbol string, id int64) (*models.GetOrderResponse, error) {
	st, err := GetServerTime()
	if err != nil {
		return nil, err
	}

	q := fmt.Sprintf("symbol=%s&orderId=%d&recvWindow=%d&timestamp=%d", symbol, id, 5000, st)
	signedQuery, err := Sign([]byte(os.Getenv(apiSecret)), q)
	if err != nil {
		return nil, err
	}

	uri := BuildURI(apiEndpoint+"order?", q, "&signature=", signedQuery)
	resp, err := Query("GET", uri, "application/json", nil)
	if err != nil {
		return nil, err
	}

	if len(resp) == 0 {
		return nil, fmt.Errorf("empty response body")
	}

	order := new(models.GetOrderResponse)
	err = json.Unmarshal(resp, order)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling order: %w", err)
	}

	return order, nil
}

func FindOrder(order *models.GetOrderResponse) (*models.GetOrderResponse, error) {
	orders, err := GetAllOrders(order.Symbol)
	if err != nil {
		return nil, fmt.Errorf("error querying all orders: %w", err)
	}

	var (
		ord   *models.GetOrderResponse
		found bool
	)
	for _, o := range orders {
		if o.OrderID == order.OrderID {
			ord = o
			found = true
			break
		}
	}

	if !found {
		return nil, fmt.Errorf("order not found")
	}
	return ord, nil
}

/*
Cancel Order (TRADE)

DELETE /api/v3/order

Cancel an active order.

Weight(IP): 1
*/
func CancelOrder(symbol string, id int64) (*models.DeleteOrderResponse, error) {
	st, err := GetServerTime()
	if err != nil {
		return nil, err
	}

	q := fmt.Sprintf("symbol=%s&orderId=%d&recvWindow=%d&timestamp=%d", symbol, id, 5000, st)
	signedQuery, err := Sign([]byte(os.Getenv(apiSecret)), q)
	if err != nil {
		return nil, err
	}

	uri := BuildURI(apiEndpoint+"order?", q, "&signature=", signedQuery)
	resp, err := Query("DELETE", uri, "application/json", nil)
	if err != nil {
		return nil, err
	}

	order := new(models.DeleteOrderResponse)
	err = json.Unmarshal(resp, order)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling order: %w", err)
	}

	return order, nil
}
