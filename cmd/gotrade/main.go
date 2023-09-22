package main

import (
	"fmt"
	"log"
	"os"

	"github.com/adamdenes/gotrade/internal/logger"
	"github.com/adamdenes/gotrade/internal/storage"
)

func main() {
	// addr := flag.String("addr", ":4000", "HTTP network address")
	// flag.Parse()

	logger.Init()

	db, err := storage.NewPostgresStore(os.Getenv("DSN"))
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("%+v\n", db)

	// server := api.NewServer(*addr, db)
	// server.Run()
}
