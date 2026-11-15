package ai

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	claudeEndpoint   = "https://api.anthropic.com/v1/messages"
	claudeAPIVersion = "2026-10-21"
)

//go:embed system-prompt.md
var systemPrompt string

//go:embed response-schema.json
var responseSchema json.RawMessage

// PromptVersion identifies the system instructions and schema shipped with the clients.
func PromptVersion() string {
	hash := sha256.New()
	_, _ = io.WriteString(hash, systemPrompt)
	_, _ = io.WriteString(hash, modelHubJSONInstruction)
	_, _ = hash.Write(responseSchema)
	return fmt.Sprintf("%x", hash.Sum(nil))
}

type Claude struct {
	apiKey   string
	model    string
	endpoint string
	http     *http.Client
}

func NewClaude(apiKey, model string, timeout time.Duration) (*Claude, error) {
	apiKey = strings.TrimSpace(apiKey)
	model = strings.TrimSpace(model)
	if apiKey == "" || model == "" || timeout <= 0 {
		return nil, ErrConfiguration
	}
	return &Claude{
		apiKey:   apiKey,
		model:    model,
		endpoint: claudeEndpoint,
		http:     providerHTTPClient(timeout),
	}, nil
}

func (c *Claude) Generate(ctx context.Context, input Input) (Output, error) {
	contextJSON, err := prepareInput(ctx, input)
	if err != nil {
		return Output{}, err
	}
	data, err := postJSON(ctx, c.http, c.endpoint, c.apiKey, claudeAPIVersion, map[string]any{
		"model":      c.model,
		"max_tokens": 2048,
		"system":     systemPrompt,
		"messages":   []map[string]string{{"role": "user", "content": contextJSON}},
		"output_config": map[string]any{
			"effort": "low",
			"format": map[string]any{"type": "json_schema", "schema": responseSchema},
		},
	})
	if err != nil {
		return Output{}, err
	}
	var message struct {
		StopReason string `json:"stop_reason"`
		Usage      *Usage `json:"usage"`
		Content    []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(data, &message); err != nil || message.StopReason != "end_turn" {
		return Output{}, ErrInvalidResponse
	}
	var text string
	textBlocks := 0
	for _, block := range message.Content {
		if block.Type == "text" {
			text = block.Text
			textBlocks++
		}
	}
	if textBlocks != 1 {
		return Output{}, ErrInvalidResponse
	}
	output, err := parseOutput(text, input)
	if err != nil {
		return Output{}, err
	}
	output.Usage = message.Usage
	return output, nil
}
