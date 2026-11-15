package knowledge

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type corpusCase struct {
	ID       string `json:"id"`
	Category string `json:"category"`
	Actor    struct {
		TeamID string `json:"team_id"`
		Role   string `json:"role"`
	} `json:"actor"`
	Question    string   `json:"question"`
	Sources     []string `json:"allowed_source_ids"`
	Forbidden   []string `json:"forbidden_source_ids"`
	FirstSource string   `json:"first_source_id"`
}

func loadCorpusFixture(t *testing.T, path string, destination any) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "..", path))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, destination); err != nil {
		t.Fatal(err)
	}
}

// Run against a seeded development database. The checks issue only SELECTs.
func TestCorpusRetrieval(t *testing.T) {
	databaseURL := os.Getenv("KNOWLEDGE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set KNOWLEDGE_TEST_DATABASE_URL to run the PostgreSQL corpus check")
	}

	var resources []Resource
	loadCorpusFixture(t, "data/synthetic/resources.json", &resources)
	searcher := NewSearcher(nil, resources)

	var demoCases, publicCases, expansionCases, holdoutCases, conversationalCases []corpusCase
	loadCorpusFixture(t, "evals/data-cases.json", &demoCases)
	loadCorpusFixture(t, "evals/retrieval-cases.json", &publicCases)

	loadCorpusFixture(t, "evals/retrieval-expansion.json", &expansionCases)
	loadCorpusFixture(t, "evals/retrieval-holdout.json", &holdoutCases)
	loadCorpusFixture(t, "evals/retrieval-conversational.json", &conversationalCases)

	for _, item := range slices.Concat(demoCases, publicCases, expansionCases, holdoutCases, conversationalCases) {
		if item.Category != "" && item.Category != "grounded" && item.Category != "security" {
			continue
		}
		t.Run(item.ID, func(t *testing.T) {
			limit := "3"
			if item.Category == "security" {
				limit = "10"
			}
			got := queryCorpusIDs(t, databaseURL, searchSQL, searcher.searchTerms(item.Actor.TeamID, item.Question), item.Actor.TeamID, item.Actor.Role, limit)
			t.Logf("search results: %v", got)
			if item.FirstSource != "" && (len(got) == 0 || got[0] != item.FirstSource) {
				t.Errorf("expected %s in first place, got %v", item.FirstSource, got)
			}
			for _, source := range item.Forbidden {
				if slices.Contains(got, source) {
					t.Errorf("restricted chunk %s appeared in search", source)
				}
			}
			for _, source := range item.Sources {
				if !slices.Contains(got, source) {
					t.Errorf("expected %s in top 3, got %v", source, got)
				}
			}
		})
	}

	t.Run("document ACL", func(t *testing.T) {
		for _, actor := range []Actor{{TeamID: "orbit", Role: "intern"}, {TeamID: "atlas", Role: "visitor"}} {
			if got := queryCorpusIDs(t, databaseURL, documentSQL, "atlas-repo", actor.TeamID, actor.Role, "3"); len(got) != 0 {
				t.Errorf("document visible to %+v: %v", actor, got)
			}
		}
		if got := queryCorpusIDs(t, databaseURL, documentSQL, "atlas-repo", "atlas", "intern", "3"); !slices.Equal(got, []string{"atlas-repo-r1"}) {
			t.Errorf("owner team document returned %v", got)
		}
	})
}

func queryCorpusIDs(t *testing.T, databaseURL, query, value, team, role, limit string) []string {
	t.Helper()
	// psql's :'name' syntax quotes values as SQL literals.
	query = strings.NewReplacer("$1", ":'value'", "$2", ":'team'", "$3", ":'role'", "$4", limit).Replace(query)
	command := exec.Command("psql", "-XAtq", "-d", databaseURL, "-v", "ON_ERROR_STOP=1",
		"-v", "value="+value, "-v", "team="+team, "-v", "role="+role)
	command.Stdin = strings.NewReader("SELECT chunk_id FROM (" + query + ") AS result;")
	output, err := command.Output()
	if err != nil {
		t.Fatalf("psql query failed: %v", err)
	}
	return strings.Fields(string(output))
}
