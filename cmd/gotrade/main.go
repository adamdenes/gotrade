package main

import (
	"flag"
	"log"
	"os"

	"github.com/adamdenes/gotrade/api"
	"github.com/adamdenes/gotrade/internal/logger"
	"github.com/adamdenes/gotrade/internal/storage"
)

func main() {
	addr := flag.String("addr", ":4000", "HTTP network address")
	flag.Parse()

	logger.Init()

	db, err := storage.NewPostgresStore(os.Getenv("DSN"))
	if err != nil {
		log.Fatal(err)
	}

	if err := db.Init(); err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	server := api.NewServer(*addr, db)
	server.Run()
}
