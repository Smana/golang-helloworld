package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"image-gallery/internal/domain/demo"
)

func (h *Handler) writeDemo(w http.ResponseWriter, c demo.Controls, err error) {
	switch {
	case errors.Is(err, demo.ErrInvalidControls):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case err != nil:
		http.Error(w, "demo controls unavailable", http.StatusInternalServerError)
	default:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(c) //nolint:errcheck // response already committed; nothing actionable on encode failure
	}
}

// getDemoHandler: GET /api/settings/demo
func (h *Handler) getDemoHandler(w http.ResponseWriter, r *http.Request) {
	c, err := h.demo.Get(r.Context())
	h.writeDemo(w, c, err)
}

// updateDemoHandler: PUT /api/settings/demo. The body may be a partial
// document (Task 15's load generator sends e.g. {"slow_db_ms":20}): it is
// decoded onto the current controls, so omitted fields keep their value.
func (h *Handler) updateDemoHandler(w http.ResponseWriter, r *http.Request) {
	c, err := h.demo.Get(r.Context())
	if err != nil {
		h.writeDemo(w, demo.Controls{}, err)
		return
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&c); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	saved, err := h.demo.Update(r.Context(), c)
	h.writeDemo(w, saved, err)
}

// resetDemoHandler: POST /api/settings/demo/reset
func (h *Handler) resetDemoHandler(w http.ResponseWriter, r *http.Request) {
	c, err := h.demo.Reset(r.Context())
	h.writeDemo(w, c, err)
}
