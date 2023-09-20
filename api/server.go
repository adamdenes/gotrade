package api

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/adamdenes/gotrade/internal/logger"
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
	router        *http.ServeMux
	infoLog       *log.Logger
	errorLog      *log.Logger
}

func NewServer(addr string) *Server {
	return &Server{
		listenAddress: addr,
		router:        &http.ServeMux{},
		infoLog:       logger.Info,
		errorLog:      logger.Error,
	}
}

func (s *Server) Run() {
	s.routes()

	s.infoLog.Printf("Server listening on localhost%s\n", s.listenAddress)
	err := http.ListenAndServe(s.listenAddress, s.router)

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

	return s.router
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
	// Handling the query/search part here
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		// http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		s.clientError(w, http.StatusMethodNotAllowed)
		return
	}

	// Get the form values for further processing
	symbol := r.FormValue("symbol")
	timeFrame := r.FormValue("timeframe")

	// Explanation why we do this (RFC 6455 - Sec-Websocket-Key)
	// https://dev.to/hgsgtk/how-decided-a-value-set-in-sec-websocket-keyaccept-header-l79
	// In short, the client is trying to connect to WS server, but not sending the correct
	// Request Headers and fields...
	r.Header.Set("Connection", "keep-alive, Upgrade")
	r.Header.Set("Upgrade", "websocket")
	r.Header.Set("Sec-Websocket-Version", "13")
	r.Header.Set("Sec-Websocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")

	// Trying to subscribe to the stream
	// Construct `Binance` with a read-only channel and start processing incoming data
	cs := &CandleSubsciption{symbol: symbol, timeFrame: timeFrame}
	b := NewBinance(context.Background(), inbound(cs))
	go receiver(b.dataChannel)

	/* ----------------------------------------------------------- */

	// Handling websocket connection
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		s.serverError(w, err)
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "something happened...")

	for {
		// To send data to the client, you can use wsjson.Write
		// Replace `response` with the data you want to send
		response := string([]byte("Hello, client!")) // Example data

		err := wsjson.Write(r.Context(), conn, response)
		if err != nil {
			s.serverError(w, err)
			return
		}
	}
}

type CandleSubsciption struct {
	symbol    string
	timeFrame string
}

func inbound[T any](in *T) <-chan *T {
	out := make(chan *T)
	go func() {
		out <- in
		close(out)
	}()
	return out
}

func receiver[T any](out chan T) {
	// Process data received from the data channel
	for data := range out {
		fmt.Println("RECEIVED", data)
	}
}
