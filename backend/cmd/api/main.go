package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"ai-buddy/backend/internal/access"
	"ai-buddy/backend/internal/ai"
	"ai-buddy/backend/internal/assistant"
	"ai-buddy/backend/internal/httpapi"
	"ai-buddy/backend/internal/knowledge"
	"ai-buddy/backend/internal/onboarding"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	address := os.Getenv("HTTP_ADDR")
	if address == "" {
		address = "127.0.0.1:8081"
	}
	handler := httpapi.NewHandler()
	url := os.Getenv("DATABASE_URL")
	mockURL, mockToken := os.Getenv("MOCK_ACCESS_URL"), os.Getenv("MOCK_ACCESS_TOKEN")
	if url == "" && os.Getenv("KNOWLEDGE_RESOURCES_PATH") != "" {
		log.Fatal("Set DATABASE_URL to enable the knowledge base")
	}
	if (mockURL == "") != (mockToken == "") || (mockURL != "" && url == "") {
		log.Fatal("Set DATABASE_URL, MOCK_ACCESS_URL and MOCK_ACCESS_TOKEN to enable request delivery")
	}
	if url != "" {
		db, err := sql.Open("pgx", url)
		if err != nil {
			log.Fatal("Invalid PostgreSQL configuration")
		}
		defer db.Close()
		db.SetMaxOpenConns(10)
		store := onboarding.NewStore(db)
		if err := initialize(db, store); err != nil {
			log.Fatal(err)
		}
		handler, err = configureChat(db, store)
		if err != nil {
			log.Fatal(err)
		}
		if mockURL != "" {
			client, err := access.NewClient(mockURL, mockToken)
			if err != nil {
				log.Fatal("Invalid mock access service configuration")
			}
			worker := onboarding.NewWorker(store, client)
			done := make(chan struct{})
			go func() {
				defer close(done)
				worker.Run(ctx)
			}()
			defer func() { stop(); <-done }()
		}
	}
	server := &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Printf("AI-buddy API listening on %s", address)
	serverResult := make(chan error, 1)
	go func() {
		serverResult <- server.ListenAndServe()
	}()
	select {
	case err := <-serverResult:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}
}

func initialize(db *sql.DB, store *onboarding.Store) error {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	var schemaReady bool
	if err := db.QueryRowContext(ctx, `SELECT count(*) = 7 FROM unnest(ARRAY[
		'app_users', 'app_sessions', 'app_resources', 'app_conversations',
		'app_messages', 'app_drafts', 'app_operations'
	]) AS tables(name) WHERE to_regclass(name) IS NOT NULL`).Scan(&schemaReady); err != nil {
		return errors.New("Could not connect to PostgreSQL. Check DATABASE_URL")
	}
	if !schemaReady {
		return errors.New("Apply the backend migrations before starting the API")
	}
	if _, err := db.ExecContext(ctx, `SELECT attempts, lease_until, lease_token, next_attempt_at FROM app_operations LIMIT 0`); err != nil {
		return errors.New("Apply the operation delivery migration before starting the API")
	}
	if _, err := db.ExecContext(ctx, `SELECT c.owner_team, c.owner_role, m.reply_metadata, m.generation_token, m.generation_until
		FROM app_conversations c JOIN app_messages m ON m.conversation_id = c.id LIMIT 0`); err != nil {
		return errors.New("Apply the grounded messages migration before starting the API")
	}
	atlas, orbit := os.Getenv("DEMO_ATLAS_TOKEN"), os.Getenv("DEMO_ORBIT_TOKEN")
	if atlas != "" && atlas == orbit {
		return errors.New("The two demo sessions require distinct tokens")
	}
	sessions := []struct{ userID, token string }{{"atlas-intern", atlas}, {"orbit-intern", orbit}}
	for _, session := range sessions {
		if session.token == "" {
			continue
		}
		parts := strings.Fields(session.token)
		if len(session.token) < 24 || len(session.token) > 512 || len(parts) != 1 || parts[0] != session.token {
			return errors.New("Demo tokens must contain 24 to 512 bytes without spaces")
		}
	}
	expires := time.Now().Add(24 * time.Hour)
	for _, session := range sessions {
		if session.token != "" {
			if err := store.ProvisionSession(ctx, session.userID, session.token, expires); err != nil {
				return errors.New("Could not create the demo session. Check the demo seed and token configuration")
			}
		}
	}
	return nil
}

func configureChat(db *sql.DB, store *onboarding.Store) (http.Handler, error) {
	path := os.Getenv("KNOWLEDGE_RESOURCES_PATH")
	apiKey := os.Getenv("LLM_API_KEY")
	if path == "" {
		if apiKey != "" {
			return nil, errors.New("Set KNOWLEDGE_RESOURCES_PATH and load the corpus before enabling the model")
		}
		return httpapi.NewApplication(store), nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("Could not read KNOWLEDGE_RESOURCES_PATH")
	}
	var resources []knowledge.Resource
	if err := json.Unmarshal(raw, &resources); err != nil || len(resources) == 0 {
		return nil, errors.New("Invalid knowledge resource catalog")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var seeded bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM knowledge_chunks)`).Scan(&seeded); err != nil || !seeded {
		return nil, errors.New("Apply the knowledge schema and load the corpus before starting the API")
	}
	searcher := knowledge.NewSearcher(db, resources)
	application := httpapi.NewIntegratedApplication
	if os.Getenv("DEMO_GUEST_ACCESS") == "1" {
		application = httpapi.NewGuestApplication
	}
	if apiKey == "" {
		return application(store, assistant.New(db, searcher, assistant.DemoClient{}), searcher), nil
	}
	provider := os.Getenv("LLM_PROVIDER")
	if provider == "" {
		provider = "modelhub"
	}
	client, err := ai.NewClient(provider, apiKey, os.Getenv("LLM_MODEL"), 45*time.Second)
	if err != nil {
		return nil, errors.New("Invalid LLM_PROVIDER or LLM_MODEL configuration")
	}
	return application(store, assistant.New(db, searcher, client), searcher), nil
}
