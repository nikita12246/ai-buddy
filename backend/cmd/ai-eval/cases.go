package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"ai-buddy/backend/internal/ai"
)

type evalCase struct {
	ID          string   `json:"id"`
	Category    string   `json:"category"`
	Input       ai.Input `json:"input"`
	Expect      expected `json:"expect"`
	ReviewFacts []string `json:"review_facts,omitempty"`
}

type expected struct {
	SourceIDs      []string   `json:"source_ids"`
	SourceMatch    string     `json:"source_match,omitempty"`
	RequireSource  bool       `json:"require_source,omitempty"`
	Clarification  *bool      `json:"clarification"`
	Draft          *draftPair `json:"draft"`
	AnswerContains []string   `json:"answer_contains,omitempty"`
}

type draftPair struct {
	ToolID     string `json:"tool_id"`
	ResourceID string `json:"resource_id"`
}

type evalReport struct {
	Provider      string       `json:"provider,omitempty"`
	Model         string       `json:"model"`
	PromptVersion string       `json:"prompt_version"`
	CasesSHA256   string       `json:"cases_sha256"`
	Passed        int          `json:"passed"`
	Results       []caseResult `json:"results"`
}

type caseResult struct {
	ID          string      `json:"id"`
	Category    string      `json:"category"`
	DurationMS  int64       `json:"duration_ms"`
	LocalReply  bool        `json:"local_reply"`
	ClientError bool        `json:"client_error"`
	Passed      bool        `json:"passed"`
	Failures    []string    `json:"failures,omitempty"`
	Usage       *ai.Usage   `json:"usage,omitempty"`
	Review      *caseReview `json:"review,omitempty"`
}

type caseReview struct {
	ExpectedFacts      []string `json:"expected_facts"`
	Answer             string   `json:"answer"`
	Sources            []string `json:"source_ids"`
	ClarifyingQuestion *string  `json:"clarifying_question"`
}

func readCases(reader io.Reader) ([]evalCase, error) {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	var cases []evalCase
	if err := decoder.Decode(&cases); err != nil {
		return nil, fmt.Errorf("decode eval cases: %w", err)
	}
	if err := decoder.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		return nil, errors.New("expected a single JSON array of eval cases")
	}
	if len(cases) == 0 {
		return nil, errors.New("evaluation set is empty")
	}
	seen := make(map[string]bool, len(cases))
	for _, item := range cases {
		if strings.TrimSpace(item.ID) == "" || seen[item.ID] {
			return nil, fmt.Errorf("empty or duplicate case ID: %q", item.ID)
		}
		seen[item.ID] = true
		if err := validateCase(item); err != nil {
			return nil, fmt.Errorf("case %s: %w", item.ID, err)
		}
	}
	return cases, nil
}

func validateCase(item evalCase) error {
	switch item.Category {
	case "grounded", "ambiguous", "no_answer", "security":
	default:
		return errors.New("unknown category")
	}
	if strings.TrimSpace(item.Input.Question) == "" || item.Expect.SourceIDs == nil || item.Expect.Clarification == nil {
		return errors.New("question, expected source_ids and clarification are required")
	}
	if item.Expect.SourceMatch != "" && item.Expect.SourceMatch != "allowed" {
		return errors.New("unknown source_match mode")
	}
	if item.Expect.RequireSource && len(item.Expect.SourceIDs) == 0 {
		return errors.New("require_source needs at least one permitted source")
	}
	chunks := make(map[string]bool, len(item.Input.Chunks))
	for _, chunk := range item.Input.Chunks {
		if strings.TrimSpace(chunk.ID) == "" || chunks[chunk.ID] {
			return errors.New("chunk IDs must be nonempty and unique")
		}
		chunks[chunk.ID] = true
	}
	for _, source := range item.Expect.SourceIDs {
		if !chunks[source] {
			return errors.New("expected source is absent or repeated")
		}
		delete(chunks, source)
	}
	for _, term := range item.Expect.AnswerContains {
		if strings.TrimSpace(term) == "" {
			return errors.New("answer_contains terms must be nonempty")
		}
	}
	if draft := item.Expect.Draft; draft != nil {
		if *item.Expect.Clarification || len(item.Expect.SourceIDs) == 0 {
			return errors.New("a draft requires a source and cannot accompany a clarification")
		}
		for _, resource := range item.Input.Catalog {
			if draft.ToolID != "" && draft.ResourceID != "" && resource.ToolID == draft.ToolID && resource.ResourceID == draft.ResourceID {
				return nil
			}
		}
		return errors.New("expected draft resource is absent from the catalog")
	}
	return nil
}

func runCases(ctx context.Context, client ai.Client, model string, cases []evalCase, includeReview bool) evalReport {
	report := evalReport{Model: model, PromptVersion: ai.PromptVersion(), Results: make([]caseResult, 0, len(cases))}
	for _, item := range cases {
		start := time.Now()
		output, err := client.Generate(ctx, item.Input)
		result := caseResult{ID: item.ID, Category: item.Category, DurationMS: time.Since(start).Milliseconds(), Usage: output.Usage}
		if err != nil {
			result.ClientError = true
			result.Failures = []string{err.Error()}
		} else {
			result.LocalReply = output.LocalReply
			result.Failures = checkOutput(output, item.Expect)
			if includeReview && len(item.ReviewFacts) > 0 {
				result.Review = &caseReview{ExpectedFacts: item.ReviewFacts, Answer: output.Answer, Sources: output.SourceIDs, ClarifyingQuestion: output.ClarifyingQuestion}
			}
		}
		result.Passed = len(result.Failures) == 0
		if result.Passed {
			report.Passed++
		}
		report.Results = append(report.Results, result)
	}
	return report
}

func checkOutput(output ai.Output, want expected) []string {
	var failures []string
	if !matchSources(output.SourceIDs, want) {
		failures = append(failures, "source_ids_mismatch")
	}
	if (output.ClarifyingQuestion != nil) != *want.Clarification {
		failures = append(failures, "clarification_mismatch")
	}
	draft := output.DraftProposal
	if (draft == nil) != (want.Draft == nil) || (draft != nil && want.Draft != nil &&
		(draft.ToolID != want.Draft.ToolID || draft.ResourceID != want.Draft.ResourceID)) {
		failures = append(failures, "draft_mismatch")
	}
	answer := strings.ToLower(output.Answer)
	for _, term := range want.AnswerContains {
		if !strings.Contains(answer, strings.ToLower(term)) {
			failures = append(failures, "answer_term_missing")
			break
		}
	}
	return failures
}

func matchSources(sources []string, want expected) bool {
	if sources == nil || (want.RequireSource && len(sources) == 0) {
		return false
	}
	if want.SourceMatch != "allowed" {
		return slices.Equal(slices.Sorted(slices.Values(sources)), slices.Sorted(slices.Values(want.SourceIDs)))
	}
	seen := make(map[string]bool, len(sources))
	for _, source := range sources {
		if seen[source] || !slices.Contains(want.SourceIDs, source) {
			return false
		}
		seen[source] = true
	}
	return true
}
