package ai

import (
	"context"
	"errors"
	"time"
)

var (
	ErrConfiguration   = errors.New("AI provider configuration is invalid")
	ErrInvalidInput    = errors.New("AI input is invalid")
	ErrTimeout         = errors.New("AI request timed out")
	ErrUnavailable     = errors.New("AI provider is temporarily unavailable")
	ErrBalance         = errors.New("AI provider balance is insufficient")
	ErrInvalidResponse = errors.New("AI provider returned an unusable response")
)

// Client proposes a response. Backend code owns authorization and mutations.
type Client interface {
	Generate(context.Context, Input) (Output, error)
}

func NewClient(provider, apiKey, model string, timeout time.Duration) (Client, error) {
	switch provider {
	case "anthropic":
		return NewClaude(apiKey, model, timeout)
	case "modelhub":
		return NewModelHub(apiKey, model, timeout)
	default:
		return nil, ErrConfiguration
	}
}

// Input is assembled by the backend after filtering history, chunks and resources
// for the current user. It carries no session or employee identifiers.
type Input struct {
	Question string     `json:"question"`
	History  []Turn     `json:"history,omitempty"`
	Chunks   []Chunk    `json:"chunks,omitempty"`
	Catalog  []Resource `json:"catalog,omitempty"`
	Draft    *Draft     `json:"draft,omitempty"`
	Status   *Status    `json:"status,omitempty"`
}

type Turn struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

type Chunk struct {
	ID    string `json:"chunk_id"`
	Title string `json:"title"`
	Text  string `json:"text"`
}

type Resource struct {
	ToolID     string `json:"tool_id"`
	ResourceID string `json:"resource_id"`
	Name       string `json:"name"`
}

type Status struct {
	ServiceStatus string `json:"service_status"`
	NextStep      string `json:"next_step"`
}

type Draft struct {
	ToolID        string `json:"tool_id"`
	ResourceID    string `json:"resource_id"`
	Justification string `json:"justification"`
}

type Output struct {
	Answer             string   `json:"answer"`
	SourceIDs          []string `json:"source_ids"`
	ClarifyingQuestion *string  `json:"clarifying_question"`
	DraftProposal      *Draft   `json:"draft_proposal"`
	Usage              *Usage   `json:"-"`
	LocalReply         bool     `json:"-"`
}

type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}
