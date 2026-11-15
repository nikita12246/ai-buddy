package httpapi_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"ai-buddy/backend/internal/httpapi"
	"ai-buddy/backend/internal/onboarding"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestOnboardingHTTP(t *testing.T) {
	databaseURL := os.Getenv("BACKEND_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("BACKEND_TEST_DATABASE_URL is required for the PostgreSQL integration test")
	}

	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatal("could not open the integration database")
	}
	if os.Getenv("BACKEND_TEST_SINGLE_CONNECTION") == "1" {
		db.SetMaxOpenConns(1)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal("could not connect to the integration database")
	}
	for _, migration := range []string{"001_session_drafts.sql", "002_demo_seed.sql", "003_operation_delivery.sql", "004_mock_access.sql", "005_grounded_messages.sql"} {
		script, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", migration))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, string(script)); err != nil {
			t.Fatalf("apply %s: %v", migration, err)
		}
	}

	store := onboarding.NewStore(db)
	atlasToken := createTestSession(t, ctx, store, "atlas-intern")
	orbitToken := createTestSession(t, ctx, store, "orbit-intern")
	app := httpapi.NewApplication(store)

	assertOnboardingError(t, app, http.MethodGet, "/api/me", "", nil, http.StatusUnauthorized)
	assertOnboardingError(t, app, http.MethodGet, "/api/operations", "", nil, http.StatusUnauthorized)
	var profile struct {
		ID     string `json:"id"`
		TeamID string `json:"team_id"`
		Role   string `json:"role"`
		Name   string `json:"name"`
	}
	callOnboarding(t, app, http.MethodGet, "/api/me", atlasToken, nil, http.StatusOK, &profile)
	if profile.ID != "atlas-intern" || profile.TeamID != "atlas" || profile.Role == "" || profile.Name == "" {
		t.Fatal("profile did not return the authenticated user's identity, team, role and name")
	}

	var created struct {
		ID string `json:"id"`
	}
	callOnboarding(t, app, http.MethodPost, "/api/conversations", atlasToken, nil, http.StatusCreated, &created)
	if created.ID == "" {
		t.Fatal("conversation ID is missing")
	}
	conversationPath := "/api/conversations/" + created.ID
	messagePath := conversationPath + "/messages"
	assertOnboardingError(t, app, http.MethodGet, conversationPath, orbitToken, nil, http.StatusNotFound)

	const question = "Нужен доступ к учебному GitLab"
	message := map[string]string{"client_message_id": "00000000-0000-4000-8000-000000000001", "text": question}
	assertOnboardingError(t, app, http.MethodPost, messagePath, orbitToken, message, http.StatusNotFound)
	var answer, replay struct {
		Answer             string          `json:"answer"`
		SourceIDs          json.RawMessage `json:"source_ids"`
		ClarifyingQuestion json.RawMessage `json:"clarifying_question"`
		Draft              onboardingDraft `json:"draft"`
	}
	callOnboarding(t, app, http.MethodPost, messagePath, atlasToken, message, http.StatusOK, &answer)
	if answer.Answer == "" || answer.SourceIDs == nil || answer.ClarifyingQuestion == nil {
		t.Fatal("message response is missing answer fields")
	}
	draft := answer.Draft
	if draft.ID == "" || draft.ToolID != "gitlab" || draft.ResourceID != "atlas-training-repo" || draft.Version < 1 {
		t.Fatalf("unexpected initial draft: %+v", draft)
	}
	assertListedRequests(t, app, atlasToken)
	var historyBefore onboardingConversation
	callOnboarding(t, app, http.MethodGet, conversationPath, atlasToken, nil, http.StatusOK, &historyBefore)
	expectedMessages := []onboardingMessage{
		{Role: "user", Text: question},
		{Role: "assistant", Text: answer.Answer},
	}
	if !reflect.DeepEqual(historyBefore.Messages, expectedMessages) {
		t.Fatal("conversation history did not preserve the question and answer in order")
	}

	callOnboarding(t, app, http.MethodPost, messagePath, atlasToken, message, http.StatusOK, &replay)
	if !reflect.DeepEqual(replay, answer) {
		t.Fatal("repeated client_message_id did not return the original response")
	}
	message["text"] = "Нужен другой ресурс"
	assertOnboardingError(t, app, http.MethodPost, messagePath, atlasToken, message, http.StatusConflict)

	draftPath := "/api/drafts/" + draft.ID
	confirmPath := draftPath + "/confirm"
	assertOnboardingError(t, app, http.MethodPost, confirmPath, atlasToken,
		map[string]int{"version": draft.Version}, http.StatusUnprocessableEntity)
	const justification = "Выполняю учебное задание"
	edit := map[string]any{"expected_version": draft.Version, "justification": justification}
	assertOnboardingError(t, app, http.MethodPatch, draftPath, orbitToken, edit, http.StatusNotFound)
	assertOnboardingError(t, app, http.MethodPatch, draftPath, atlasToken,
		map[string]any{"expected_version": draft.Version, "resource_id": "orbit-training-repo"}, http.StatusUnprocessableEntity)

	var updated onboardingDraft
	callOnboarding(t, app, http.MethodPatch, draftPath, atlasToken, edit, http.StatusOK, &updated)
	if updated.ID != draft.ID || updated.Version != draft.Version+1 || updated.Justification != justification ||
		updated.ToolID != draft.ToolID || updated.ResourceID != draft.ResourceID {
		t.Fatalf("unexpected edited draft: %+v", updated)
	}
	assertOnboardingError(t, app, http.MethodPatch, draftPath, atlasToken, edit, http.StatusConflict)
	assertOnboardingError(t, app, http.MethodPost, confirmPath, atlasToken,
		map[string]int{"version": draft.Version}, http.StatusConflict)

	confirmation := map[string]int{"version": updated.Version}
	assertOnboardingError(t, app, http.MethodPost, confirmPath, orbitToken, confirmation, http.StatusNotFound)
	var operation, duplicate, retrieved onboardingOperation
	operation = confirmConcurrently(t, app, confirmPath, atlasToken, updated.Version)
	if operation.ID == "" || operation.DeliveryState != "queued" || operation.ExternalID != "" ||
		operation.ServiceStatus != "" || operation.NextStep == "" {
		t.Fatalf("unexpected queued operation: %+v", operation)
	}
	callOnboarding(t, app, http.MethodPost, confirmPath, atlasToken, confirmation, http.StatusOK, &duplicate)
	if duplicate != operation {
		t.Fatalf("repeated confirmation returned a different operation: %+v", duplicate)
	}
	assertOnboardingError(t, app, http.MethodPatch, draftPath, atlasToken,
		map[string]any{"expected_version": updated.Version, "justification": "Новая причина"}, http.StatusConflict)
	var payload []byte
	var version int
	var key string
	if err := db.QueryRowContext(ctx, `SELECT immutable_payload, confirmed_version, idempotency_key
		FROM app_operations WHERE id = $1`, operation.ID).Scan(&payload, &version, &key); err != nil {
		t.Fatal(err)
	}
	var snapshot struct {
		UserID        string `json:"user_id"`
		TeamID        string `json:"team_id"`
		DraftID       string `json:"draft_id"`
		ResourceID    string `json:"resource_id"`
		Justification string `json:"justification"`
	}
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		t.Fatal(err)
	}
	if version != updated.Version || key == "" || snapshot.UserID != "atlas-intern" || snapshot.TeamID != "atlas" ||
		snapshot.DraftID != draft.ID || snapshot.ResourceID != draft.ResourceID || snapshot.Justification != justification {
		t.Fatal("queued operation did not save the exact confirmed draft and trusted actor")
	}
	operationPath := "/api/operations/" + operation.ID
	assertOnboardingError(t, app, http.MethodGet, operationPath, orbitToken, nil, http.StatusNotFound)
	assertOnboardingError(t, app, http.MethodGet, operationPath, "", nil, http.StatusUnauthorized)

	restarted := httpapi.NewApplication(onboarding.NewStore(db))
	callOnboarding(t, restarted, http.MethodGet, operationPath, atlasToken, nil, http.StatusOK, &retrieved)
	if retrieved != operation {
		t.Fatalf("unexpected persisted operation: %+v", retrieved)
	}
	var historyAfter onboardingConversation
	callOnboarding(t, restarted, http.MethodGet, conversationPath, atlasToken, nil, http.StatusOK, &historyAfter)
	if historyAfter.ID != created.ID || historyAfter.Draft != updated || historyAfter.Operation != operation {
		t.Fatal("fresh handler did not restore the conversation, draft and operation")
	}
	if !reflect.DeepEqual(historyAfter.Messages, historyBefore.Messages) {
		t.Fatal("retried message changed the saved conversation history")
	}
	var newConversation struct{ ID string }
	callOnboarding(t, restarted, http.MethodPost, "/api/conversations", atlasToken, nil, http.StatusCreated, &newConversation)
	if newConversation.ID == "" || newConversation.ID == created.ID {
		t.Fatal("expected a separate conversation")
	}
	assertListedRequests(t, restarted, atlasToken, operation)
	assertListedRequests(t, restarted, orbitToken)

	statusApp := httpapi.NewIntegratedApplication(store, statusOnlyAnswerer{t}, nil)
	checkChatStatus(t, statusApp, atlasToken, newConversation.ID, "status-before", "Заявка в очереди", "atlas-training-repo")
	var orbitConversation struct{ ID string }
	callOnboarding(t, statusApp, http.MethodPost, "/api/conversations", orbitToken, nil, http.StatusCreated, &orbitConversation)
	checkChatStatus(t, statusApp, orbitToken, orbitConversation.ID, "status-empty", "нет подтверждённых заявок", "")

	verifyRequestDelivery(t, ctx, db, store, restarted, operationPath, atlasToken, key, payload)
	callOnboarding(t, restarted, http.MethodGet, operationPath, atlasToken, nil, http.StatusOK, &retrieved)
	assertListedRequests(t, restarted, atlasToken, retrieved)
	checkChatStatus(t, statusApp, atlasToken, newConversation.ID, "status-many", "несколько заявок", "")
	checkChatStatus(t, statusApp, atlasToken, created.ID, "status-approved", "Учебный доступ одобрен", retrieved.ExternalID)

	verifyGroundedChat(t, ctx, db, store, atlasToken, orbitToken)
	verifyGuestSessions(t, store)

	if _, err := db.ExecContext(ctx, "UPDATE app_sessions SET expires_at = now() - interval '1 minute' WHERE user_id = $1", "orbit-intern"); err != nil {
		t.Fatalf("expire test session: %v", err)
	}
	assertOnboardingError(t, restarted, http.MethodGet, "/api/me", orbitToken, nil, http.StatusUnauthorized)
}

func verifyGuestSessions(t *testing.T, store *onboarding.Store) {
	t.Helper()
	app := httpapi.NewGuestApplication(store, nil, nil)
	newGuest := func() (onboarding.Profile, *http.Cookie) {
		t.Helper()
		response := httptest.NewRecorder()
		app.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/session", nil))
		var actor onboarding.Profile
		cookies := response.Result().Cookies()
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &actor) != nil ||
			actor.TeamID != "atlas" || actor.Role != "intern" || len(cookies) != 1 ||
			!cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
			t.Fatalf("guest session was not created: HTTP %d", response.Code)
		}
		return actor, cookies[0]
	}
	first, firstCookie := newGuest()
	second, secondCookie := newGuest()
	if first.ID == second.ID || firstCookie.Value == secondCookie.Value {
		t.Fatal("each browser must get a separate guest identity")
	}

	request := func(method, path string, cookie *http.Cookie) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.AddCookie(cookie)
		response := httptest.NewRecorder()
		app.ServeHTTP(response, req)
		return response
	}
	resumed := request(http.MethodPost, "/api/session", firstCookie)
	var actor onboarding.Profile
	if resumed.Code != http.StatusOK || json.Unmarshal(resumed.Body.Bytes(), &actor) != nil || actor != first {
		t.Fatal("guest identity must survive page reload")
	}
	response := request(http.MethodPost, "/api/conversations", firstCookie)
	var created struct{ ID string }
	if response.Code != http.StatusCreated || json.Unmarshal(response.Body.Bytes(), &created) != nil || created.ID == "" {
		t.Fatalf("guest conversation was not created: HTTP %d", response.Code)
	}
	path := "/api/conversations/" + created.ID
	if request(http.MethodGet, path, firstCookie).Code != http.StatusOK ||
		request(http.MethodGet, path, secondCookie).Code != http.StatusNotFound {
		t.Fatal("a guest conversation must only be visible to its owner")
	}
}

type statusOnlyAnswerer struct{ t *testing.T }

func (a statusOnlyAnswerer) Generate(context.Context, onboarding.Profile, string, []onboarding.Message, *onboarding.Operation) (*onboarding.Generation, error) {
	a.t.Fatal("a status command must not call the model")
	return nil, nil
}

func (a statusOnlyAnswerer) VerifyEvidence(context.Context, onboarding.Profile, []onboarding.Citation) (bool, error) {
	a.t.Fatal("a status command must not use search citations")
	return false, nil
}

func checkChatStatus(t *testing.T, app http.Handler, token, conversationID, messageID, status, identifier string) {
	t.Helper()
	var reply onboarding.Reply
	callOnboarding(t, app, http.MethodPost, "/api/conversations/"+conversationID+"/messages", token,
		map[string]string{"client_message_id": messageID, "text": "Проверить статус заявки"}, http.StatusOK, &reply)
	if !strings.Contains(reply.Answer, status) || len(reply.SourceIDs) != 0 {
		t.Fatalf("unexpected status answer: %+v", reply)
	}
	if identifier != "" && !strings.Contains(reply.Answer, identifier) {
		t.Fatalf("missing request identifier %q in %q", identifier, reply.Answer)
	}
	if strings.Contains(reply.Answer, "orbit-training-repo") || (identifier == "" && strings.Contains(reply.Answer, "atlas-training-repo")) {
		t.Fatalf("unexpected request details in %q", reply.Answer)
	}
}

func confirmConcurrently(t *testing.T, app http.Handler, path, token string, version int) onboardingOperation {
	t.Helper()
	payload, err := json.Marshal(map[string]int{"version": version})
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan *httptest.ResponseRecorder, 2)
	for range 2 {
		go func() {
			request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(payload))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Authorization", "Bearer "+token)
			recorder := httptest.NewRecorder()
			<-start
			app.ServeHTTP(recorder, request)
			results <- recorder
		}()
	}
	close(start)

	var operations [2]onboardingOperation
	for i := range operations {
		response := <-results
		if response.Code != http.StatusOK {
			t.Fatalf("concurrent confirmation returned HTTP %d: %s", response.Code, response.Body)
		}
		if err := json.Unmarshal(response.Body.Bytes(), &operations[i]); err != nil {
			t.Fatal(err)
		}
	}
	if operations[0] != operations[1] {
		t.Fatalf("concurrent confirmation returned different operations: %+v", operations)
	}
	return operations[0]
}

func assertListedRequests(t *testing.T, app http.Handler, token string, expected ...onboardingOperation) {
	t.Helper()
	type listedRequest struct {
		onboardingOperation
		ToolID     string `json:"tool_id"`
		ResourceID string `json:"resource_id"`
	}
	var result struct {
		Operations []listedRequest `json:"operations"`
	}
	callOnboarding(t, app, http.MethodGet, "/api/operations", token, nil, http.StatusOK, &result)
	if result.Operations == nil || len(result.Operations) < len(expected) || (len(expected) == 0 && len(result.Operations) != 0) {
		t.Fatalf("unexpected operations array: %+v", result.Operations)
	}
	for _, operation := range expected {
		found := slices.ContainsFunc(result.Operations, func(request listedRequest) bool {
			return request.onboardingOperation == operation && request.ToolID == "gitlab" && request.ResourceID == "atlas-training-repo"
		})
		if !found {
			t.Fatalf("expected operation %+v in %+v", operation, result.Operations)
		}
	}
}

type onboardingDraft struct {
	ID            string `json:"id"`
	ToolID        string `json:"tool_id"`
	ResourceID    string `json:"resource_id"`
	Justification string `json:"justification"`
	Version       int    `json:"version"`
}

type onboardingOperation struct {
	ID               string `json:"id"`
	DeliveryState    string `json:"delivery_state"`
	ExternalID       string `json:"external_id"`
	ServiceStatus    string `json:"service_status"`
	NextStep         string `json:"next_step"`
	NeedsManualCheck bool   `json:"needs_manual_check"`
}

type onboardingMessage struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

type onboardingConversation struct {
	ID        string              `json:"id"`
	Messages  []onboardingMessage `json:"messages"`
	Draft     onboardingDraft     `json:"draft"`
	Operation onboardingOperation `json:"operation"`
}

func createTestSession(t *testing.T, ctx context.Context, store *onboarding.Store, userID string) string {
	t.Helper()
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		t.Fatal(err)
	}
	token := hex.EncodeToString(secret[:])
	if err := store.ProvisionSession(ctx, userID, token, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("create test session for %s: %v", userID, err)
	}
	return token
}

func callOnboarding(t *testing.T, app http.Handler, method, path, token string, body any, status int, response any) {
	t.Helper()
	var payload bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&payload).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	request := httptest.NewRequest(method, path, &payload)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, request)
	if recorder.Code != status {
		t.Fatalf("%s %s: status = %d, want %d", method, path, recorder.Code, status)
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), response); err != nil {
		t.Fatalf("%s %s: invalid JSON response: %v", method, path, err)
	}
}

func assertOnboardingError(t *testing.T, app http.Handler, method, path, token string, body any, status int) {
	t.Helper()
	var response struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		RequestID string `json:"request_id"`
	}
	callOnboarding(t, app, method, path, token, body, status, &response)
	if response.Error.Code == "" || response.Error.Message == "" || response.RequestID == "" {
		t.Fatalf("%s %s: expected error code, message and request ID", method, path)
	}
}
