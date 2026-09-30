package main

import (
	"io"
	"log"
	"net/http"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /webhook", func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			log.Printf("read webhook body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		log.Printf("received event %s", r.Header.Get("X-HookRelay-Event-ID"))
		w.WriteHeader(http.StatusNoContent)
	})

	log.Print("example receiver listening on http://127.0.0.1:9000/webhook")
	log.Fatal(http.ListenAndServe("127.0.0.1:9000", mux))
}
