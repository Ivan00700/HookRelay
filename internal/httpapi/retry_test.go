package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"hookrelay/internal/domain"
	"hookrelay/internal/repository"
)

func TestRetryAndAttemptRoutes(t *testing.T) {
	store := repository.NewMemoryStore()
	mux := http.NewServeMux()
	NewHandler(store).RegisterRoutes(mux)
	created := request(mux, http.MethodPost, "/api/v1/endpoints", `{"name":"orders","url":"http://example.test","secret":"private"}`)
	var endpoint domain.Endpoint
	if err := json.Unmarshal(created.Body.Bytes(), &endpoint); err != nil {
		t.Fatal(err)
	}
	created = request(mux, http.MethodPost, "/api/v1/events", fmt.Sprintf(`{"endpoint_id":%q,"type":"test","payload":{}}`, endpoint.ID))
	var event domain.Event
	if err := json.Unmarshal(created.Body.Bytes(), &event); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/events/" + event.ID
	history := request(mux, http.MethodGet, path+"/attempts", "")
	if history.Code != http.StatusOK || strings.TrimSpace(history.Body.String()) != "[]" {
		t.Fatalf("empty history: %d %s", history.Code, history.Body.String())
	}
	assertAPIError(t, request(mux, http.MethodPost, path+"/retry", ""), http.StatusConflict, "conflict", "")
	assertAPIError(t, request(mux, http.MethodPost, "/api/v1/events/missing/retry", ""), http.StatusNotFound, "not_found", "")
	assertAPIError(t, request(mux, http.MethodGet, "/api/v1/events/missing/attempts", ""), http.StatusNotFound, "not_found", "")
	ctx := context.Background()
	job, err := store.ClaimEvent(ctx, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	assertAPIError(t, request(mux, http.MethodPost, path+"/retry", ""), http.StatusConflict, "conflict", "")
	if err := store.FinishEvent(ctx, job, domain.DeliveryResult{HTTPStatus: 400, ErrorMessage: "bad request"}); err != nil {
		t.Fatal(err)
	}
	retried := request(mux, http.MethodPost, path+"/retry", "")
	if retried.Code != http.StatusAccepted {
		t.Fatalf("retry: %d %s", retried.Code, retried.Body.String())
	}
	var pending domain.Event
	if err := json.Unmarshal(retried.Body.Bytes(), &pending); err != nil || pending.ID != event.ID || pending.Status != domain.DeliveryStatusPending {
		t.Fatalf("retry response: %s, %v", retried.Body.String(), err)
	}
	assertAPIError(t, request(mux, http.MethodPost, path+"/retry", ""), http.StatusConflict, "conflict", "")
	job, err = store.ClaimEvent(ctx, time.Minute)
	if err != nil || job.AttemptNumber != 2 || job.CycleAttempt != 1 {
		t.Fatalf("new delivery cycle: %+v, %v", job, err)
	}
	if err := store.FinishEvent(ctx, job, domain.DeliveryResult{HTTPStatus: 204}); err != nil {
		t.Fatal(err)
	}
	history = request(mux, http.MethodGet, path+"/attempts", "")
	var attempts []domain.DeliveryAttempt
	if err := json.Unmarshal(history.Body.Bytes(), &attempts); err != nil || history.Code != http.StatusOK || len(attempts) != 2 {
		t.Fatalf("history: %d %s, %v", history.Code, history.Body.String(), err)
	}
	assertAPIError(t, request(mux, http.MethodPost, path+"/retry", ""), http.StatusConflict, "conflict", "")
}
