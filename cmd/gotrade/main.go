package main

import (
	"fmt"

	"github.com/adamdenes/gotrade/internal/api"
)

func main() {
	port := 8080
	addr := fmt.Sprintf(":%d", port)

	server := api.NewServer(addr)
	server.Run()
}
