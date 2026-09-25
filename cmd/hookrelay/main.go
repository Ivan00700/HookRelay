package main

import (
	"log"
	"net/http"
	"os"
)

func healthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if _, err := w.Write([]byte(`{"status":"ok"}`)); err != nil {
		log.Printf("write health response: %v", err)
	}
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler)

	port := os.Getenv("HTTP_PORT")
	if port == "" {
		port = "8080"
	}

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: mux,
	}

	log.Printf("starting server on http://localhost:%s", port)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("server stopped: %v", err)
	}
}
