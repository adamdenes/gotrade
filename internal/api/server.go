package api

import (
	"log"
	"net/http"

	"github.com/adamdenes/gotrade/internal/routes"
)

type Server struct {
	listenAddress string
	router        http.Handler
}

func NewServer(addr string) *Server {
	router := routes.NewRouter()
	return &Server{
		listenAddress: addr,
		router:        router,
	}
}

func (s *Server) Run() {
	log.Printf("Server listening on localhost%s\n", s.listenAddress)
	err := http.ListenAndServe(s.listenAddress, s.router)

	if err != nil {
		log.Fatalf("error listening on %s: %v", s.listenAddress, err)
	}
}
