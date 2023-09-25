package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/adamdenes/gotrade/internal/logger"
	"github.com/adamdenes/gotrade/internal/storage"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

var templateFiles = []string{
	"./web/templates/base.tmpl.html",
	"./web/templates/pages/chart.tmpl.html",
	"./web/templates/partials/script.tmpl.html",
	"./web/templates/partials/search_bar.tmpl.html",
	"./web/templates/partials/dropdown_tf.tmpl.html",
}

type Server struct {
	listenAddress string
	store         storage.Storage
	router        *http.ServeMux
	infoLog       *log.Logger
	errorLog      *log.Logger
}

func NewServer(addr string, db storage.Storage) *Server {
	return &Server{
		listenAddress: addr,
		store:         db,
		router:        &http.ServeMux{},
		infoLog:       logger.Info,
		errorLog:      logger.Error,
	}
}

func (s *Server) Run() {
	s.infoLog.Printf("Server listening on localhost%s\n", s.listenAddress)
	err := http.ListenAndServe(s.listenAddress, s.routes())

	if err != nil {
		s.errorLog.Fatalf("error listening on %s: %v", s.listenAddress, err)
	}
}

func (s *Server) routes() http.Handler {
	s.router = http.NewServeMux()

	fileServer := http.FileServer(http.Dir("./web/static/"))
	s.router.Handle("/static/", http.StripPrefix("/static", fileServer))

	s.router.HandleFunc("/", s.indexHandler)
	s.router.HandleFunc("/ws", s.websocketClientHandler)
	s.router.HandleFunc("/klines", s.klinesHandler)
	s.router.HandleFunc("/klines/live", s.liveKlinesHandler)

	// Chain middlewares here
	return s.recoverPanic(s.logRequest(s.secureHeader(s.tagRequest(s.router))))
}

func (s *Server) indexHandler(w http.ResponseWriter, r *http.Request) {
	// Send 404 if destination is not `/`
	if r.URL.Path != "/" {
		// http.Error(w, "404 Page not found", http.StatusNotFound)
		s.notFound(w)
		return
	}

	s.render(w, nil)
}

func (s *Server) websocketClientHandler(w http.ResponseWriter, r *http.Request) {
	// Get the form values for further processing
	// All symbols for streams are lowercase
	symbol := strings.ToLower(r.FormValue("symbol"))
	interval := r.FormValue("interval")

	// Handling websocket connection
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		s.serverError(w, err)
		return
	}

	// Construct `Binance` with a read-only channel and start processing incoming data
	// Make the context 'cancellable'
	ctx, cancel := context.WithCancel(r.Context())
	cs := &CandleSubsciption{symbol: symbol, interval: interval}
	b := NewBinance(ctx, inbound(cs))

	defer s.cleanUp(w, r, conn, cancel)
	// defer s.cleanUp(w, r, conn, cancel)
	go receiver[[]byte](r.Context(), b.dataChannel, conn)
}

func (s *Server) klinesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		s.clientError(w, http.StatusMethodNotAllowed)
		return
	}
	if r.URL.Path != "/klines" {
		s.notFound(w)
		return
	}

	// Binance endpoint (daily / montly)
	// https://data.binance.vision/?prefix=data/spot/monthly/klines/BTCUSDT/1s/

	// 1. get all symbols
	symbols, _ := getSymbols()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	json.NewEncoder(w).Encode(symbols)
}

func (s *Server) liveKlinesHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.clientError(w, http.StatusBadRequest)
		return
	}

	// Trading pair has to be all uppercase for REST API
	symbol := strings.ToUpper(r.FormValue("symbol"))
	interval := r.FormValue("interval")

	// GET request to binance
	getKlinesURL := fmt.Sprintf("https://api.binance.com/api/v3/uiKlines?symbol=%s&interval=%s", symbol, interval)
	resp, err := GET(getKlinesURL)
	if err != nil {
		s.serverError(w, err)
	}
	defer resp.Body.Close()

	// Check the HTTP status code
	if resp.StatusCode != http.StatusOK {
		s.serverError(w, fmt.Errorf("Binance API request failed with status code %d", resp.StatusCode))
		return
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		s.serverError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(respBody)
}

func (s *Server) cleanUp(w http.ResponseWriter, r *http.Request, conn *websocket.Conn, cancel context.CancelFunc) {
	for {
		msgType, msg, err := conn.Read(r.Context())
		if err != nil {
			switch websocket.CloseStatus(err) {
			case websocket.StatusNormalClosure: // Disconnect by client
				s.infoLog.Printf("WebSocket closed by client")
			case websocket.StatusNoStatusRcvd: // Switching stream
				s.infoLog.Printf("WebSocket switching stream")
			case websocket.StatusGoingAway: // Closing the tab
				s.infoLog.Printf("WebSocket going away")
			default:
				s.errorLog.Printf("WebSocket read error: %v - %v\n", err, websocket.CloseStatus(err))
			}
			return
		}

		if msgType == websocket.MessageText {
			// Process the incoming text message (closing message or other messages)
			message := string(msg)
			if message == "CLOSE" {
				// Handle the custom closing message from the client
				s.infoLog.Printf("Received closing message from client: %v\n", message)
				conn.Close(websocket.StatusNormalClosure, "Closed by client")
				return
			}
		}
	}
}

type CandleSubsciption struct {
	symbol   string
	interval string
}

func inbound[T any](in *T) <-chan *T {
	out := make(chan *T)
	go func() {
		out <- in
		close(out)
	}()
	return out
}

func receiver[T ~string | ~[]byte](ctx context.Context, in chan T, conn *websocket.Conn) {
	// Process data received from the data channel
	for data := range in {
		// Write the data to the WebSocket connection
		if err := wsjson.Write(ctx, conn, string(data)); err != nil {
			logger.Error.Printf("Error writing data to WebSocket: %v\n", err)
			continue
		}
	}
}
