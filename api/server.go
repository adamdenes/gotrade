package api

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/adamdenes/gotrade/internal/backtest"
	"github.com/adamdenes/gotrade/internal/logger"
	"github.com/adamdenes/gotrade/internal/models"
	"github.com/adamdenes/gotrade/internal/storage"
	"github.com/adamdenes/gotrade/strategy"
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
	symbolCache   map[string]struct{}
}

func NewServer(
	addr string,
	db storage.Storage,
	templates map[string]*template.Template,
) *Server {
	return &Server{
		listenAddress: addr,
		store:         db,
		router:        &http.ServeMux{},
		infoLog:       logger.Info,
		errorLog:      logger.Error,
		templateCache: templates,
		symbolCache:   make(map[string]struct{}, 1),
	}
}

func (s *Server) Run() {
	go s.updateSymbolCache()

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
	return s.recoverPanic(s.logRequest(s.secureHeader(s.tagRequest(s.gzipMiddleware(s.router)))))
}

func (s *Server) indexHandler(w http.ResponseWriter, r *http.Request) {
	// Send 404 if destination is not `/`
	if r.URL.Path != "/" {
		// http.Error(w, "404 Page not found", http.StatusNotFound)
		s.notFound(w)
		return
	}

	s.render(w, http.StatusOK, "home.tmpl.html", nil)
}

func (s *Server) websocketClientHandler(w http.ResponseWriter, r *http.Request) {
	symbol := strings.ToLower(r.FormValue("symbol"))
	interval := r.FormValue("interval")
	local, _ := strconv.ParseBool(r.FormValue("local"))
	strat := r.FormValue("strategy")

	// Handling websocket connection
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		s.serverError(w, err)
		return
	}

	// Make the context 'cancellable'
	ctx, cancel := context.WithCancel(r.Context())
	if !local {
		// Construct `Binance` with a read-only channel and start processing incoming data
		cs := &models.CandleSubsciption{Symbol: symbol, Interval: interval}
		b := NewBinance(ctx, inbound(cs))

		defer s.cleanUp(w, r, conn, cancel)
		go receiver[[]byte](r.Context(), b.dataChannel, conn)
	} else {
		defer s.cleanUp(w, r, conn, cancel)

		strategies := map[string]backtest.Strategy[any]{
			"sma": strategy.NewSMAStrategy(12, 24),
			// Add more strategies as needed
		}
		selectedStrategy, found := strategies[strat]
		if !found {
			s.clientError(w, http.StatusBadRequest)
			return
		}

		// Execute backtest
		dataChan := make(chan *models.Order, 1)
		errChan := make(chan error)
		go s.backtest(ctx, conn, selectedStrategy, dataChan, errChan)

		for {
			select {
			case d, ok := <-dataChan:
				if !ok {
					s.infoLog.Println("Data Channel is closed.")
					return
				}
				if err := wsjson.Write(ctx, conn, d); err != nil {
					logger.Error.Printf("Error writing data to WebSocket: %v\n", err)
					continue
				}
			case err := <-errChan:
				s.errorLog.Println(err)
				s.serverError(w, err)
				break
			}
		}
	}
}

func (s *Server) fetchDataHandler(w http.ResponseWriter, r *http.Request) {
	t := time.Now()

	if r.Method != http.MethodPost {
		s.clientError(w, http.StatusMethodNotAllowed)
		return
	}

	val := time.Now()
	kr, err := s.validateKlineRequest(r)
	if err != nil {
		s.errorLog.Println(err)
		s.serverError(w, err)
		return
	}

	startDate, endDate, err := s.getDateRange(kr)
	if err != nil {
		s.errorLog.Println(err)
		s.serverError(w, err)
		return
	}
	s.infoLog.Printf("Request validation took: %v\n", time.Since(val))

	select {
	case <-r.Context().Done():
		s.errorLog.Println("Client disconnected early.")
		return
	default:
		ohlc, err := s.store.FetchData(
			r.Context(),
			kr.Interval,
			kr.Symbol,
			startDate.UnixMilli(),
			endDate.UnixMilli(),
		)
		if err != nil {
			s.serverError(w, err)
			return
		}

		s.writeResponse(w, ohlc)
		s.infoLog.Printf("Elapsed time: %v\n", time.Since(t))
	}
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
		kr, err := s.validateKlineRequest(r)
		if err != nil {
			s.errorLog.Println(err)
			s.serverError(w, err)
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

			s.writeResponse(w, resp)
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

func receiver[T ~string | ~[]byte](
	ctx context.Context,
	in chan T,
	conn *websocket.Conn,
) {
	// Process data received from the data channel
	for data := range in {
		// Write the data to the WebSocket connection
		if err := wsjson.Write(ctx, conn, string(data)); err != nil {
			logger.Error.Printf("Error writing data to WebSocket: %v\n", err)
			continue
		}
	}
}

func (s *Server) validateKlineRequest(r *http.Request) (*models.KlineRequest, error) {
	kr := &models.KlineRequest{}
	if err := json.NewDecoder(r.Body).Decode(&kr); err != nil {
		return nil, err
	}
	if err := ValidateSymbol(kr.Symbol, s.symbolCache); err != nil {
		return nil, err
	}
	return kr, nil
}

func (s *Server) getDateRange(kr *models.KlineRequest) (time.Time, time.Time, error) {
	timeSlice, err := ValidateTimes(kr.OpenTime, kr.CloseTime)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	startDate := timeSlice[0]
	endDate := timeSlice[1].Add(24 * time.Hour)
	return startDate, endDate, nil
}

func (s *Server) writeResponse(w http.ResponseWriter, batch any) {
	if err := WriteJSON(w, http.StatusOK, batch); err != nil {
		s.errorLog.Println("Failed to write response:", err)
	}
}

func (s *Server) backtest(
	ctx context.Context,
	conn *websocket.Conn,
	strat backtest.Strategy[any],
	dataChan chan *models.Order,
	errChan chan error,
) {
	engine := backtest.NewBacktestEngine(100000, nil, strat)
	engine.DataChannel = dataChan
	engine.Init()

	var wg sync.WaitGroup

	wg.Add(1)
	go func(wg *sync.WaitGroup) {
		defer wg.Done()
		for {
			bar := []*models.KlineSimple{}
			_, msg, err := conn.Read(ctx)
			if err != nil {
				errChan <- err
				return
			}

			err = json.Unmarshal(msg, &bar)
			if err != nil {
				errChan <- err
				return
			}
			engine.SetData(bar)
			engine.Run()
		}
	}(&wg)
	wg.Wait()

	close(dataChan)
	close(errChan)
}

func (s *Server) updateSymbolCache() {
	// Attempt to fetch Symbols from database
	rows, err := s.store.(*storage.TimescaleDB).GetSymbols()
	if err != nil {
		s.errorLog.Fatalf("error fetching symbols from db: %v", err)
	} else {
		defer rows.Close()

		// Update the cache with the symbols from the database
		var asset string
		for rows.Next() {
			if err := rows.Scan(&asset); err != nil {
				s.errorLog.Fatalf("error updating symbols: %v", err)
			}
			if c, ok := s.symbolCache[asset]; !ok {
				s.symbolCache[asset] = c
			}
		}

		if err := rows.Err(); err != nil {
			s.errorLog.Fatal(err)
		}

		if len(s.symbolCache) == 0 {
			// rows.Next() == false -> database was probably empty
			s.infoLog.Println("creating new symbol cache")
			sc, err := NewSymbolCache()
			if err != nil {
				logger.Error.Fatal(err)
			}
			s.symbolCache = sc
			if err := s.store.SaveSymbols(s.symbolCache); err != nil {
				s.errorLog.Fatalf("error saving symbols: %v", err)
			}
		}
		s.infoLog.Println("Symbols saved to DB.")
	}
}

func (s *Server) render(w http.ResponseWriter, status int, page string, data any) {
	ts, ok := s.templateCache[page]
	if !ok {
		s.serverError(w, fmt.Errorf("the template '%s' does not exists", page))
		return
	}

	w.WriteHeader(status)
	// Render the template
	err := ts.ExecuteTemplate(w, "base", data)
	if err != nil {
		s.serverError(w, err)
	}
}

func (s *Server) serverError(w http.ResponseWriter, err error) {
	trace := fmt.Sprintf("%s\n%s", err.Error(), debug.Stack())
	s.errorLog.Output(2, trace)

	http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
}

func (s *Server) clientError(w http.ResponseWriter, status int) {
	http.Error(w, http.StatusText(status), status)
}

func (s *Server) notFound(w http.ResponseWriter) {
	s.clientError(w, http.StatusNotFound)
}
