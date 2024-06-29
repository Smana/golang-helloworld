package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"helloworld/internal/server"

	"github.com/gorilla/mux"
	_ "github.com/lib/pq"
)

func main() {
	dbURL := getDatabaseURL()
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

func getDatabaseURL() string {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL != "" {
		return dbURL
	}

	// Construct the DATABASE_URL from individual components
	pgHost := os.Getenv("PGHOST")
	pgHostAddr := os.Getenv("PGHOSTADDR")
	pgPort := os.Getenv("PGPORT")
	pgDatabase := os.Getenv("PGDATABASE")
	pgUser := os.Getenv("PGUSER")
	pgPassword := os.Getenv("PGPASSWORD")

	host := pgHost
	if pgHostAddr != "" {
		host = pgHostAddr
	}

	if host == "" || pgPort == "" || pgDatabase == "" || pgUser == "" {
		log.Fatal("All database environment variables (PGHOST/PGHOSTADDR, PGPORT, PGDATABASE, PGUSER) are required if DATABASE_URL is not set")
	}

	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", pgUser, pgPassword, host, pgPort, pgDatabase)
}
