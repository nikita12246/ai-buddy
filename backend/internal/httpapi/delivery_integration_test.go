package httpapi_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"ai-buddy/backend/internal/access"
	"ai-buddy/backend/internal/mockaccess"
	"ai-buddy/backend/internal/onboarding"
)

func verifyRequestDelivery(t *testing.T, ctx context.Context, db *sql.DB, store *onboarding.Store, app http.Handler, operationPath, atlasToken, key string, payload []byte) {
	t.Helper()
	serviceToken := strings.Repeat("s", 32)
	decisionToken := strings.Repeat("d", 32)
	mock := mockaccess.NewHandler(db, serviceToken, decisionToken)
	var createCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/requests" && createCalls.Add(1) == 1 {
			response := httptest.NewRecorder()
			mock.ServeHTTP(response, r)
			if response.Code == http.StatusCreated {
				// Persist first, then drop the HTTP response.
				conn, _, err := w.(http.Hijacker).Hijack()
				if err == nil {
					_ = conn.Close()
					return
				}
			}
			w.WriteHeader(response.Code)
			_, _ = w.Write(response.Body.Bytes())
			return
		}
		mock.ServeHTTP(w, r)
	}))
	defer server.Close()
	client, err := access.NewClient(server.URL, serviceToken)
	if err != nil {
		t.Fatal(err)
	}

	stopFirst := startTestWorker(ctx, store, client)
	defer stopFirst()
	waitForOperation(t, app, operationPath, atlasToken, func(operation onboardingOperation) bool {
		return operation.DeliveryState == "reconciling" && operation.ExternalID == "" && operation.ServiceStatus == ""
	})
	stopFirst()
	committed, err := client.Lookup(ctx, key)
	if err != nil {
		t.Fatalf("mock did not save the request before the response was lost: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE app_operations SET delivery_state = 'sending',
		lease_token = 'expired-test-worker', lease_until = now() - interval '1 second', next_attempt_at = now()
		WHERE idempotency_key = $1`, key); err != nil {
		t.Fatal(err)
	}

	stopSecond := startTestWorker(ctx, store, client)
	defer stopSecond()
	waitForOperation(t, app, operationPath, atlasToken, func(operation onboardingOperation) bool {
		return operation.DeliveryState == "delivered" && operation.ExternalID == committed.ID && operation.ServiceStatus == "pending_approval"
	})
	if createCalls.Load() != 1 {
		t.Fatalf("recovery sent %d create calls, want one", createCalls.Load())
	}
	replayed, err := client.Create(ctx, key, payload)
	if err != nil || replayed.ID != committed.ID {
		t.Fatalf("same idempotency key did not return the original request: %v", err)
	}
	var changed map[string]any
	if err := json.Unmarshal(payload, &changed); err != nil {
		t.Fatal(err)
	}
	changed["justification"] = "Изменённое обоснование"
	body, err := json.Marshal(changed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Create(ctx, key, body); !errors.Is(err, access.ErrConflict) {
		t.Fatalf("same key with a different body: got %v, want conflict", err)
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM mock_access.requests WHERE idempotency_key = $1`, key).Scan(&count); err != nil || count != 1 {
		t.Fatalf("expected one saved mock request, got %d: %v", count, err)
	}
	decisionPath := "/requests/" + committed.ID + "/decision"
	decision := map[string]string{"status": "approved"}
	var ignored json.RawMessage
	callOnboarding(t, mock, http.MethodPatch, decisionPath, serviceToken, decision, http.StatusUnauthorized, &ignored)
	var accepted access.Ticket
	callOnboarding(t, mock, http.MethodPatch, decisionPath, decisionToken, decision, http.StatusOK, &accepted)
	if accepted.ID != committed.ID || accepted.Status != "approved" {
		t.Fatalf("unexpected decision: %+v", accepted)
	}
	waitForOperation(t, app, operationPath, atlasToken, func(operation onboardingOperation) bool {
		return operation.DeliveryState == "delivered" && operation.ExternalID == committed.ID && operation.ServiceStatus == "approved"
	})

	var conversation struct{ ID string }
	callOnboarding(t, app, http.MethodPost, "/api/conversations", atlasToken, nil, http.StatusCreated, &conversation)
	var message struct{ Draft onboardingDraft }
	callOnboarding(t, app, http.MethodPost, "/api/conversations/"+conversation.ID+"/messages", atlasToken,
		map[string]string{"client_message_id": "normal-delivery", "text": "Нужен доступ к учебному GitLab"}, http.StatusOK, &message)
	var draft onboardingDraft
	draftPath := "/api/drafts/" + message.Draft.ID
	callOnboarding(t, app, http.MethodPatch, draftPath, atlasToken,
		map[string]any{"expected_version": message.Draft.Version, "justification": "Второй учебный пример"}, http.StatusOK, &draft)
	var queued onboardingOperation
	callOnboarding(t, app, http.MethodPost, draftPath+"/confirm", atlasToken,
		map[string]int{"version": draft.Version}, http.StatusOK, &queued)
	secondPath := "/api/operations/" + queued.ID
	waitForOperation(t, app, secondPath, atlasToken, func(operation onboardingOperation) bool {
		return operation.DeliveryState == "delivered" && operation.ExternalID != "" && operation.ExternalID != committed.ID &&
			operation.ServiceStatus == "pending_approval"
	})
	stopSecond()

	// Simulate the last lookup still running, then its lease expiring without a result.
	if _, err := db.ExecContext(ctx, `UPDATE app_operations
		SET delivery_state = 'reconciling', service_status = NULL, external_id = NULL,
			attempts = 5, lease_until = now() + interval '15 seconds'
		WHERE id = $1`, queued.ID); err != nil {
		t.Fatal(err)
	}
	var status onboardingOperation
	callOnboarding(t, app, http.MethodGet, secondPath, atlasToken, nil, http.StatusOK, &status)
	if status.NeedsManualCheck {
		t.Fatal("a live lease must keep the last lookup in progress")
	}
	if _, err := db.ExecContext(ctx, "UPDATE app_operations SET lease_until = now() WHERE id = $1", queued.ID); err != nil {
		t.Fatal(err)
	}
	callOnboarding(t, app, http.MethodGet, secondPath, atlasToken, nil, http.StatusOK, &status)
	if !status.NeedsManualCheck || status.DeliveryState != "reconciling" || status.ExternalID != "" {
		t.Fatalf("exhausted reconciliation must remain unknown: %+v", status)
	}
}

func startTestWorker(ctx context.Context, store *onboarding.Store, client *access.Client) func() {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		onboarding.NewWorker(store, client).Run(ctx)
	}()
	return func() { cancel(); <-done }
}

func waitForOperation(t *testing.T, app http.Handler, path, token string, matches func(onboardingOperation) bool) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	var operation onboardingOperation
	for time.Now().Before(deadline) {
		callOnboarding(t, app, http.MethodGet, path, token, nil, http.StatusOK, &operation)
		if matches(operation) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("operation did not reach the expected state: %+v", operation)
}
