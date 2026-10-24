package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestRequestID(t *testing.T) {
	for _, value := range []string{"", "caller_123-ABC", "contains space", "line\nbreak", "русский", strings.Repeat("a", 65)} {
		t.Run(value, func(t *testing.T) {
			var logs bytes.Buffer
			monitor := NewMonitor(NewLogger(&logs), nil)
			handler := monitor.HTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if RequestID(r.Context()) == "" {
					t.Error("request context is missing an ID")
				}
				w.Header().Set("Seen-ID", RequestID(r.Context()))
				w.WriteHeader(http.StatusNoContent)
			}))
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request.Header.Set(RequestIDHeader, value)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			id := response.Header().Get(RequestIDHeader)
			if !validRequestID(id) || id != response.Header().Get("Seen-ID") {
				t.Fatalf("invalid or inconsistent request ID: %q", id)
			}
			if value == "caller_123-ABC" && id != value {
				t.Fatal("valid caller ID was replaced")
			}
			if value != "caller_123-ABC" && (id == value || len(id) != 32) {
				t.Fatal("expected a new random ID")
			}
			var entry map[string]any
			if err := json.Unmarshal(logs.Bytes(), &entry); err != nil || entry["request_id"] != id || entry["status"] != float64(204) {
				t.Fatalf("request log: %s, %v", logs.String(), err)
			}
		})
	}
}

func TestDuplicateRequestIDIsReplaced(t *testing.T) {
	monitor := NewMonitor(nil, nil)
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Add(RequestIDHeader, "first")
	request.Header.Add(RequestIDHeader, "second")
	response := httptest.NewRecorder()
	monitor.HTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).ServeHTTP(response, request)
	if id := response.Header().Get(RequestIDHeader); !validRequestID(id) || id == "first" || id == "second" {
		t.Fatalf("ambiguous caller ID accepted: %q", id)
	}
}

func TestHTTPMetricsUseBoundedLabels(t *testing.T) {
	var logs bytes.Buffer
	monitor := NewMonitor(NewLogger(&logs), nil)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/events/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	handler := monitor.HTTP(mux)
	for _, id := range []string{"private-first", "private-second"} {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/events/"+id+"?token=hidden-query", nil)
		request.Header.Set("Authorization", "hidden-authorization")
		handler.ServeHTTP(httptest.NewRecorder(), request)
	}
	for i := 0; i < 20; i++ {
		request := httptest.NewRequest(fmt.Sprintf("CUSTOM%d", i), fmt.Sprintf("/unknown-%d", i), nil)
		handler.ServeHTTP(httptest.NewRecorder(), request)
	}
	if count := testutil.ToFloat64(monitor.requests.WithLabelValues("GET", "/api/v1/events/{id}", "200")); count != 2 {
		t.Fatalf("successful requests=%v, want 2", count)
	}
	if count := testutil.ToFloat64(monitor.requests.WithLabelValues("OTHER", "unmatched", "404")); count != 20 {
		t.Fatalf("unknown requests=%v, want 20", count)
	}
	families, err := monitor.registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range families {
		if family.GetName() == "hookrelay_http_requests_total" && len(family.Metric) != 2 {
			t.Fatalf("request labels created %d series", len(family.Metric))
		}
		if family.GetName() == "hookrelay_http_request_duration_seconds" {
			var count uint64
			for _, metric := range family.Metric {
				count += metric.GetHistogram().GetSampleCount()
			}
			if count != 22 {
				t.Fatalf("histogram observed %d requests, want 22", count)
			}
		}
	}
	for _, private := range []string{"private-first", "private-second", "hidden-query", "hidden-authorization"} {
		if strings.Contains(logs.String(), private) {
			t.Fatalf("request log exposed %q", private)
		}
	}
}

func TestPanicIsRecordedAndRethrown(t *testing.T) {
	var logs bytes.Buffer
	monitor := NewMonitor(NewLogger(&logs), nil)
	handler := monitor.HTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("private-panic-value")
	}))
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	}()
	if recovered != "private-panic-value" || strings.Contains(logs.String(), "private-panic-value") {
		t.Fatal("panic was swallowed or its contents leaked")
	}
	if count := testutil.ToFloat64(monitor.requests.WithLabelValues("GET", "unmatched", "500")); count != 1 {
		t.Fatalf("panic count=%v", count)
	}
}

type headerRecorder struct {
	header   http.Header
	statuses []int
}

func (r *headerRecorder) Header() http.Header            { return r.header }
func (r *headerRecorder) WriteHeader(status int)         { r.statuses = append(r.statuses, status) }
func (r *headerRecorder) Write(body []byte) (int, error) { return len(body), nil }

func TestResponseWriterKeepsFinalStatus(t *testing.T) {
	base := &headerRecorder{header: make(http.Header)}
	writer := &responseWriter{ResponseWriter: base}
	writer.WriteHeader(http.StatusEarlyHints)
	writer.WriteHeader(http.StatusCreated)
	writer.WriteHeader(http.StatusInternalServerError)
	writer.Write([]byte("ok"))
	if writer.status != http.StatusCreated || writer.bytes != 2 || len(base.statuses) != 2 || base.statuses[0] != 103 || base.statuses[1] != 201 {
		t.Fatalf("status recording changed HTTP behavior: %+v, %+v", writer, base)
	}
	response := httptest.NewRecorder()
	if err := http.NewResponseController(&responseWriter{ResponseWriter: response}).Flush(); err != nil {
		t.Fatalf("underlying writer is inaccessible: %v", err)
	}
}

func TestLoggerOutsideHTTPContext(t *testing.T) {
	if RequestID(context.Background()) != "" || Logger(context.Background()) == nil {
		t.Fatal("unexpected logger fallback")
	}
}
