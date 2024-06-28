package main

import (
	"log"
	"net/http"
	"os"

	"helloworld/internal/server"

	"github.com/gorilla/mux"
	_ "github.com/lib/pq"
)

func main() {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL environment variable is required")
	}

	db, err := server.NewDB(dbURL)
	if err != nil {
		log.Fatalf("Could not connect to the database: %v", err)
	}
	defer db.Close()

	h := server.NewHandler(db)

	r := mux.NewRouter()
	r.HandleFunc("/", h.HelloHandler).Methods("GET")
	r.HandleFunc("/store", h.StoreHandler).Methods("POST")
	r.HandleFunc("/list", h.ListWordsHandler).Methods("GET")

	http.Handle("/", r)
	log.Println("Starting server on :8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatalf("Could not start server: %s", err)
	}
}
