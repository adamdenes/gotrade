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

	db, err := storage.NewPostgresDB(os.Getenv("DSN"))
	if err != nil {
		logger.Error.Fatal(err)
	}

	if err := db.Init(); err != nil {
		logger.Error.Fatal(err)
	}
	defer db.Close()

	server := api.NewServer(*addr, db)
	server.Run()
}
