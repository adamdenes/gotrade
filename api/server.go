package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"

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

	s.render(w, http.StatusOK, "home.tmpl.html", nil)
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

	var wg sync.WaitGroup
	dataCh := make(chan []any, 31) // Assuming 31 days max
	errCh := make(chan error)

	t := time.Now()
	for d := startDate; d.Before(endDate); d = d.Add(24 * time.Hour) {
		nextDate := d.Add(24 * time.Hour)

		if endDate.Compare(nextDate) == 0 {
			break
		}

		wg.Add(1)
		go s.fetchData(
			r.Context(),
			kr.Symbol,
			d.UnixMilli(),
			nextDate.UnixMilli(),
			dataCh,
			errCh,
			&wg,
		)
	}

	go func() {
		wg.Wait()
		close(dataCh)
		close(errCh)
	}()

	batch, err := s.processChannels(dataCh, errCh)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			s.infoLog.Println(err)
			return
		} else {
			s.errorLog.Println(err)
			s.serverError(w, err)
			return
		}
	}

	// TODO: use context to stop downstream operations
	select {
	case <-r.Context().Done():
		s.errorLog.Println("Client disconnected early.")
		return
	default:
		s.writeResponse(w, batch)
		s.infoLog.Printf("Elapsed time: %v\n", time.Since(t))
	}

	// Check if client is still connected.
	// select {
	// case <-r.Context().Done():
	// 	s.errorLog.Println("Client disconnected early.")
	// 	return
	// default:
	// 	resp, err := s.fetchData2(kr.Symbol, timeSlice[0].UnixMilli(), timeSlice[1].UnixMilli())
	// 	if err != nil {
	// 		s.serverError(w, err)
	// 		return
	// 	}
	//  s.writeResponse(w, resp)
	// 	s.infoLog.Printf("Elapsed time: %v\n", time.Since(t))
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

func (s *Server) fetchData(
	ctx context.Context,
	symbol string,
	start int64,
	end int64,
	dataCh chan []any,
	errCh chan error,
	wg *sync.WaitGroup,
) {
	defer wg.Done()
	klines, err := s.store.FetchData(ctx, symbol, start, end)
	if err != nil {
		errCh <- err
	}

	for _, k := range klines {
		item := []any{
			k.OpenTime,
			k.Open,
			k.High,
			k.Low,
			k.Close,
			k.CloseTime,
		}
		dataCh <- item
	}
}

// func (s *Server) fetchData2(symbol string, start int64, end int64) ([][]interface{}, error) {
// 	klines, err := s.store.FetchData(symbol, start, end)
// 	if err != nil {
// 		return nil, err
// 	}
//
// 	var result [][]interface{}
// 	for _, k := range klines {
// 		item := []interface{}{
// 			k.OpenTime,
// 			k.Open,
// 			k.High,
// 			k.Low,
// 			k.Close,
// 			k.Volume,
// 			k.CloseTime,
// 			k.QuoteAssetVolume,
// 			k.NumberOfTrades,
// 			k.TakerBuyBaseAssetVol,
// 			k.TakerBuyQuoteAssetVol,
// 		}
// 		result = append(result, item)
// 	}
// 	return result, nil
// }

func (s *Server) validateKlineRequest(r *http.Request) (*models.KlineRequest, error) {
	kr := &models.KlineRequest{}
	if err := json.NewDecoder(r.Body).Decode(&kr); err != nil {
		return nil, err
	}
	// Validate symbol
	if err := ValidateSymbol(kr.Symbol); err != nil {
		return nil, err
	}
	return kr, nil
}

func (s *Server) getDateRange(kr *models.KlineRequest) (time.Time, time.Time, error) {
	timeSlice, err := ValidateTimes(
		fmt.Sprintf("%d", kr.OpenTime),
		fmt.Sprintf("%d", kr.CloseTime),
	)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	startDate := timeSlice[0]
	endDate := timeSlice[1].Add(24 * time.Hour)
	return startDate, endDate, nil
}

func (s *Server) processChannels(dataCh chan []any, errCh chan error) ([][]any, error) {
	var batch [][]any
	var anyError error
	// If both channels closed, exit the loop
	for dataCh != nil || errCh != nil {
		select {
		case data, ok := <-dataCh:
			// Check if dataCh is closed
			if !ok {
				dataCh = nil
				continue
			}
			batch = append(batch, data)
		case err, ok := <-errCh:
			// Check if errCh is closed
			if !ok {
				errCh = nil
				continue
			}
			anyError = err
		}
	}
	if anyError != nil {
		return nil, anyError
	}
	sort.Slice(batch, func(i, j int) bool {
		return batch[i][0].(int64) < batch[j][0].(int64)
	})
	return batch, nil
}

func (s *Server) writeResponse(w http.ResponseWriter, batch any) {
	if err := WriteJSON(w, http.StatusOK, batch); err != nil {
		s.errorLog.Println("Failed to write response:", err)
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
