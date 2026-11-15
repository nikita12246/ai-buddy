package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type fakeTransport func(*http.Request) (*http.Response, error)

func (f fakeTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

const validReply = `{"answer":"Укажи проект.","source_ids":["gitlab-01"],"clarifying_question":"Какой проект?","draft_proposal":null}`

func sampleInput() Input {
	return Input{
		Question: "Как получить доступ к GitLab?",
		Chunks:   []Chunk{{ID: "gitlab-01", Text: "Укажи проект и цель доступа."}},
		Catalog:  []Resource{{ToolID: "gitlab", ResourceID: "training-api"}},
	}
}

func testClient(t *testing.T, transport fakeTransport) *Claude {
	t.Helper()
	client, err := NewClaude("test-credential", "claude-test", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	client.http.Transport = transport
	return client
}

func upstream(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}
}

func messageJSON(t *testing.T, reason, text string) string {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"stop_reason": reason,
		"content":     []map[string]string{{"type": "text", "text": text}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestClaudeRequestAndAnswer(t *testing.T) {
	client := testClient(t, func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost || request.URL.String() != claudeEndpoint ||
			request.Header.Get("Authorization") != "Bearer test-credential" ||
			request.Header.Get("anthropic-version") != claudeAPIVersion {
			t.Fatal("unexpected Claude endpoint or headers")
		}
		var payload struct {
			Model        string                           `json:"model"`
			Messages     []struct{ Role, Content string } `json:"messages"`
			OutputConfig struct {
				Format struct{ Type string } `json:"format"`
			} `json:"output_config"`
			Tools json.RawMessage `json:"tools"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.Model != "claude-test" || payload.OutputConfig.Format.Type != "json_schema" ||
			len(payload.Messages) != 1 || payload.Messages[0].Role != "user" || payload.Tools != nil {
			t.Fatal("expected a single user message, structured output and no tools")
		}
		var input Input
		if err := json.Unmarshal([]byte(payload.Messages[0].Content), &input); err != nil ||
			input.Question != sampleInput().Question || len(input.Chunks) != 1 || input.Chunks[0].ID != "gitlab-01" {
			t.Fatal("unexpected context sent to Claude")
		}
		return upstream(http.StatusOK, messageJSON(t, "end_turn", validReply)), nil
	})
	output, err := client.Generate(context.Background(), sampleInput())
	if err != nil || len(output.SourceIDs) != 1 || output.SourceIDs[0] != "gitlab-01" ||
		output.ClarifyingQuestion == nil || output.DraftProposal != nil {
		t.Fatalf("expected grounded clarification, got %+v, %v", output, err)
	}
}

func TestClaudeGeneratesClarificationWithoutSources(t *testing.T) {
	const clarification = "С каким инструментом нужна помощь?"
	client := testClient(t, func(*http.Request) (*http.Response, error) {
		return upstream(http.StatusOK, messageJSON(t, "end_turn",
			`{"answer":"Помогу разобраться.","source_ids":[],"clarifying_question":"`+clarification+`","draft_proposal":null}`)), nil
	})
	output, err := client.Generate(context.Background(), Input{Question: "Неизвестный инструмент"})
	if err != nil || output.Answer != "Помогу разобраться." || output.ClarifyingQuestion == nil ||
		*output.ClarifyingQuestion != clarification || len(output.SourceIDs) != 0 || output.DraftProposal != nil {
		t.Fatalf("expected a generated clarification without sources, got %+v, %v", output, err)
	}
}

func TestClaudeFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{"auth", 401, "private upstream error", ErrConfiguration},
		{"rate limit", 429, "private upstream error", ErrUnavailable},
		{"overloaded", 529, "private upstream error", ErrUnavailable},
		{"malformed envelope", 200, "not JSON", ErrInvalidResponse},
		{"truncated", 200, messageJSON(t, "max_tokens", validReply), ErrInvalidResponse},
		{"refused", 200, messageJSON(t, "refusal", validReply), ErrInvalidResponse},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := testClient(t, func(*http.Request) (*http.Response, error) {
				calls++
				return upstream(tc.status, tc.body), nil
			})
			output, err := client.Generate(context.Background(), sampleInput())
			if err != tc.want || output.Answer != "" || output.DraftProposal != nil || calls != 1 {
				t.Fatalf("expected one attempt and no partial result, got %d, %+v, %v", calls, output, err)
			}
		})
	}
}

func TestClaudeTimeoutAndCancellation(t *testing.T) {
	client := testClient(t, func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})
	client.http.Timeout = 30 * time.Millisecond
	if _, err := client.Generate(context.Background(), sampleInput()); !errors.Is(err, ErrTimeout) {
		t.Fatalf("expected timeout, got %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Generate(ctx, sampleInput()); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}

func TestOutputValidation(t *testing.T) {
	const validDraftReply = `{"answer":"Проверь черновик.","source_ids":["gitlab-01"],"clarifying_question":null,"draft_proposal":{"tool_id":"gitlab","resource_id":"training-api","justification":"Исправление бага"}}`
	for _, tc := range []struct{ name, text string }{
		{"missing required field", strings.Replace(validReply, `,"draft_proposal":null`, "", 1)},
		{"unknown field", strings.Replace(validReply, `"draft_proposal":null`, `"draft_proposal":null,"execute":true`, 1)},
		{"unknown source", strings.Replace(validReply, "gitlab-01", "closed-source", 1)},
		{"duplicate source", strings.Replace(validReply, `["gitlab-01"]`, `["gitlab-01","gitlab-01"]`, 1)},
		{"null source list", strings.Replace(validReply, `["gitlab-01"]`, "null", 1)},
		{"wrong resource", strings.Replace(validDraftReply, "training-api", "admin", 1)},
		{"uncited draft", strings.Replace(validDraftReply, `["gitlab-01"]`, "[]", 1)},
		{"blank reason", strings.Replace(validDraftReply, "Исправление бага", "  ", 1)},
		{"draft and clarification", strings.Replace(validDraftReply, `"clarifying_question":null`, `"clarifying_question":"Какой проект?"`, 1)},
		{"trailing JSON", validReply + "{}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if output, err := parseOutput(tc.text, sampleInput()); err != ErrInvalidResponse || output.DraftProposal != nil {
				t.Fatalf("expected rejection, got %+v, %v", output, err)
			}
		})
	}
	if output, err := parseOutput(validDraftReply, sampleInput()); err != nil || output.DraftProposal == nil {
		t.Fatalf("expected accepted draft proposal, got %+v, %v", output, err)
	}
}
