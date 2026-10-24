package orchestrator

import "os"

// Порт читается из окружения, чтобы один бинарник работал с разными настройками.
func httpPort() string {
	port := os.Getenv("HTTP_PORT")
	if port == "" {
		return "8080"
	}
	return port
}
