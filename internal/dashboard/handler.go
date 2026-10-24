package dashboard

import (
	"embed"
	"net/http"
)

// Файлы интерфейса входят в бинарник и не зависят от рабочей папки процесса.
//
//go:embed assets/*
var files embed.FS

// RegisterRoutes подключает страницу и её статические файлы.
func RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", index)
	mux.HandleFunc("GET /assets/app.css", stylesheet)
	mux.HandleFunc("GET /assets/app.js", script)
}

func index(w http.ResponseWriter, r *http.Request) {
	serveFile(w, r, "assets/index.html")
}

func stylesheet(w http.ResponseWriter, r *http.Request) {
	serveFile(w, r, "assets/app.css")
}

func script(w http.ResponseWriter, r *http.Request) {
	serveFile(w, r, "assets/app.js")
}

func serveFile(w http.ResponseWriter, r *http.Request, name string) {
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
	http.ServeFileFS(w, r, files, name)
}
