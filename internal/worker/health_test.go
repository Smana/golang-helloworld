package worker

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthEndpoints(t *testing.T) {
	var readyErr error
	h := NewHealthHandler(func(context.Context) error { return readyErr })
	get := func(p string) int {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, http.NoBody))
		return rec.Code
	}
	if get("/healthz") != 200 || get("/readyz") != 200 {
		t.Fatal("healthy worker must answer 200 on both probes")
	}
	readyErr = errors.New("consumer group missing")
	if get("/readyz") != 503 || get("/healthz") != 200 {
		t.Fatal("readiness must fail alone when the queue is not ready")
	}
}
