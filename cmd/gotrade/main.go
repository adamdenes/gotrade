package main

import (
	"flag"

	"github.com/adamdenes/gotrade/api"
	"github.com/adamdenes/gotrade/internal/logger"
)

func main() {
	addr := flag.String("addr", ":4000", "HTTP network address")
	flag.Parse()

	logger.Init()

	server := api.NewServer(*addr)
	server.Run()
}
