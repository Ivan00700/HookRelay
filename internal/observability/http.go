package observability

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const RequestIDHeader = "X-Request-ID"

type responseWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *responseWriter) WriteHeader(status int) {
	if status >= 100 && status < 200 {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	if w.status == 0 {
		w.status = status
		w.ResponseWriter.WriteHeader(status)
	}
}

func (w *responseWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(body)
	w.bytes += n
	return n, err
}

func (w *responseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (m *Monitor) HTTP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := ""
		if values := r.Header.Values(RequestIDHeader); len(values) == 1 {
			id = values[0]
		}
		if !validRequestID(id) {
			var random [16]byte
			if _, err := rand.Read(random[:]); err != nil {
				m.logger.Error("request id generation failed")
				http.Error(w, "internal server error", http.StatusInternalServerError)
				return
			}
			id = hex.EncodeToString(random[:])
		}
		w.Header().Set(RequestIDHeader, id)
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		ctx = context.WithValue(ctx, loggerKey, m.logger.With("request_id", id))
		r = r.WithContext(ctx)
		writer := &responseWriter{ResponseWriter: w}
		start := time.Now()
		defer func() {
			panicValue := recover()
			status := writer.status
			if status == 0 {
				status = http.StatusOK
				if panicValue != nil {
					status = http.StatusInternalServerError
				}
			}
			// в метках остаются шаблоны маршрутов - ID и query string не создают новые ряды
			route := r.Pattern
			if _, path, ok := strings.Cut(route, " "); ok {
				route = path
			}
			if route == "" {
				route = "unmatched"
			}
			method := metricMethod(r.Method)
			duration := time.Since(start).Seconds()
			m.requests.WithLabelValues(method, route, strconv.Itoa(status)).Inc()
			m.requestTime.WithLabelValues(method, route).Observe(duration)
			level := slog.LevelInfo
			if status >= 500 {
				level = slog.LevelError
			} else if status >= 400 {
				level = slog.LevelWarn
			}
			Logger(ctx).Log(ctx, level, "http request completed", "method", method, "route", route, "status", status, "duration_seconds", duration, "response_bytes", writer.bytes, "panicked", panicValue != nil)
			if panicValue != nil {
				panic(panicValue)
			}
		}()
		next.ServeHTTP(writer, r)
	})
}

func validRequestID(id string) bool {
	if len(id) == 0 || len(id) > 64 {
		return false
	}
	for _, char := range id {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '-' || char == '_' {
			continue
		}
		return false
	}
	return true
}

func metricMethod(method string) string {
	switch method {
	case "GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "CONNECT", "TRACE":
		return method
	default:
		return "OTHER"
	}
}
