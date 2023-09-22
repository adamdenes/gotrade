package api

import (
	"context"
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
	store         Storage
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
	s.router.HandleFunc("/klines", s.historyHandler)

	return s.router
}

func (s *Server) indexHandler(w http.ResponseWriter, r *http.Request) {
	s.infoLog.Printf("%v %v: %v\n", r.Proto, r.Method, r.URL)
	// Send 404 if destination is not `/`
	if r.URL.Path != "/" {
		// http.Error(w, "404 Page not found", http.StatusNotFound)
		s.notFound(w)
		return
	}

	s.render(w, nil)
}

func (s *Server) websocketClientHandler(w http.ResponseWriter, r *http.Request) {
	s.infoLog.Printf("%v %v: %v\n", r.Proto, r.Method, r.URL)
	// Handling the query/search part here
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		// http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		s.clientError(w, http.StatusMethodNotAllowed)
		return
	}

	// Get the form values for further processing
	symbol := r.FormValue("symbol")
	interval := r.FormValue("interval")

	// Handling websocket connection
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		s.serverError(w, err)
		return
	}
	// Trying to subscribe to the stream
	// Construct `Binance` with a read-only channel and start processing incoming data
	cs := &CandleSubsciption{symbol: symbol, interval: interval}
	b := NewBinance(context.Background(), inbound(cs))

	defer s.cleanUp(w, r, conn, b)
	go receiver[[]byte](b.dataChannel, conn)
}

func (s *Server) historyHandler(w http.ResponseWriter, r *http.Request) {
	s.infoLog.Printf("%v %v: %v\n", r.Proto, r.Method, r.URL)
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		s.clientError(w, http.StatusMethodNotAllowed)
		return
	}
	if r.URL.Path != "/klines" {
		s.notFound(w)
		return
	}

	/* TODO:
	- need to get historical data from Database
	- need to check how much data to load (limit=1000 ???)
	- need to create db stuff
	- need implement binance api request `GET /api/v3/klines`
	  and store the data in db
	*/
	s.render(w, nil)
}

func (s *Server) cleanUp(w http.ResponseWriter, r *http.Request, conn *websocket.Conn, b *Binance) {
	for {
		msgType, msg, err := conn.Read(r.Context())
		if err != nil {
			switch websocket.CloseStatus(err) {
			case websocket.StatusNormalClosure: // Disconnect by client
			case websocket.StatusGoingAway: // Closing the tab
				s.infoLog.Printf("Received message from client: %v\n", websocket.CloseStatus(err))
				defer b.close()
				s.infoLog.Println("CALLED defer b.close()!")
				return
			default:
				s.errorLog.Printf("WebSocket read error: %v - %v\n", err, websocket.CloseStatus(err))
				return
			}
		}

		if msgType == websocket.MessageText {
			// Process the incoming text message (closing message or other messages)
			message := string(msg)
			if message == "CLOSE" {
				// Handle the custom closing message from the client
				s.infoLog.Printf("Received closing message from client: %v\n", message)
				conn.Close(websocket.StatusNormalClosure, "Closed by client")
				b.close()
				break
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

func receiver[T ~string | ~[]byte](in chan T, conn *websocket.Conn) {
	// Process data received from the data channel
	for data := range in {
		// Write the data to the WebSocket connection
		if err := wsjson.Write(context.Background(), conn, string(data)); err != nil {
			log.Printf("Error writing data to WebSocket: %v\n", err)
			continue
		}
	}
}
