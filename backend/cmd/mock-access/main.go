package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"ai-buddy/backend/internal/mockaccess"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	url := os.Getenv("MOCK_DATABASE_URL")
	if url == "" {
		log.Fatal("MOCK_DATABASE_URL is required")
	}
	token := os.Getenv("MOCK_ACCESS_TOKEN")
	if !validToken(token) {
		log.Fatal("MOCK_ACCESS_TOKEN must contain a nonempty token without whitespace")
	}
	decisionToken := os.Getenv("MOCK_DECISION_TOKEN")
	if decisionToken != "" && (!validToken(decisionToken) || decisionToken == token) {
		log.Fatal("MOCK_DECISION_TOKEN must be a distinct token without whitespace")
	}

	db, err := sql.Open("pgx", url)
	if err != nil {
		log.Fatal("Invalid mock PostgreSQL configuration")
	}
	defer db.Close()
	db.SetMaxOpenConns(10)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	err = db.PingContext(ctx)
	cancel()
	if err != nil {
		log.Fatal("Could not connect to mock PostgreSQL. Check MOCK_DATABASE_URL")
	}

	address := os.Getenv("MOCK_HTTP_ADDR")
	if address == "" {
		address = "127.0.0.1:8082"
	}
	server := &http.Server{
		Addr:              address,
		Handler:           mockaccess.NewHandler(db, token, decisionToken),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Printf("Mock access service listening on %s", address)
	log.Fatal(server.ListenAndServe())
}

func validToken(token string) bool {
	fields := strings.Fields(token)
	return len(fields) == 1 && fields[0] == token
}
