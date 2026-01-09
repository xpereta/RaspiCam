package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/xpereta/RaspiCam/internal/web"
)

func main() {
	addr := os.Getenv("UI_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	srv, err := web.NewServer()
	if err != nil {
		log.Fatalf("init server: %v", err)
	}

	httpServer := &http.Server{
		Addr:              addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("ui listening on %s", addr)
	log.Fatal(httpServer.ListenAndServe())
}
