package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/adamdenes/gotrade/internal/logger"
	"nhooyr.io/websocket"
)

const (
	wsEndpoint = "wss://stream.binance.com:9443/stream?streams="
	apiKey     = "APCA_API_KEY_ID"
	apiSecret  = "APCA_API_SECRET_KEY"
)

type Binance struct {
	ws          *websocket.Conn
	ctx         context.Context
	infoLog     *log.Logger
	errorLog    *log.Logger
	dataChannel chan []byte
}

func NewBinance(ctx context.Context, cs <-chan *CandleSubsciption) *Binance {
	b := &Binance{
		ctx:         ctx,
		infoLog:     logger.Info,
		errorLog:    logger.Error,
		dataChannel: make(chan []byte, 1),
	}

	b.infoLog.Printf("Starting binance WebSocket instance on: %v\n", &b.ws)
	go b.handleSymbolSubscriptions(cs)
	return b
}

func (b *Binance) close() {
	b.infoLog.Printf("Closing data channel: %v\n", b.dataChannel)
	close(b.dataChannel)
	b.infoLog.Printf("Closing Binance WebSocket connection on: %v\n", &b.ws)
	if err := b.ws.Close(websocket.StatusNormalClosure, "Closed by client"); err != nil {
		b.errorLog.Printf(err.Error())
	}
	b.infoLog.Printf("BINANCE connection closed successfully.")
}

func (b *Binance) subscribe(subdata *CandleSubsciption) error {
	header := make(http.Header)
	header.Add("APCA-API-KEY-ID", os.Getenv(apiKey))
	header.Add("APCA-API-SECRET-KEY", os.Getenv(apiSecret))

	endpoint := createWsEndpoint(subdata.symbol, subdata.interval)

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
	b.infoLog.Printf("Websocket loop started on channel: %v\n", b.dataChannel)
	for {
		b.ws.SetReadLimit(65536)
		_, msg, err := b.ws.Read(b.ctx)
		if err != nil {
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

func (b *Binance) handleSymbolSubscriptions(cs <-chan *CandleSubsciption) {
	if err := b.subscribe(<-cs); err != nil {
		b.errorLog.Printf("subsciption error: %v\n", err)
	}
}

func createWsEndpoint(symbol string, interval string) string {
	return fmt.Sprintf("%s%s@kline_%s", wsEndpoint, symbol, interval)
}

func splitStream(stream string) (string, string) {
	parts := strings.Split(stream, "@")
	return parts[0], parts[1]
}
