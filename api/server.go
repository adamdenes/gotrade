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

	"github.com/adamdenes/gotrade/cmd/rest"
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
	botContexts   map[int]context.CancelFunc
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
		botContexts:   make(map[int]context.CancelFunc),
	}
}

func (s *Server) Run() {
	go s.updateSymbolCache()
	go s.monitorTrades()

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
	s.router.HandleFunc("/start-bot", s.startBotHandler)
	s.router.HandleFunc("/delete-bot", s.deleteBotHandler)

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

	// Load running bots here on index page even if user navigates out
	bots, err := s.store.GetBots()
	if err != nil {
		s.errorLog.Println(err)
		s.serverError(w, err)
		return
	}

	data := struct {
		Bots []*models.TradingBot
	}{
		Bots: bots,
	}

	s.render(w, http.StatusOK, "home.tmpl.html", &data)
}

func (s *Server) startBotHandler(w http.ResponseWriter, r *http.Request) {
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

	bot := &models.TradingBot{
		Symbol:    kr.Symbol,
		Interval:  kr.Interval,
		Status:    models.ACTIVE,
		Strategy:  kr.Strat,
		CreatedAt: time.Now(),
	}
	if err := s.store.CreateBot(bot); err != nil {
		s.errorLog.Println(err)
		s.serverError(w, err)
		return
	}

	// Need to get the ID from database to render bot-card properly
	bot, err = s.store.GetBot(bot.Symbol, bot.Strategy)
	if err != nil {
		s.errorLog.Println(err)
		s.serverError(w, err)
		return
	}

	// Update bot context map
	ctx, cancel := context.WithCancel(context.Background())
	s.botContexts[bot.ID] = cancel

	// Get data from exchange
	cs := &models.CandleSubsciption{Symbol: strings.ToLower(kr.Symbol), Interval: kr.Interval}
	b := NewBinance(ctx, inbound(cs))

	// Select strategy
	strategies := map[string]backtest.Strategy[any]{
		"sma":  strategy.NewSMAStrategy(12, 24, 5, s.store),
		"macd": strategy.NewMACDStrategy(5, s.store),
	}
	selectedStrategy, found := strategies[kr.Strat]
	if !found {
		s.clientError(w, http.StatusBadRequest)
		return
	}

	go s.processBars(kr, selectedStrategy, b.dataChannel)

	select {
	case <-r.Context().Done():
		s.errorLog.Println("Client disconnected early.")
		return
	default:
		s.writeResponse(w, bot)
	}
}

func (s *Server) deleteBotHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	botID := r.URL.Query().Get("id")
	if botID == "" {
		s.errorLog.Panicln("Bot ID is required for deletion.")
		s.clientError(w, http.StatusBadRequest)
		return
	}

	id, err := strconv.Atoi(botID)
	if err != nil {
		s.errorLog.Panicln("Invalid bot ID")
		s.clientError(w, http.StatusBadRequest)
		return
	}

	err = s.store.DeleteBot(id)
	if err != nil {
		s.errorLog.Println(err)
		http.Error(w, "Failed to delete bot", http.StatusInternalServerError)
		return
	}

	// Cancel the WebSocket connection context
	if cancel, ok := s.botContexts[id]; ok {
		cancel()
		delete(s.botContexts, id)
	}

	s.writeResponse(w, "ok")
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
			"sma":  strategy.NewSMAStrategy(12, 24, 5, s.store),
			"macd": strategy.NewMACDStrategy(5, s.store),
		}
		selectedStrategy, found := strategies[strat]
		if !found {
			s.clientError(w, http.StatusBadRequest)
			return
		}

		// Execute backtest
		dataChan := make(chan models.TypeOfOrder, 1)
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
			resp, err := rest.GetUiKlines(kr.String())
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
	dataChan chan models.TypeOfOrder,
	errChan chan error,
) {
	engine := backtest.NewBacktestEngine(1000, nil, strat)
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
			sc, err := rest.NewSymbolCache()
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

func (s *Server) monitorTrades() {
	var id int64
	var isOCO bool

	for {
		trades, err := s.store.FetchTrades()
		if err != nil {
			s.errorLog.Printf("error fetching trades from db: %v", err)
			return
		}

		if len(trades) == 0 {
			s.infoLog.Println("No trades found, going to sleep...")
			time.Sleep(60 * time.Second)
			continue // Check again after sleep
		}

		for _, trade := range trades {
			// Skip already finished trades
			if trade.Status == "FILLED" || trade.Status == "ALL_DONE" {
				continue
			}

			if trade.OrderID != 0 {
				id = trade.OrderID
				isOCO = false
			} else {
				id = trade.OrderListID
				isOCO = true
			}

			go s.monitorTrade(trade.Symbol, id, isOCO)
		}

		s.infoLog.Println("monitorTrades() going to sleep for 60 seconds.")
		time.Sleep(60 * time.Second)
	}
}

func (s *Server) monitorTrade(symbol string, orderID int64, isOCOorder bool) {
	var err error
	for {
		var o interface{}
		if isOCOorder {
			o, err = rest.GetOCOOrder(orderID)
			if err != nil {
				s.errorLog.Printf("error getting OCO order: %v", err)
				return
			}
		} else {
			o, err = rest.GetOrder(symbol, orderID)
			if err != nil {
				s.errorLog.Printf("error getting order: %v", err)
				return
			}
		}

		// Process the order
		switch order := o.(type) {
		case *models.OrderResponse:
			if order.Status == "FILLED" {
				s.infoLog.Printf("Order %s! Updating Database...", order.Status)

				err = s.store.UpdateTrade(order.OrderID, string(order.Status))
				if err != nil {
					s.errorLog.Printf("error updating trade: %v", err)
				}
				// done updating, stop monitoring
				return
			}
		case *models.OrderOCOResponse:
			if order.ListOrderStatus == "ALL_DONE" {
				s.infoLog.Printf("OCO Order %s! Updating Database...", order.ListOrderStatus)

				err = s.store.UpdateTrade(order.OrderListID, string(order.ListOrderStatus))
				if err != nil {
					s.errorLog.Printf("error updating OCO trade: %v", err)
				}
				// done updating, stop monitoring
				return
			}
		}

		time.Sleep(10 * time.Second)
	}
}

func (s *Server) processBars(
	kr *models.KlineRequest,
	strategy backtest.Strategy[any],
	dc chan []byte,
) {
	// Prefetch data here for talib calculations
	bars, err := convertKlines(kr.Symbol, kr.Interval)
	strategy.SetData(bars)
	strategy.SetAsset(kr.Symbol)

	for data := range dc {
		// Unmarshal bar data
		var (
			bar = &models.KlineSimple{}
			kws = &models.KlineWebSocket{}
		)
		err = json.Unmarshal(data, &kws)
		if err != nil {
			s.errorLog.Println(err)
			return
		}

		// Only evaluate closed bars
		if kws.Data.Kline.IsKlineClosed {
			s.infoLog.Println(kws)
			bar.OpenTime = time.UnixMilli(kws.Data.Kline.StartTime)
			bar.Open, _ = strconv.ParseFloat(kws.Data.Kline.OpenPrice, 64)
			bar.High, _ = strconv.ParseFloat(kws.Data.Kline.HighPrice, 64)
			bar.Low, _ = strconv.ParseFloat(kws.Data.Kline.LowPrice, 64)
			bar.Close, _ = strconv.ParseFloat(kws.Data.Kline.ClosePrice, 64)
			bars = append(bars, bar)

			strategy.SetData(bars)
			strategy.Execute()
		}
	}
}

func convertKlines(symbol, interval string) ([]*models.KlineSimple, error) {
	bars := []*models.KlineSimple{}

	// Prefetch data here for talib calculations?
	resp, err := rest.GetKlines(
		fmt.Sprintf("symbol=%s&interval=%s", strings.ToUpper(symbol), interval),
	)
	if err != nil {
		return nil, fmt.Errorf("error getting klines: %w", err)
	}

	var klines [][]interface{}
	err = json.Unmarshal(resp, &klines)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling klines: %w", err)
	}

	for _, k := range klines {
		open, _ := strconv.ParseFloat(k[1].(string), 64)
		high, _ := strconv.ParseFloat(k[2].(string), 64)
		low, _ := strconv.ParseFloat(k[3].(string), 64)
		cloze, _ := strconv.ParseFloat(k[4].(string), 64)
		volume, _ := strconv.ParseFloat(k[5].(string), 64)
		b := &models.KlineSimple{
			OpenTime: time.UnixMilli(int64(k[0].(float64))),
			Open:     open,
			High:     high,
			Low:      low,
			Close:    cloze,
			Volume:   volume,
		}
		bars = append(bars, b)
	}
	return bars, nil
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

func WriteJSON(w http.ResponseWriter, status int, v any) error {
	// []byte slices are not converting correctly, so I need to type switch
	switch data := v.(type) {
	case []byte:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, err := w.Write(data)
		return err
	default:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		return json.NewEncoder(w).Encode(data)
	}
}
