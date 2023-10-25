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
func Query(qs string) ([]byte, error) {
	resp, err := http.Get(qs)
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

	if resp.StatusCode != http.StatusOK {
		return nil, err
	}

	r, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return r, nil
}

func Post(url string, contType string, jsonBody []byte) ([]byte, error) {
	fmt.Println("post url: \n", url)
	fmt.Println(string(jsonBody))
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonBody))
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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Check the response status code for errors
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP Status: %s\nResponse Body: %s", resp.Status, body)
	}

	return body, nil
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
	resp, err := Query(uri)
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
	resp, err := Query(uri)
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

func NewSymbolCache() (map[string]struct{}, error) {
	uri := BuildURI(apiEndpoint + "exchangeInfo")
	resp, err := Query(uri)
	if err != nil {
		return nil, err
	}

	// Define a struct to unmarshal the JSON response
	var exchangeInfo struct {
		Symbols []struct {
			Symbol string `json:"symbol"`
		} `json:"symbols"`
	}

	// Parse the JSON response
	err = json.Unmarshal(resp, &exchangeInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	cache := make(map[string]struct{}, len(exchangeInfo.Symbols))
	for _, s := range exchangeInfo.Symbols {
		cache[s.Symbol] = struct{}{}
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
func TestOrder(order *models.Order) ([]byte, error) {
	signedQuery, err := Sign([]byte(os.Getenv(apiSecret)), order.String())
	if err != nil {
		return nil, err
	}

	uri := BuildURI(apiEndpoint + "order/test")
	jb := []byte(order.String() + "&signature=" + signedQuery)
	resp, err := Post(uri, "application/json", jb)
	if err != nil {
		return nil, err
	}

	return resp, nil
}
