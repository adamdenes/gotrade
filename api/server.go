package api

import (
	"context"
	"fmt"
	"html/template"
	"log"
	"net/http"

	"github.com/adamdenes/gotrade/internal/logger"
)

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

	s.router.HandleFunc("/", s.indexHandler)
	s.router.HandleFunc("/search", s.searchHandler)

	return s.router
}

func (s *Server) indexHandler(w http.ResponseWriter, r *http.Request) {
	// Send 404 if destination is not `/`
	if r.URL.Path != "/" {
		// http.Error(w, "404 Page not found", http.StatusNotFound)
		s.notFound(w)
		return
	}

	templateFiles := []string{
		"./web/templates/base.tmpl.html",
		"./web/templates/pages/chart.tmpl.html",
		"./web/templates/partials/script.tmpl.html",
		"./web/templates/partials/search_bar.tmpl.html",
		"./web/templates/partials/dropdown_tf.tmpl.html",
	}

	// Parse the HTML template
	ts, err := template.ParseFiles(templateFiles...)
	if err != nil {
		s.errorLog.Println(err.Error())
		// http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		s.serverError(w, err)
		return
	}
	// Render the template
	err = ts.ExecuteTemplate(w, "base", nil)
	if err != nil {
		s.errorLog.Println(err.Error())
		// http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		s.serverError(w, err)
		return
	}
}

func (s *Server) searchHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		// http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		s.clientError(w, http.StatusMethodNotAllowed)
		return
	}

	// Get the form values for further processing
	symbol := r.FormValue("symbol")
	timeFrame := r.FormValue("timeframe")

	// Trying to subscribe to the stream
	// Construct `Binance` with a read-only channel and start processing incoming data
	cs := &CandleSubsciption{symbol: symbol, timeFrame: timeFrame}
	b := NewBinance(context.Background(), setupWs(cs))
	go processWsData(b.dataChannel)

	fmt.Fprintf(w, "doing something else... you searched for %s", symbol)
}

type CandleSubsciption struct {
	symbol    string
	timeFrame string
}

func setupWs(cs *CandleSubsciption) <-chan *CandleSubsciption {
	out := make(chan *CandleSubsciption)
	go func() {
		out <- cs
		close(out)
	}()
	return out
}

func processWsData(dataChannel chan string) {
	// Process data received from the data channel
	for data := range dataChannel {
		fmt.Println("RECEIVED", data)
	}
}
