package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/rusik69/fp/internal/httpapi"
)

func main() {
	addr := os.Getenv("FP_LISTEN_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	server := &http.Server{
		Addr:              addr,
		Handler:           httpapi.NewHandler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("fp backend listening on %s", addr)
	log.Fatal(server.ListenAndServe())
}
