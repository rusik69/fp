package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/rusik69/fp/internal/httpapi"
)

func main() {
	addr := listenAddress()

	server := &http.Server{
		Addr:              addr,
		Handler:           httpapi.NewHandler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("fp backend listening on %s", addr)
	log.Fatal(server.ListenAndServe())
}

func listenAddress() string {
	if addr := os.Getenv("FP_LISTEN_ADDR"); addr != "" {
		return addr
	}
	return "127.0.0.1:8080"
}
