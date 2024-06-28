package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
)

type Handler struct {
	DB *sql.DB
}

func NewHandler(db *sql.DB) *Handler {
	return &Handler{DB: db}
}

func (h *Handler) HelloHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Hello, World!")
}

func (h *Handler) StoreHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Word string `json:"word"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	if req.Word == "" {
		http.Error(w, "Word is required", http.StatusBadRequest)
		return
	}

	if _, err := h.DB.Exec("INSERT INTO words (word) VALUES ($1)", req.Word); err != nil {
		http.Error(w, "Could not store the word", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

// ListWordsHandler returns a list of words stored in the database.
func (h *Handler) ListWordsHandler(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.Query("SELECT word FROM words")
	if err != nil {
		http.Error(w, "Could not list words", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var words []string
	for rows.Next() {
		var word string
		if err := rows.Scan(&word); err != nil {
			http.Error(w, "Could not list words", http.StatusInternalServerError)
			return
		}
		words = append(words, word)
	}

	if err := rows.Err(); err != nil {
		http.Error(w, "Could not list words", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(words); err != nil {
		http.Error(w, "Could not list words", http.StatusInternalServerError)
		return
	}
}
