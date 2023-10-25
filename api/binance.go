package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"

	"github.com/adamdenes/gotrade/internal/logger"
	"github.com/adamdenes/gotrade/internal/models"
	"nhooyr.io/websocket"
)

const (
	wsEndpoint = "wss://testnet.binance.vision/stream?streams="
	apiKey     = "APCA_API_KEY_ID"
	apiSecret  = "APCA_API_SECRET_KEY"
)

type Binance struct {
	ws          *websocket.Conn
	ctx         context.Context
	debugLog    *log.Logger
	errorLog    *log.Logger
	dataChannel chan []byte
}

func NewBinance(ctx context.Context, cs <-chan *models.CandleSubsciption) *Binance {
	b := &Binance{
		ctx:         ctx,
		debugLog:    logger.Debug,
		errorLog:    logger.Error,
		dataChannel: make(chan []byte, 1),
	}

	b.debugLog.Printf(
		"Starting binance WebSocket instance on: %v - request: %v\n",
		&b.ws,
		b.ctx.Value(RequestIDContextKey),
	)
	go b.handleSymbolSubscriptions(cs)
	return b
}

func (b *Binance) close() {
	b.debugLog.Printf("Closing data channel: %v\n", b.dataChannel)
	close(b.dataChannel)
	b.debugLog.Printf("Closing Binance WebSocket connection on: %v\n", &b.ws)
	b.ws.Close(websocket.StatusNormalClosure, "Closed by client")
	b.debugLog.Printf(
		"BINANCE connection closed successfully for request: %v\n",
		b.ctx.Value(RequestIDContextKey),
	)
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
	b.debugLog.Printf("Websocket loop started on channel: %v\n", b.dataChannel)
	for {
		b.ws.SetReadLimit(65536)
		_, msg, err := b.ws.Read(b.ctx)
		if err != nil {
			if errors.Is(err, b.ctx.Err()) {
				// StatusCode(-1) -> just closing/switching to other stream
				b.debugLog.Println("Context cancelled successfully.")
				b.close()
				break
			}
			if errors.Is(err, net.ErrClosed) {
				b.errorLog.Printf("Unexpected error: %v", err)
				break
			}
			if websocket.CloseStatus(err) != websocket.StatusNormalClosure {
				b.errorLog.Printf("WebSocket closed: %v", err)
				break
			}
			continue
		}
		b.dataChannel <- msg
	}
}

func (b *Binance) handleSymbolSubscriptions(cs <-chan *models.CandleSubsciption) {
	b.debugLog.Printf("Request/Context processing -> '%v'\n", b.ctx.Value(RequestIDContextKey))
	if err := b.subscribe(<-cs); err != nil {
		b.errorLog.Printf("subsciption error: %v\n", err)
		b.close()
	}
}

func createWsEndpoint(symbol string, interval string) string {
	return fmt.Sprintf("%s%s@kline_%s", wsEndpoint, symbol, interval)
}
