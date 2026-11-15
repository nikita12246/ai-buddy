package assistant

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"ai-buddy/backend/internal/ai"
	"ai-buddy/backend/internal/knowledge"
	"ai-buddy/backend/internal/onboarding"
)

const (
	maxInputBytes = 48 * 1024 // Matches the AI adapters' JSON payload limit.
	maxHistory    = 6
)

type Service struct {
	db       *sql.DB
	searcher *knowledge.Searcher
	client   ai.Client
}

func New(db *sql.DB, searcher *knowledge.Searcher, client ai.Client) *Service {
	return &Service{db: db, searcher: searcher, client: client}
}

func (s *Service) Generate(ctx context.Context, actor onboarding.Profile, text string, history []onboarding.Message, operation *onboarding.Operation) (*onboarding.Generation, error) {
	searchActor := knowledge.Actor{TeamID: actor.TeamID, Role: actor.Role}
	roleFollowUp := isRoleFollowUp(text)
	var chunks []knowledge.Chunk
	var err error
	if roleFollowUp {
		var identified bool
		chunks, identified, err = s.searcher.SearchRole(ctx, searchActor, previousTopic(history))
		if _, demo := s.client.(DemoClient); demo && err == nil && len(chunks) == 0 {
			return demoRoleReply(identified), nil
		}
	} else {
		chunks, err = s.searcher.Search(ctx, searchActor, text, 3)
	}
	if err != nil {
		return nil, err
	}
	catalog, err := s.catalog(ctx, actor.TeamID)
	if err != nil {
		return nil, err
	}

	input := ai.Input{
		Question: boundJSONText(text, 24002), // A 4,000-character API question fits even when JSON-escaped.
		History:  userHistory(history),
		Chunks:   make([]ai.Chunk, 0, len(chunks)),
		Catalog:  catalog,
	}
	if operation != nil && operation.DeliveryState == "delivered" && operation.ServiceStatus != nil {
		input.Status = &ai.Status{ServiceStatus: *operation.ServiceStatus, NextStep: operation.NextStep}
	}
	evidence := make([]onboarding.Citation, 0, len(chunks))
	byID := make(map[string]int, len(chunks))
	for i, chunk := range chunks {
		input.Chunks = append(input.Chunks, ai.Chunk{
			ID:    chunk.ChunkID,
			Title: boundJSONText(chunk.Title, 400),
			Text:  boundJSONText(chunk.Text, 4200),
		})
		evidence = append(evidence, onboarding.Citation{
			ChunkID:    chunk.ChunkID,
			DocumentID: chunk.DocumentID,
			Title:      chunk.Title,
			SourceKind: chunk.SourceKind,
			Version:    chunk.Version,
			SourceURL:  chunk.SourceURL,
		})
		byID[chunk.ChunkID] = i
	}
	for {
		payload, err := json.Marshal(input)
		if err != nil {
			return nil, onboarding.ErrAssistantUnavailable
		}
		if len(payload) <= maxInputBytes {
			break
		}
		if len(input.History) == 0 {
			return nil, onboarding.ErrAssistantUnavailable
		}
		input.History = input.History[1:]
	}

	output, err := s.client.Generate(ctx, input)
	if errors.Is(err, ai.ErrTimeout) || errors.Is(err, context.DeadlineExceeded) {
		return nil, onboarding.ErrAssistantTimeout
	}
	if err != nil || strings.TrimSpace(output.Answer) == "" || strings.ContainsRune(output.Answer, 0) ||
		(output.ClarifyingQuestion != nil && strings.ContainsRune(*output.ClarifyingQuestion, 0)) {
		return nil, onboarding.ErrAssistantUnavailable
	}

	result := &onboarding.Generation{
		Answer:             output.Answer,
		ClarifyingQuestion: output.ClarifyingQuestion,
		Sources:            make([]onboarding.Citation, 0, len(output.SourceIDs)),
		Evidence:           evidence,
	}
	proposal := output.DraftProposal
	proposalGrounded := false
	for _, id := range output.SourceIDs {
		i, ok := byID[id]
		if !ok {
			return nil, onboarding.ErrAssistantUnavailable
		}
		delete(byID, id)
		citation := evidence[i]
		excerpt := []rune(strings.Join(strings.Fields(input.Chunks[i].Text), " "))
		if len(excerpt) > 180 {
			citation.Excerpt = strings.TrimSpace(string(excerpt[:179])) + "…"
		} else {
			citation.Excerpt = string(excerpt)
		}
		result.Sources = append(result.Sources, citation)
		if proposal != nil && chunks[i].SourceKind == "demo_policy" &&
			referencesResource(chunks[i].Text, len(input.Chunks[i].Text), proposal.ResourceID) {
			proposalGrounded = true
		}
	}
	if !roleFollowUp && proposalGrounded && output.ClarifyingQuestion == nil && allowedProposal(proposal, catalog) {
		justification := strings.TrimSpace(proposal.Justification)
		if !strings.Contains(text, justification) {
			justification = ""
		}
		result.DraftProposal = &onboarding.DraftProposal{
			ToolID:        proposal.ToolID,
			ResourceID:    proposal.ResourceID,
			Justification: justification,
		}
	}
	return result, nil
}

func isRoleFollowUp(text string) bool {
	question := strings.Join(questionWords(text), " ")
	question = strings.TrimPrefix(question, "а ")
	question = strings.TrimSuffix(question, " для этого")
	switch question {
	case "какая роль", "какая роль нужна", "какая нужна роль", "какую роль выбрать", "какую роль указать", "какую роль нужно указать":
		return true
	}
	return false
}

func previousTopic(history []onboarding.Message) string {
	for i := len(history) - 1; i >= 0; i-- {
		message := history[i]
		if message.Role == "user" && strings.TrimSpace(message.Text) != "" && !isRoleFollowUp(message.Text) {
			return message.Text
		}
	}
	return ""
}

func (s *Service) VerifyEvidence(ctx context.Context, actor onboarding.Profile, evidence []onboarding.Citation) (bool, error) {
	documents := make(map[string][]knowledge.Chunk, len(evidence))
	for _, citation := range evidence {
		chunks, cached := documents[citation.DocumentID]
		if !cached {
			var err error
			chunks, err = s.searcher.GetDocument(ctx, knowledge.Actor{TeamID: actor.TeamID, Role: actor.Role}, citation.DocumentID)
			if errors.Is(err, knowledge.ErrDocumentNotFound) {
				return false, nil
			}
			if err != nil {
				return false, err
			}
			documents[citation.DocumentID] = chunks
		}
		if !slices.ContainsFunc(chunks, func(chunk knowledge.Chunk) bool {
			return chunk.ChunkID == citation.ChunkID && chunk.Version == citation.Version
		}) {
			return false, nil
		}
	}
	return true, nil
}

func (s *Service) catalog(ctx context.Context, teamID string) ([]ai.Resource, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT tool_id, resource_id FROM app_resources
		WHERE team_id = $1 ORDER BY tool_id, resource_id`, teamID)
	if err != nil {
		return nil, fmt.Errorf("query assistant resource catalog: %w", err)
	}
	defer rows.Close()

	var catalog []ai.Resource
	for rows.Next() {
		var resource ai.Resource
		if err := rows.Scan(&resource.ToolID, &resource.ResourceID); err != nil {
			return nil, fmt.Errorf("scan assistant resource catalog: %w", err)
		}
		resource.Name = resource.ResourceID
		catalog = append(catalog, resource)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read assistant resource catalog: %w", err)
	}
	return catalog, nil
}

func userHistory(history []onboarding.Message) []ai.Turn {
	turns := make([]ai.Turn, 0, min(len(history), maxHistory))
	for i := len(history) - 1; i >= 0 && len(turns) < maxHistory; i-- {
		message := history[i]
		if message.Role == "user" && strings.TrimSpace(message.Text) != "" {
			turns = append(turns, ai.Turn{Role: "user", Text: boundJSONText(message.Text, 1200)})
		}
	}
	slices.Reverse(turns)
	return turns
}

// Bound the JSON-encoded size without cutting a UTF-8 character in half.
func boundJSONText(value string, maxBytes int) string {
	encoded, _ := json.Marshal(value)
	if len(encoded) <= maxBytes {
		return value
	}
	runes := []rune(value)
	lower, upper := 0, len(runes)
	for lower < upper {
		mid := lower + (upper-lower+1)/2
		encoded, _ := json.Marshal(string(runes[:mid]))
		if len(encoded) <= maxBytes {
			lower = mid
		} else {
			upper = mid - 1
		}
	}
	return string(runes[:lower])
}

func referencesResource(text string, visibleBytes int, resourceID string) bool {
	if resourceID == "" {
		return false
	}
	for offset := 0; offset < visibleBytes; {
		index := strings.Index(text[offset:visibleBytes], resourceID)
		if index < 0 {
			break
		}
		start := offset + index
		end := start + len(resourceID)
		before, _ := utf8.DecodeLastRuneInString(text[:start])
		after, _ := utf8.DecodeRuneInString(text[end:])
		if !resourceCharacter(before) && !resourceCharacter(after) {
			return true
		}
		offset = end
	}
	return false
}

func resourceCharacter(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsNumber(r) || r == '-' || r == '_' || r == '/'
}

func allowedProposal(proposal *ai.Draft, catalog []ai.Resource) bool {
	justification := strings.TrimSpace(proposal.Justification)
	return !strings.ContainsRune(justification, 0) &&
		utf8.RuneCountInString(justification) <= 600 && slices.ContainsFunc(catalog, func(resource ai.Resource) bool {
		return resource.ToolID == proposal.ToolID && resource.ResourceID == proposal.ResourceID
	})
}
