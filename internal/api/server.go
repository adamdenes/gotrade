package api

import (
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
	s.NewRouter()

	s.infoLog.Printf("Server listening on localhost%s\n", s.listenAddress)
	err := http.ListenAndServe(s.listenAddress, s.router)

	if err != nil {
		s.errorLog.Fatalf("error listening on %s: %v", s.listenAddress, err)
	}
}

func (s *Server) NewRouter() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/", s.indexHandler)
	mux.HandleFunc("/search", s.searchHandler)

	return mux
}

func (s *Server) indexHandler(w http.ResponseWriter, r *http.Request) {
	// Send 404 if destination is not `/`
	if r.URL.Path != "/" {
		http.Error(w, "404 Page not found", http.StatusNotFound)
		return
	}

	templateFiles := []string{
		"./web/templates/base.tmpl.html",
		"./web/templates/pages/chart.tmpl.html",
		"./web/templates/partials/script.tmpl.html",
		"./web/templates/partials/search_bar.tmpl.html",
	}

	// Parse the HTML template
	ts, err := template.ParseFiles(templateFiles...)
	if err != nil {
		s.errorLog.Println(err.Error())
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}
	// Render the template
	err = ts.ExecuteTemplate(w, "base", nil)
	if err != nil {
		s.errorLog.Println(err.Error())
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		return
	}
}

func (s *Server) searchHandler(w http.ResponseWriter, r *http.Request) {
	// Send 404 if destination is not `/`
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	symbol := r.FormValue("symbol")
	fmt.Fprintf(w, "You were searching for '%v'\n", symbol)
}
