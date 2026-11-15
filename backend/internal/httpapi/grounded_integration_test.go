package httpapi_test

import (
	"context"
	"database/sql"
	"net/http"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"ai-buddy/backend/internal/ai"
	"ai-buddy/backend/internal/assistant"
	"ai-buddy/backend/internal/httpapi"
	"ai-buddy/backend/internal/knowledge"
	"ai-buddy/backend/internal/onboarding"
)

func verifyGroundedChat(t *testing.T, ctx context.Context, db *sql.DB, store *onboarding.Store, atlasToken, orbitToken string) {
	t.Helper()
	schema, err := os.ReadFile("../knowledge/schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, string(schema)); err != nil {
		t.Fatalf("apply knowledge schema: %v", err)
	}

	const (
		atlasChunk   = "integration-atlas-gitlab-chunk"
		atlasDoc     = "integration-atlas-gitlab"
		atlasRepo    = "atlas-training-repo"
		atlasText    = "Учебный доступ к GitLab для команды Atlas: atlas-training-repo. Укажите роль Reporter."
		contextChunk = "integration-atlas-approval-chunk"
		contextDoc   = "integration-atlas-approval"
		contextText  = "Доступ к учебному GitLab Atlas подтверждает наставник."
		orbitChunk   = "integration-orbit-gitlab-chunk"
		orbitDoc     = "integration-orbit-gitlab"
		orbitText    = "Учебный доступ к GitLab для команды Orbit: orbit-training-repo. Укажите роль Reporter."
		reason       = "Нужен для выполнения учебного задания."
		question     = "Как получить доступ к учебному GitLab? " + reason
		answer       = "Запросите доступ к учебному репозиторию GitLab команды Atlas."
	)
	_, err = db.ExecContext(ctx, `
		INSERT INTO knowledge_chunks
			(chunk_id, document_id, title, text, version, source_id, scope, team_id, allowed_roles, source_kind)
		VALUES
			($1, $2, 'GitLab Atlas', $3, 'v1', 'integration-atlas', 'team', 'atlas', ARRAY['intern'], 'demo_policy'),
			($4, $5, 'GitLab Orbit', $6, 'v1', 'integration-orbit', 'team', 'orbit', ARRAY['intern'], 'demo_policy'),
			($7, $8, 'Согласование GitLab Atlas', $9, 'v1', 'integration-approval', 'team', 'atlas', ARRAY['intern'], 'demo_policy')
		ON CONFLICT (chunk_id) DO UPDATE SET version = EXCLUDED.version
	`, atlasChunk, atlasDoc, atlasText, orbitChunk, orbitDoc, orbitText, contextChunk, contextDoc, contextText)
	if err != nil {
		t.Fatalf("seed grounded chat fixtures: %v", err)
	}

	searcher := knowledge.NewSearcher(db, nil)
	calls := 0
	client := groundedClientFunc(func(_ context.Context, input ai.Input) (ai.Output, error) {
		if input.Question == "Помоги мне с чем-то" {
			if len(input.Chunks) != 0 {
				t.Fatalf("expected empty search context, got %+v", input.Chunks)
			}
			clarification := "Какая задача у тебя сейчас?"
			return ai.Output{Answer: "Помогу разобраться со стажировкой.", ClarifyingQuestion: &clarification}, nil
		}
		calls++
		if input.Question != question {
			t.Errorf("model question = %q, want %q", input.Question, question)
		}
		expectedChunks := []ai.Chunk{
			{ID: atlasChunk, Title: "GitLab Atlas", Text: atlasText},
			{ID: contextChunk, Title: "Согласование GitLab Atlas", Text: contextText},
		}
		expectedCatalog := []ai.Resource{{ToolID: "gitlab", ResourceID: atlasRepo, Name: atlasRepo}}
		if len(input.Chunks) != 2 || !slices.Contains(input.Chunks, expectedChunks[0]) ||
			!slices.Contains(input.Chunks, expectedChunks[1]) || !slices.Equal(input.Catalog, expectedCatalog) {
			t.Error("model input must contain only Atlas chunks and the allowed resource")
		}
		if calls == 1 {
			return ai.Output{}, ai.ErrUnavailable
		}
		citationID := atlasChunk
		if calls > 2 {
			citationID = "invented-chunk"
		}
		return ai.Output{
			Answer:    answer,
			SourceIDs: []string{citationID},
			DraftProposal: &ai.Draft{
				ToolID: "gitlab", ResourceID: atlasRepo, Justification: reason,
			},
		}, nil
	})
	app := httpapi.NewIntegratedApplication(store, assistant.New(db, searcher, client), searcher)
	var emptyContextChat struct{ ID string }
	callOnboarding(t, app, http.MethodPost, "/api/conversations", atlasToken, nil, http.StatusCreated, &emptyContextChat)
	var emptyContextReply onboarding.Reply
	callOnboarding(t, app, http.MethodPost, "/api/conversations/"+emptyContextChat.ID+"/messages", atlasToken,
		map[string]string{"client_message_id": "unfocused", "text": "Помоги мне с чем-то"}, http.StatusOK, &emptyContextReply)
	if emptyContextReply.Answer != "Помогу разобраться со стажировкой." || emptyContextReply.ClarifyingQuestion == nil ||
		len(emptyContextReply.SourceIDs) != 0 || emptyContextReply.Draft != nil {
		t.Fatalf("expected a generated clarification with no sources or draft, got %+v", emptyContextReply)
	}

	var document struct {
		ID     string `json:"document_id"`
		Chunks []struct {
			ID   string `json:"chunk_id"`
			Text string `json:"text"`
		} `json:"chunks"`
	}
	callOnboarding(t, app, http.MethodGet, "/api/documents/"+atlasDoc, atlasToken, nil, http.StatusOK, &document)
	if document.ID != atlasDoc || len(document.Chunks) != 1 || document.Chunks[0].ID != atlasChunk || document.Chunks[0].Text != atlasText {
		t.Fatal("document lookup did not return the Atlas fixture")
	}
	assertOnboardingError(t, app, http.MethodGet, "/api/documents/"+orbitDoc, atlasToken, nil, http.StatusNotFound)
	assertOnboardingError(t, app, http.MethodGet, "/api/documents/"+atlasDoc, orbitToken, nil, http.StatusNotFound)

	var created struct {
		ID string `json:"id"`
	}
	callOnboarding(t, app, http.MethodPost, "/api/conversations", atlasToken, nil, http.StatusCreated, &created)
	if created.ID == "" {
		t.Fatal("conversation ID is missing")
	}
	conversationPath := "/api/conversations/" + created.ID
	messagePath := conversationPath + "/messages"
	message := map[string]string{"client_message_id": "grounded-gitlab-question", "text": question}
	assertOnboardingError(t, app, http.MethodPost, messagePath, atlasToken, message, http.StatusServiceUnavailable)
	var failed groundedConversation
	callOnboarding(t, app, http.MethodGet, conversationPath, atlasToken, nil, http.StatusOK, &failed)
	if calls != 1 || len(failed.Messages) != 1 || failed.Messages[0].Role != "user" || failed.Messages[0].Text != question ||
		failed.Draft != nil || failed.Operation != nil {
		t.Fatal("failed generation must save only the original question")
	}
	if failed.PendingMessage == nil || failed.PendingMessage.ClientID != message["client_message_id"] ||
		failed.PendingMessage.Text != question {
		t.Fatal("failed generation must expose the original question and client ID for retry")
	}

	var reply, replay struct {
		Answer    string           `json:"answer"`
		SourceIDs []string         `json:"source_ids"`
		Sources   []groundedSource `json:"sources"`
		Draft     onboardingDraft  `json:"draft"`
	}
	retry := map[string]string{"client_message_id": failed.PendingMessage.ClientID, "text": failed.PendingMessage.Text}
	callOnboarding(t, app, http.MethodPost, messagePath, atlasToken, retry, http.StatusOK, &reply)
	expectedIDs := []string{atlasChunk}
	expectedSources := []groundedSource{{ChunkID: atlasChunk, DocumentID: atlasDoc, Version: "v1"}}
	if calls != 2 || reply.Answer != answer || !slices.Equal(reply.SourceIDs, expectedIDs) ||
		!slices.Equal(reply.Sources, expectedSources) || reply.Draft.ID == "" || reply.Draft.ToolID != "gitlab" ||
		reply.Draft.ResourceID != atlasRepo || reply.Draft.Justification != reason {
		t.Fatal("retry must generate a cited answer and the allowed draft in one model call")
	}
	callOnboarding(t, app, http.MethodPost, messagePath, atlasToken, message, http.StatusOK, &replay)
	if calls != 2 || !reflect.DeepEqual(reply, replay) {
		t.Fatal("replaying a successful message must return the saved reply without calling the model")
	}

	var saved groundedConversation
	callOnboarding(t, app, http.MethodGet, conversationPath, atlasToken, nil, http.StatusOK, &saved)
	if len(saved.Messages) != 2 || saved.Messages[0].Role != "user" || saved.Messages[0].Text != question ||
		saved.Messages[1].Role != "assistant" || saved.Messages[1].Text != answer ||
		!slices.Equal(saved.Messages[1].SourceIDs, expectedIDs) || !slices.Equal(saved.Messages[1].Sources, expectedSources) ||
		saved.Draft == nil || *saved.Draft != reply.Draft || saved.Operation != nil || saved.PendingMessage != nil {
		t.Fatal("conversation must contain one cited exchange and a draft awaiting confirmation")
	}

	var queued onboardingOperation
	callOnboarding(t, app, http.MethodPost, "/api/drafts/"+reply.Draft.ID+"/confirm", atlasToken,
		map[string]int{"version": reply.Draft.Version}, http.StatusOK, &queued)
	if queued.ID == "" || queued.DeliveryState != "queued" || queued.ExternalID != "" || queued.ServiceStatus != "" {
		t.Fatal("the grounded draft must enter the queue only after explicit confirmation")
	}

	if _, err := db.ExecContext(ctx, "UPDATE knowledge_chunks SET version = 'v2' WHERE chunk_id = $1", contextChunk); err != nil {
		t.Fatalf("update uncited input chunk version: %v", err)
	}
	var stale groundedConversation
	callOnboarding(t, app, http.MethodGet, conversationPath, atlasToken, nil, http.StatusOK, &stale)
	if len(stale.Messages) == 0 || stale.Messages[0].Role != "user" || stale.Messages[0].Text != question {
		t.Fatal("source version change must preserve the user's question")
	}
	for _, entry := range stale.Messages {
		if entry.Text == answer || len(entry.SourceIDs) != 0 || len(entry.Sources) != 0 {
			t.Fatal("old answer or citations remained visible after the source version changed")
		}
	}

	message["client_message_id"] = "grounded-invented-citation"
	assertOnboardingError(t, app, http.MethodPost, messagePath, atlasToken, message, http.StatusServiceUnavailable)
	if calls != 3 {
		t.Fatal("a response with an invented source ID must fail after a single model call")
	}

	if _, err := db.ExecContext(ctx, "UPDATE app_users SET role = 'mentor' WHERE id = 'atlas-intern'"); err != nil {
		t.Fatal(err)
	}
	assertOnboardingError(t, app, http.MethodGet, conversationPath, atlasToken, nil, http.StatusNotFound)
	assertOnboardingError(t, app, http.MethodGet, "/api/operations/"+queued.ID, atlasToken, nil, http.StatusNotFound)
	if _, err := db.ExecContext(ctx, "UPDATE app_users SET role = 'intern' WHERE id = 'atlas-intern'"); err != nil {
		t.Fatal(err)
	}

	verifyRoleFollowUp(t, db, store, atlasToken, orbitToken, atlasChunk, orbitChunk)
}

func verifyRoleFollowUp(t *testing.T, db *sql.DB, store *onboarding.Store, atlasToken, orbitToken, atlasChunk, orbitChunk string) {
	t.Helper()
	searcher := knowledge.NewSearcher(db, []knowledge.Resource{
		{TeamID: "atlas", ToolID: "gitlab", ResourceID: "atlas-training-repo"},
		{TeamID: "orbit", ToolID: "gitlab", ResourceID: "orbit-training-repo"},
		{TeamID: "atlas", ToolID: "jira", ResourceID: "atlas-training-tracker"},
	})
	app := httpapi.NewIntegratedApplication(store, assistant.New(db, searcher, assistant.DemoClient{}), searcher)
	for _, test := range []struct {
		name, token, previous, source string
		missingPolicy                 bool
	}{
		{name: "Atlas", token: atlasToken, previous: "Как получить доступ к GitLab?", source: atlasChunk},
		{name: "Orbit", token: orbitToken, previous: "Как получить доступ к GitLab?", source: orbitChunk},
		{name: "no context", token: atlasToken},
		{name: "ambiguous topic", token: atlasToken, previous: "Где инструкции про GitLab и Jira?"},
		{name: "foreign resource", token: atlasToken, previous: "Где GitLab orbit-training-repo?"},
		{name: "foreign team", token: atlasToken, previous: "Где GitLab команды Orbit?"},
		{name: "missing policy", token: atlasToken, previous: "Где Jira?", missingPolicy: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var conversation struct{ ID string }
			callOnboarding(t, app, http.MethodPost, "/api/conversations", test.token, nil, http.StatusCreated, &conversation)
			path := "/api/conversations/" + conversation.ID + "/messages"
			var reply struct {
				Answer        string           `json:"answer"`
				SourceIDs     []string         `json:"source_ids"`
				Clarification *string          `json:"clarifying_question"`
				Draft         *onboardingDraft `json:"draft"`
			}
			if test.previous != "" {
				callOnboarding(t, app, http.MethodPost, path, test.token,
					map[string]string{"client_message_id": "topic", "text": test.previous}, http.StatusOK, &reply)
			}
			callOnboarding(t, app, http.MethodPost, path, test.token,
				map[string]string{"client_message_id": "role", "text": "А какая роль нужна?"}, http.StatusOK, &reply)
			if reply.Draft != nil {
				t.Fatal("a role question must not prepare a request")
			}
			if test.missingPolicy {
				if reply.Clarification != nil || len(reply.SourceIDs) != 0 || !strings.Contains(reply.Answer, "не нашёл") {
					t.Fatalf("expected a missing policy notice without sources, got %+v", reply)
				}
				return
			}
			if test.source == "" {
				if reply.Clarification == nil || len(reply.SourceIDs) != 0 {
					t.Fatalf("expected a clarification without sources, got %+v", reply)
				}
				return
			}
			if reply.Clarification != nil || !strings.Contains(reply.Answer, "Reporter") || !slices.Equal(reply.SourceIDs, []string{test.source}) {
				t.Fatalf("expected the role from the team's own policy, got %+v", reply)
			}
		})
	}
}

type groundedClientFunc func(context.Context, ai.Input) (ai.Output, error)

func (f groundedClientFunc) Generate(ctx context.Context, input ai.Input) (ai.Output, error) {
	return f(ctx, input)
}

type groundedSource struct {
	ChunkID    string `json:"chunk_id"`
	DocumentID string `json:"document_id"`
	Version    string `json:"version"`
}

type groundedConversation struct {
	Messages []struct {
		Role      string           `json:"role"`
		Text      string           `json:"text"`
		SourceIDs []string         `json:"source_ids"`
		Sources   []groundedSource `json:"sources"`
	} `json:"messages"`
	Draft          *onboardingDraft           `json:"draft"`
	Operation      *onboardingOperation       `json:"operation"`
	PendingMessage *onboarding.PendingMessage `json:"pending_message"`
}
