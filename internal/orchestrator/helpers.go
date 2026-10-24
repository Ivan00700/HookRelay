package orchestrator

import "os"

// читаем порт из окружения для запуска с разными настройками
func httpPort() string {
	port := os.Getenv("HTTP_PORT")
	if port == "" {
		return "8080"
	}
	return port
}
