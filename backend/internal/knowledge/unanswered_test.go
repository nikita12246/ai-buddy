package knowledge

import (
	"encoding/json"
	"os"
	"slices"
	"testing"
)

type unansweredCase struct {
	corpusCase
	ContextSources []string `json:"context_source_ids"`
}

type contextResult struct {
	ID       string   `json:"id"`
	ChunkIDs []string `json:"chunk_ids"`
}

// A relevant search result can still lack the fact the question asks for.
func TestCorpusUnansweredContext(t *testing.T) {
	databaseURL := os.Getenv("KNOWLEDGE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set KNOWLEDGE_TEST_DATABASE_URL to run the PostgreSQL corpus check")
	}
	var resources []Resource
	var cases []unansweredCase
	loadCorpusFixture(t, "data/synthetic/resources.json", &resources)
	loadCorpusFixture(t, "evals/unanswered-cases.json", &cases)
	searcher := NewSearcher(nil, resources)
	results := make([]contextResult, 0, len(cases))
	for _, item := range cases {
		t.Run(item.ID, func(t *testing.T) {
			terms := searcher.searchTerms(item.Actor.TeamID, item.Question)
			got := queryCorpusIDs(t, databaseURL, searchSQL, terms, item.Actor.TeamID, item.Actor.Role, "3")
			t.Logf("search results: %v", got)
			if !slices.ContainsFunc(got, func(id string) bool { return slices.Contains(item.ContextSources, id) }) {
				t.Fatalf("expected a related chunk from %v in top 3, got %v", item.ContextSources, got)
			}
			results = append(results, contextResult{ID: item.ID, ChunkIDs: got})
		})
	}
	if path := os.Getenv("KNOWLEDGE_UNANSWERED_CONTEXT_OUT"); path != "" && !t.Failed() {
		body, err := json.MarshalIndent(results, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(body, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
}
