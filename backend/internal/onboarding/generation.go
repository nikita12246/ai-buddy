package onboarding

import (
	"context"
	"errors"
)

var (
	ErrAssistantUnavailable = errors.New("assistant is unavailable")
	ErrAssistantTimeout     = errors.New("assistant response timed out")
	ErrGenerationInProgress = errors.New("a response is already being generated")
)

type Citation struct {
	ChunkID    string  `json:"chunk_id"`
	DocumentID string  `json:"document_id"`
	Title      string  `json:"title"`
	SourceURL  *string `json:"source_url"`
	SourceKind string  `json:"source_kind"`
	Version    string  `json:"version"`
	Excerpt    string  `json:"excerpt,omitempty"`
}

type DraftProposal struct {
	ToolID, ResourceID, Justification string
}

type Generation struct {
	Answer             string
	ClarifyingQuestion *string
	Sources            []Citation
	Evidence           []Citation
	DraftProposal      *DraftProposal
}

type Answerer interface {
	Generate(context.Context, Profile, string, []Message, *Operation) (*Generation, error)
	VerifyEvidence(context.Context, Profile, []Citation) (bool, error)
}

type replyMetadata struct {
	Sources            []Citation `json:"sources,omitempty"`
	ClarifyingQuestion *string    `json:"clarifying_question,omitempty"`
	Evidence           []Citation `json:"evidence,omitempty"`
}

func sourceIDs(sources []Citation) []string {
	ids := make([]string, 0, len(sources))
	for _, source := range sources {
		ids = append(ids, source.ChunkID)
	}
	return ids
}
