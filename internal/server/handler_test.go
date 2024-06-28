package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestStoreHandler(t *testing.T) {
	// Create a new sqlmock database connection
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("An error '%s' was not expected when opening a stub database connection", err)
	}
	defer db.Close()

	// Set up your expected database interactions
	mock.ExpectExec("INSERT INTO words").WithArgs("example").WillReturnResult(sqlmock.NewResult(1, 1))

	// Initialize your handler with the mock database connection
	handler := &Handler{DB: db}

	// Create a request to pass to our handler
	req, err := http.NewRequest("POST", "/store", strings.NewReader(`{"word":"example"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Record the response
	rr := httptest.NewRecorder()
	handler.StoreHandler(rr, req)

	// Check the status code is what we expect
	if status := rr.Code; status != http.StatusCreated {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusCreated)
	}

	// Ensure all expectations were met
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("there were unfulfilled expectations: %s", err)
	}
}

func TestListWordsHandler(t *testing.T) {
	// Mock the database connection
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("An error '%s' was not expected when opening a stub database connection", err)
	}
	defer db.Close()

	// Set up expected database interactions
	rows := sqlmock.NewRows([]string{"word"}).
		AddRow("word1").
		AddRow("word2").
		AddRow("word3")
	mock.ExpectQuery("SELECT word FROM words").WillReturnRows(rows)

	// Create a new request
	req, err := http.NewRequest("GET", "/list", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Create a response recorder to capture the response
	rr := httptest.NewRecorder()

	// Create a new handler instance with the mock database
	handler := &Handler{DB: db}

	// Call the ListWordsHandler function
	handler.ListWordsHandler(rr, req)

	// Check the response status code
	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
	}

	// Check the response body
	expectedBody := "[\"word1\",\"word2\",\"word3\"]\n"
	if rr.Body.String() != expectedBody {
		t.Errorf("expected body %q, got %q", expectedBody, rr.Body.String())
	}

	// Ensure all expectations were met
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("there were unfulfilled expectations: %s", err)
	}
}
