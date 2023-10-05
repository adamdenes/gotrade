package api

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strings"

	"github.com/adamdenes/gotrade/internal/logger"
	"github.com/adamdenes/gotrade/internal/models"
	"github.com/adamdenes/gotrade/internal/storage"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

type Server struct {
	listenAddress string
	store         storage.Storage
	router        *http.ServeMux
	infoLog       *log.Logger
	errorLog      *log.Logger
	templateCache map[string]*template.Template
}

func NewServer(addr string, db storage.Storage, cache map[string]*template.Template) *Server {
	return &Server{
		listenAddress: addr,
		store:         db,
		router:        &http.ServeMux{},
		infoLog:       logger.Info,
		errorLog:      logger.Error,
		templateCache: cache,
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
	s.router.HandleFunc("/backtest", s.klinesHandler)
	s.router.HandleFunc("/klines/live", s.liveKlinesHandler)
	s.router.HandleFunc("/fetch-data", s.fetchDataHandler)

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

	s.render(w, http.StatusOK, "chart.tmpl.html", nil)
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
	cs := &models.CandleSubsciption{Symbol: symbol, Interval: interval}
	b := NewBinance(ctx, inbound(cs))

	defer s.cleanUp(w, r, conn, cancel)
	go receiver[[]byte](r.Context(), b.dataChannel, conn)
}

func (s *Server) fetchDataHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.clientError(w, http.StatusMethodNotAllowed)
		return
	}
	kr := &models.KlineRequest{}
	if err := json.NewDecoder(r.Body).Decode(&kr); err != nil {
		s.errorLog.Println(err)
		s.clientError(w, http.StatusBadRequest)
		return
	}
	// Validate symbol
	if err := ValidateSymbol(kr.Symbol); err != nil {
		s.errorLog.Println(err)
		s.clientError(w, http.StatusBadRequest)
		return
	}

	timeSlice, err := ValidateTimes(
		fmt.Sprintf("%d", kr.OpenTime),
		fmt.Sprintf("%d", kr.CloseTime),
	)

	// Check if client is still connected.
	select {
	case <-r.Context().Done():
		s.errorLog.Println("Client disconnected early.")
		return
	default:
		if err != nil {
			s.errorLog.Println(err)
			s.clientError(w, http.StatusBadRequest)
		}
		resp, err := s.fetchData(kr.Symbol, timeSlice[0].UnixMilli(), timeSlice[1].UnixMilli())
		if err != nil {
			s.serverError(w, err)
			return
		}
		if err := WriteJSON(w, http.StatusOK, resp); err != nil {
			s.serverError(w, err)
			return
		}
	}
	// Sending chunked data to avoid transferring large files
	// w.Header().Set("Transfer-Encoding", "chunked")

	// startChunk := timeSlice[0].UnixMilli()
	// endChunk := timeSlice[1].UnixMilli()
	//
	// next := func() ([]*models.Kline, error) {
	// 	nextChunk := startChunk + 860000
	//
	// 	if endChunk-startChunk <= 860000 {
	// 		s.infoLog.Println("changing next to end")
	// 		nextChunk = endChunk
	// 	}
	// 	s.infoLog.Printf("start=%v, next=%v, end=%v", startChunk, nextChunk, endChunk)
	// 	chunk, err := s.store.FetchData(
	// 		kr.Symbol,
	// 		startChunk,
	// 		nextChunk,
	// 	)
	// 	if err != nil {
	// 		return nil, err
	// 	}
	// 	startChunk = nextChunk
	//
	// 	return chunk, nil
	// }
	//
	// for {
	// 	klines, err := next()
	// 	if err != nil {
	// 		s.serverError(w, err)
	// 		break
	// 	}
	// 	if len(klines) == 0 {
	// 		s.errorLog.Println("No more chunks to be received..., endtime was ->", endChunk)
	// 		break
	// 	}
	// 	if err := WriteJSON(w, http.StatusOK, klines); err != nil {
	// 		s.serverError(w, err)
	// 	}
	// }
}

func (s *Server) klinesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.clientError(w, http.StatusMethodNotAllowed)
		return
	}

	s.render(w, http.StatusOK, "backtest.tmpl.html", nil)
}

func (s *Server) liveKlinesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		s.render(w, http.StatusOK, "chart.tmpl.html", nil)
	} else if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			s.clientError(w, http.StatusBadRequest)
			return
		}

		kr := &models.KlineRequest{}
		err := json.NewDecoder(r.Body).Decode(kr)
		if err != nil {
			s.serverError(w, err)
			return
		}
		if err := ValidateSymbol(kr.Symbol); err != nil {
			s.errorLog.Println(err)
			s.clientError(w, http.StatusBadRequest)
			return
		}
		// Check if client is still connected.
		select {
		case <-r.Context().Done():
			s.errorLog.Println("Client disconnected early.")
			return
		default:
			// Trading pair has to be all uppercase for REST API
			// GET request to binance
			resp, err := getUiKlines(kr.String())
			if err != nil {
				s.serverError(w, err)
				return
			}

			if err := WriteJSON(w, http.StatusOK, resp); err != nil {
				s.serverError(w, err)
				return
			}
		}
	} else {
		s.clientError(w, http.StatusMethodNotAllowed)
		return
	}
}

func (s *Server) cleanUp(
	w http.ResponseWriter,
	r *http.Request,
	conn *websocket.Conn,
	cancel context.CancelFunc,
) {
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
				s.errorLog.Printf(
					"WebSocket read error: %v - %v\n",
					err,
					websocket.CloseStatus(err),
				)
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

func (s *Server) fetchData(symbol string, start int64, end int64) ([][]interface{}, error) {
	klines, err := s.store.FetchData(symbol, start, end)
	if err != nil {
		return nil, err
	}

	var result [][]interface{}
	for _, k := range klines {
		item := []interface{}{
			k.OpenTime,
			k.Open,
			k.High,
			k.Low,
			k.Close,
			k.Volume,
			k.CloseTime,
			k.QuoteAssetVolume,
			k.NumberOfTrades,
			k.TakerBuyBaseAssetVol,
			k.TakerBuyQuoteAssetVol,
		}
		result = append(result, item)
	}
	return result, nil
}

func clientStillConnected(r *http.Request) bool {
	select {
	case <-r.Context().Done():
		return false
	default:
		return true
	}
}
