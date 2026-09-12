package worker

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// healthResponse is the body for both probes.
type healthResponse struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// NewHealthHandler serves the worker's probes: /healthz while the process is
// alive, /readyz while ready() (stream reachable, consumer group present) succeeds.
func NewHealthHandler(ready func(context.Context) error) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeHealth(w, http.StatusOK, healthResponse{Status: "ok"})
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := ready(ctx); err != nil {
			writeHealth(w, http.StatusServiceUnavailable, healthResponse{Status: "unhealthy", Error: err.Error()})
			return
		}
		writeHealth(w, http.StatusOK, healthResponse{Status: "ok"})
	})
	return mux
}

func writeHealth(w http.ResponseWriter, code int, resp healthResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(resp) //nolint:errcheck // best-effort response
}
