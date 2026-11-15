package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestModelHubUsesOwnEndpointAndValidatesResponse(t *testing.T) {
	client, err := NewClient("modelhub", "test-gateway-credential", "claude-sonnet-5-5", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	hub := client.(*ModelHub)
	hub.http.Transport = fakeTransport(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != modelHubEndpoint || request.Header.Get("Authorization") != "Bearer test-gateway-credential" || request.Header.Get("anthropic-version") != "" {
			t.Fatal("unexpected gateway endpoint or authentication")
		}
		var payload struct {
			Model    string                           `json:"model"`
			Messages []struct{ Role, Content string } `json:"messages"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil || payload.Model != "claude-sonnet-5-5" ||
			len(payload.Messages) != 2 || payload.Messages[0].Role != "system" || !strings.Contains(payload.Messages[0].Content, "draft_proposal") ||
			payload.Messages[1].Role != "user" || !strings.Contains(payload.Messages[1].Content, "gitlab-01") {
			t.Fatal("unexpected chat completions payload")
		}
		reply, _ := json.Marshal(map[string]any{
			"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]string{"content": validReply}}},
			"usage":   map[string]int{"prompt_tokens": 20, "completion_tokens": 10},
		})
		return upstream(http.StatusOK, string(reply)), nil
	})
	output, err := client.Generate(context.Background(), sampleInput())
	if err != nil || len(output.SourceIDs) != 1 || output.SourceIDs[0] != "gitlab-01" || output.Usage == nil || output.Usage.InputTokens != 20 || output.Usage.OutputTokens != 10 {
		t.Fatalf("unexpected gateway output: %+v, %v", output, err)
	}
}

func TestModelHubReportsBalanceWithoutReturningProviderBody(t *testing.T) {
	client, err := NewModelHub("test-gateway-credential", "claude-sonnet-5-5", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	client.http.Transport = fakeTransport(func(*http.Request) (*http.Response, error) {
		return upstream(http.StatusPaymentRequired, "private provider response"), nil
	})
	if output, err := client.Generate(context.Background(), sampleInput()); err != ErrBalance || output.Answer != "" {
		t.Fatalf("unexpected gateway error: %+v, %v", output, err)
	}
}

func TestModelHubGeneratesReplyWithoutSources(t *testing.T) {
	client, err := NewModelHub("test-gateway-credential", "gpt-6.1-sol", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	const question = "Помоги мне с чем-то"
	const answer = "Могу помочь разобраться с задачей или доступом."
	const clarification = "С чего начнём?"
	calls := 0
	client.http.Transport = fakeTransport(func(request *http.Request) (*http.Response, error) {
		calls++
		var payload struct {
			Messages []struct{ Role, Content string } `json:"messages"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil || len(payload.Messages) != 2 {
			t.Fatal("expected a chat completions request")
		}
		var input Input
		if err := json.Unmarshal([]byte(payload.Messages[1].Content), &input); err != nil ||
			input.Question != question || len(input.Chunks) != 0 || input.Status != nil {
			t.Fatalf("unexpected input for the model: %+v, %v", input, err)
		}
		content := `{"answer":"` + answer + `","source_ids":[],"clarifying_question":"` + clarification + `","draft_proposal":null}`
		reply, _ := json.Marshal(map[string]any{
			"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]string{"content": content}}},
			"usage":   map[string]int{"prompt_tokens": 15, "completion_tokens": 8},
		})
		return upstream(http.StatusOK, string(reply)), nil
	})
	output, err := client.Generate(context.Background(), Input{Question: question})
	if err != nil || calls != 1 || output.Answer != answer || output.ClarifyingQuestion == nil ||
		*output.ClarifyingQuestion != clarification || len(output.SourceIDs) != 0 || output.DraftProposal != nil ||
		output.Usage == nil || output.Usage.InputTokens != 15 || output.Usage.OutputTokens != 8 {
		t.Fatalf("expected a billable provider path with a generated reply, got %d calls, %+v, %v", calls, output, err)
	}
}
