package main

import (
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	secret := os.Getenv("WEBHOOK_SECRET")
	if strings.TrimSpace(secret) == "" {
		log.Fatal("set WEBHOOK_SECRET to the secret of the sender endpoint")
	}
	server := &http.Server{
		Addr:              "127.0.0.1:9000",
		Handler:           newHandler(secret),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
	}
	log.Print("example receiver listening on http://127.0.0.1:9000/webhook")
	log.Fatal(server.ListenAndServe())
}
