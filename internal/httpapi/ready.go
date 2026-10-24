package httpapi

import (
	"context"
	"net/http"
	"time"

	"hookrelay/internal/observability"
)

// готовность зависит от хранилища - проверка здоровья остаётся независимой от БД
func (h *Handler) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := h.store.CheckReady(ctx); err != nil {
		observability.Logger(ctx).Warn("readiness check failed", "error_kind", observability.ErrorKind(err))
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}
