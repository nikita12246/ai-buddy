package ai

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

func parseOutput(text string, input Input) (Output, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text), &fields); err != nil || len(fields) != 4 ||
		fields["answer"] == nil || fields["source_ids"] == nil ||
		fields["clarifying_question"] == nil || fields["draft_proposal"] == nil {
		return Output{}, ErrInvalidResponse
	}
	var output Output
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&output); err != nil || !validText(output.Answer, 2400) || output.SourceIDs == nil {
		return Output{}, ErrInvalidResponse
	}
	if output.ClarifyingQuestion != nil && (!validText(*output.ClarifyingQuestion, 400) || output.DraftProposal != nil) {
		return Output{}, ErrInvalidResponse
	}
	if !validSources(output.SourceIDs, input.Chunks) {
		return Output{}, ErrInvalidResponse
	}
	if output.DraftProposal != nil && (len(output.SourceIDs) == 0 || !validDraft(*output.DraftProposal, input.Catalog)) {
		return Output{}, ErrInvalidResponse
	}
	return output, nil
}

func validText(text string, maxChars int) bool {
	return strings.TrimSpace(text) != "" && utf8.RuneCountInString(text) <= maxChars
}

func validSources(ids []string, chunks []Chunk) bool {
	available := make(map[string]bool, len(chunks))
	for _, chunk := range chunks {
		if strings.TrimSpace(chunk.ID) != "" {
			available[chunk.ID] = true
		}
	}
	for _, id := range ids {
		if !available[id] {
			return false
		}
		delete(available, id)
	}
	return true
}

func validDraft(draft Draft, catalog []Resource) bool {
	if !validText(draft.ToolID, 128) || !validText(draft.ResourceID, 128) || !validText(draft.Justification, 600) {
		return false
	}
	for _, resource := range catalog {
		if resource.ToolID == draft.ToolID && resource.ResourceID == draft.ResourceID {
			return true
		}
	}
	return false
}
