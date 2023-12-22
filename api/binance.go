package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/adamdenes/gotrade/internal/logger"
	"github.com/adamdenes/gotrade/internal/models"
	"github.com/adamdenes/gotrade/internal/storage"
	"nhooyr.io/websocket"
)

const (
	wsEndpoint = "wss://testnet.binance.vision/stream?streams="
	apiKey     = "APCA_API_KEY_ID"
	apiSecret  = "APCA_API_SECRET_KEY"
)

type Binance struct {
	db          storage.Storage
	ws          *websocket.Conn
	ctx         context.Context
	debugLog    *log.Logger
	errorLog    *log.Logger
	dataChannel chan []byte
	closing     context.Context
	closeSignal context.CancelFunc
	subdata     *models.CandleSubsciption
}

func NewBinance(
	ctx context.Context,
	db storage.Storage,
	cs <-chan *models.CandleSubsciption,
) *Binance {
	closingCtx, cancelFunc := context.WithCancel(context.Background())
	b := &Binance{
		db:          db,
		ctx:         ctx,
		debugLog:    logger.Debug,
		errorLog:    logger.Error,
		dataChannel: make(chan []byte, 1),
		closing:     closingCtx,
		closeSignal: cancelFunc,
		subdata:     <-cs,
	}

	b.debugLog.Printf(
		"Starting binance WebSocket instance on: %v - request: %v\n",
		&b.ws,
		b.ctx.Value(RequestIDContextKey),
	)
	go b.handleSymbolSubscriptions()
	return b
}

func (b *Binance) close() {
	// Signal all goroutines to stop sending messages
	b.closeSignal()

	// Use a goroutine to wait for all goroutines to finish
	done := make(chan struct{})
	go func() {
		close(b.dataChannel)
		close(done)
	}()

	// Wait for goroutines to finish or timeout
	select {
	case <-done:
		b.debugLog.Println("Goroutine finished.")
	case <-time.After(2 * time.Second):
		b.errorLog.Println("Timeout waiting for goroutines to finish.")
	}

	// Close the WebSocket connection and check for errors
	b.ws.Close(websocket.StatusNormalClosure, "Closed by client")
}

func (b *Binance) subscribe(subdata *models.CandleSubsciption) error {
	header := make(http.Header)
	header.Add("APCA-API-KEY-ID", os.Getenv(apiKey))
	header.Add("APCA-API-SECRET-KEY", os.Getenv(apiSecret))

	endpoint := createWsEndpoint(subdata.Symbol, subdata.Interval)

	conn, _, err := websocket.Dial(b.ctx, endpoint, &websocket.DialOptions{
		HTTPHeader: header,
	})
	if err != nil {
		b.errorLog.Printf("dial: %v", err)
		return err
	}
	b.ws = conn

	go b.handleWsLoop()

	return nil
}

func (b *Binance) handleWsLoop() {
	done := make(chan struct{})
	defer close(done)

	b.debugLog.Printf("Websocket loop started on channel: %v\n", b.dataChannel)

	const maxReconnectAttempts = 10
	reconnectAttempts := 0
	for {
		select {
		case <-b.closing.Done():
			return // Stop the goroutine if the closing signal is received
		default:
			b.ws.SetReadLimit(65536)
			_, msg, err := b.ws.Read(b.ctx)
			if err != nil {
				b.errorLog.Printf("error: %v", err)

				// StatusCode(-1) -> just closing/switching to other stream
				if errors.Is(err, b.ctx.Err()) {
					b.debugLog.Println("Context cancelled successfully.", err)
					break
				}

				// Handle EOF error specifically
				if err.Error() == "failed to read frame header: EOF" ||
					errors.Is(err, net.ErrClosed) ||
					errors.Is(err, io.EOF) {
					reconnectAttempts++
				}

				if websocket.CloseStatus(err) != websocket.StatusNormalClosure {
					b.errorLog.Printf("WebSocket closed unexpectedly: %v", err)
					reconnectAttempts++
				}

				if reconnectAttempts >= maxReconnectAttempts {
					b.debugLog.Println("Max number of reconnections reached. Giving up.")
					b.close()
					return
				}
				b.debugLog.Printf(
					"WebSocket connection dropped, attempting to reconnect -> %d",
					reconnectAttempts,
				)
				// Reconnect
				if err := b.reconnect(b.subdata); err != nil {
					b.errorLog.Printf("Reconnection failed: %v", err)
					// Exponential backoff
					backoff := time.Second * time.Duration(
						math.Pow(2, float64(reconnectAttempts)),
					)
					b.debugLog.Printf("Backing off for duration -> %f", backoff.Seconds())
					time.Sleep(backoff)
					continue
				} else {
					// Reset counter when connection is successful
					reconnectAttempts = 0
				}

				// Any other type of error...
				continue
			}

			b.dataChannel <- msg
		}
	}
}

func (b *Binance) handleSymbolSubscriptions() {
	if b.ctx.Value(RequestIDContextKey) != nil {
		b.debugLog.Printf("Request/Context processing -> '%v'\n", b.ctx.Value(RequestIDContextKey))
	}

	if err := b.subscribe(b.subdata); err != nil {
		b.errorLog.Printf("subsciption error: %v\n", err)
		b.close()
	}

	// Gracefully close WS conn to Binance
	select {
	case <-b.ctx.Done():
		b.close()
	}
}

// Reconnects upon io.EOF error (dropped/reset connection)
func (b *Binance) reconnect(sd *models.CandleSubsciption) error {
	// Close and clean up the old instance
	b.close()

	strat, err := getStrategy(sd.Strategy, b.db)
	if err != nil {
		b.errorLog.Println(err)
		return err
	}
	// Re-establish the subscription using stored subdata
	x := connectExchange(b.ctx, b.db, sd.Symbol, sd.Interval, sd.Strategy)
	b.debugLog.Println("Reconnected and created a new Binance instance.")
	// Start data processing
	go processBars(sd.Symbol, sd.Interval, strat, x.dataChannel)

	return nil
}

func createWsEndpoint(symbol string, interval string) string {
	return fmt.Sprintf("%s%s@kline_%s", wsEndpoint, symbol, interval)
}
