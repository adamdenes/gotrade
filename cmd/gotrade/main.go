package main

import (
	"flag"
	"os"

	"github.com/adamdenes/gotrade/api"
	"github.com/adamdenes/gotrade/internal/logger"
	"github.com/adamdenes/gotrade/internal/storage"
)

func main() {
	addr := flag.String("addr", ":4000", "HTTP network address")
	flag.Parse()

	logger.Init()

	tc, err := api.NewTemplateCache()
	if err != nil {
		logger.Error.Fatal(err)
	}

	sc, err := api.NewSymbolCache()
	if err != nil {
		logger.Error.Fatal(err)
	}

	db, err := storage.NewTimescaleDB(os.Getenv("TS_DSN"))
	if err != nil {
		logger.Error.Fatal(err)
	}
	defer db.Close()

	go api.PollHistoricalData(db)

	server := api.NewServer(*addr, db, tc, sc)
	server.Run()
}
