package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"

	"github.com/adamdenes/gotrade/internal/logger"
	"nhooyr.io/websocket"
)

const wsEndpoint = "wss://stream.binance.com/stream?streams="
const apiKey = "6r3PLGC5RcRnHIlMkAej55otVT9YHPPkXKCB4z2dUIDx698MUVj1IvOcQPBnEFns"
const apiSecret = "xVdliUOTP48qj0gQj96MKJ6F8Vmf4urj2vVEXSwlODINMxDXA8tXXBzf307Qt3q2"

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

	go b.handleSymbolSubscriptions(cs)
	return b
}

func (b *Binance) close() {
	defer close(b.dataChannel)
	b.ws.Close(websocket.StatusAbnormalClosure, "Closed by client")
}

func (b *Binance) subscribe(subdata *CandleSubsciption) error {
	header := make(http.Header)
	header.Add("APCA-API-KEY-ID", apiKey)
	header.Add("APCA-API-SECRET-KEY", apiSecret)

	endpoint := createWsEndpoint(subdata.symbol, subdata.timeFrame)

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
	for {
		b.ws.SetReadLimit(65536)
		_, msg, err := b.ws.Read(b.ctx)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				break
			}
			b.errorLog.Println(err)
			continue
		}
		// parser := fastjson.Parser{}
		// v, err := parser.ParseBytes(msg)
		// if err != nil {
		// 	b.errorLog.Println(err)
		// 	continue
		// }
		b.dataChannel <- msg
	}
}

func (b *Binance) handleSymbolSubscriptions(cs <-chan *CandleSubsciption) {
	out := make(chan string)
	go func() {
		select {
		case sub := <-cs:
			err := b.subscribe(sub)
			if err != nil {
				b.errorLog.Printf("subsciption error: %v\n", err)
			}
		case <-b.ctx.Done():
			// Context canceled
			b.close()
			return
		}
		close(out)
	}()
}

func createWsEndpoint(symbol string, timeFrame string) string {
	return fmt.Sprintf("%s%s@kline_%s", wsEndpoint, symbol, timeFrame)
}

func splitStream(stream string) (string, string) {
	parts := strings.Split(stream, "@")
	return parts[0], parts[1]
}
