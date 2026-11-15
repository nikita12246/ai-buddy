package knowledge

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// Actor comes from an authenticated server session.
type Actor struct {
	TeamID string
	Role   string
}

type Chunk struct {
	ChunkID    string  `json:"chunk_id"`
	DocumentID string  `json:"document_id"`
	Title      string  `json:"title"`
	Text       string  `json:"text"`
	SourceURL  *string `json:"source_url"`
	Version    string  `json:"version"`
	SourceID   string  `json:"source_id"`
	SourceKind string  `json:"source_kind"`
}

type Resource struct {
	ToolID     string   `json:"tool_id"`
	ResourceID string   `json:"resource_id"`
	TeamID     string   `json:"team_id"`
	Title      string   `json:"title"`
	Aliases    []string `json:"aliases"`
}

type resourceTerms struct {
	aliases  []string
	terms    []string
	tool     string
	resource string
}

type Searcher struct {
	db        *sql.DB
	resources map[string][]resourceTerms
}

func NewSearcher(db *sql.DB, resources []Resource) *Searcher {
	searcher := &Searcher{db: db, resources: make(map[string][]resourceTerms)}
	for _, resource := range resources {
		if resource.TeamID == "" {
			continue
		}
		entry := resourceTerms{
			tool:     strings.Join(tokenize(resource.ToolID), " "),
			resource: strings.Join(tokenize(resource.ResourceID), " "),
		}
		for _, name := range []string{resource.ToolID, resource.ResourceID, resource.Title} {
			if phrase := strings.Join(tokenize(name), " <-> "); phrase != "" {
				entry.terms = append(entry.terms, phrase)
			}
		}
		for _, alias := range resource.Aliases {
			if normalized := strings.Join(tokenize(alias), " "); normalized != "" {
				entry.aliases = append(entry.aliases, " "+normalized+" ")
			}
		}
		searcher.resources[resource.TeamID] = append(searcher.resources[resource.TeamID], entry)
	}
	return searcher
}

var ErrInvalidActor = errors.New("knowledge access requires an authenticated actor with a team and role")

// ErrDocumentNotFound means the document is missing or has no visible chunks.
var ErrDocumentNotFound = errors.New("knowledge document not found")

const chunkColumns = "chunk_id, document_id, title, text, source_url, version, source_id, source_kind"

// Both queries bind the actor's team and role at positions 2 and 3.
const actorACL = `(scope = 'public' OR (
	scope = 'team' AND team_id = $2 AND $3 = ANY(allowed_roles)
))`

const searchSQL = `
WITH search_query AS (
	SELECT to_tsquery('pg_catalog.russian', $1) AS terms
)
SELECT ` + chunkColumns + `
FROM knowledge_chunks
CROSS JOIN search_query
WHERE search_vector @@ search_query.terms
  AND ` + actorACL + `
ORDER BY ts_rank(search_vector, search_query.terms) DESC, chunk_id COLLATE "C"
LIMIT $4
`

const documentSQL = `
SELECT ` + chunkColumns + `
FROM knowledge_chunks
WHERE document_id = $1
  AND ` + actorACL + `
ORDER BY length(chunk_id), chunk_id COLLATE "C"
`

type DocumentSummary struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Kind  string `json:"kind"`
}

func (s *Searcher) ListDocuments(ctx context.Context, actor Actor) ([]DocumentSummary, error) {
	if !actor.valid() {
		return nil, ErrInvalidActor
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT document_id, title, source_kind FROM (
	SELECT DISTINCT ON (document_id) document_id, title, source_kind
	FROM knowledge_chunks
	WHERE `+strings.NewReplacer("$2", "$1", "$3", "$2").Replace(actorACL)+`
	ORDER BY document_id, length(chunk_id), chunk_id COLLATE "C"
) AS documents ORDER BY title, document_id`, actor.TeamID, actor.Role)
	if err != nil {
		return nil, fmt.Errorf("list knowledge documents: %w", err)
	}
	defer rows.Close()
	result := make([]DocumentSummary, 0)
	for rows.Next() {
		var document DocumentSummary
		if err := rows.Scan(&document.ID, &document.Title, &document.Kind); err != nil {
			return nil, err
		}
		result = append(result, document)
	}
	return result, rows.Err()
}

func (a Actor) valid() bool {
	return strings.TrimSpace(a.TeamID) != "" && strings.TrimSpace(a.Role) != ""
}

func (s *Searcher) Search(ctx context.Context, actor Actor, query string, limit int) ([]Chunk, error) {
	if !actor.valid() {
		return nil, ErrInvalidActor
	}

	terms := s.searchTerms(actor.TeamID, query)
	if terms == "" {
		return []Chunk{}, nil
	}

	limit = min(max(limit, 1), 10)
	return s.queryChunks(ctx, searchSQL, terms, actor.TeamID, actor.Role, limit)
}

// SearchRole finds role instructions for one catalog resource mentioned in the previous question.
// The bool distinguishes an identified resource with no instructions from an ambiguous topic.
func (s *Searcher) SearchRole(ctx context.Context, actor Actor, previousQuestion string) ([]Chunk, bool, error) {
	if !actor.valid() {
		return nil, false, ErrInvalidActor
	}
	phrase := " " + strings.Join(tokenize(previousQuestion), " ") + " "
	for team, resources := range s.resources {
		if team == actor.TeamID {
			continue
		}
		if name := strings.Join(tokenize(team), " "); name != "" && strings.Contains(phrase, " "+name+" ") {
			return nil, false, nil
		}
		for _, entry := range resources {
			if entry.resource != "" && strings.Contains(phrase, " "+entry.resource+" ") {
				return nil, false, nil
			}
		}
	}
	var resource string
	for _, entry := range s.resources[actor.TeamID] {
		if entry.resource == "" || !entry.mentionedIn(phrase) {
			continue
		}
		if resource != "" && resource != entry.resource {
			return nil, false, nil
		}
		resource = entry.resource
	}
	if resource == "" {
		return nil, false, nil
	}
	terms := "(" + strings.ReplaceAll(resource, " ", " <-> ") + ") & роль"
	chunks, err := s.queryChunks(ctx, searchSQL, terms, actor.TeamID, actor.Role, 3)
	return chunks, true, err
}

func (r resourceTerms) mentionedIn(phrase string) bool {
	if strings.Contains(phrase, " "+r.resource+" ") || (r.tool != "" && strings.Contains(phrase, " "+r.tool+" ")) {
		return true
	}
	for _, alias := range r.aliases {
		if strings.Contains(phrase, alias) {
			return true
		}
	}
	return false
}

// GetDocument returns the document chunks visible to the actor.
func (s *Searcher) GetDocument(ctx context.Context, actor Actor, documentID string) ([]Chunk, error) {
	if !actor.valid() {
		return nil, ErrInvalidActor
	}
	if strings.TrimSpace(documentID) == "" {
		return nil, ErrDocumentNotFound
	}
	chunks, err := s.queryChunks(ctx, documentSQL, documentID, actor.TeamID, actor.Role)
	if err != nil {
		return nil, err
	}
	if len(chunks) == 0 {
		return nil, ErrDocumentNotFound
	}
	return chunks, nil
}

func (s *Searcher) queryChunks(ctx context.Context, query string, args ...any) ([]Chunk, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query knowledge chunks: %w", err)
	}
	defer rows.Close()

	chunks := make([]Chunk, 0)
	for rows.Next() {
		var chunk Chunk
		err := rows.Scan(&chunk.ChunkID, &chunk.DocumentID, &chunk.Title, &chunk.Text,
			&chunk.SourceURL, &chunk.Version, &chunk.SourceID, &chunk.SourceKind)
		if err != nil {
			return nil, fmt.Errorf("scan knowledge chunk: %w", err)
		}
		chunks = append(chunks, chunk)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read knowledge chunks: %w", err)
	}
	return chunks, nil
}

func tokenize(value string) []string {
	return strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
}

// These aliases describe public tools and concepts in the corpus.
var publicAliases = []struct {
	aliases []string
	term    string
}{
	{[]string{"инженерные подходы", "инженерным подходам"}, "принципы <-> разработки"},
	{[]string{"сентри"}, "sentry:A"},
	{[]string{"графана", "графане", "графаны", "графану"}, "grafana:A"},
	{[]string{"тдр"}, "tdr:A"},
	{[]string{"техдеп", "тех деп"}, "tech <-> dep"},
	{[]string{"сташ", "сташе", "битбакет", "битбакете"}, "bitbucket:A"},
	{[]string{"збп", "зеро баг полиси"}, "zero <-> bug <-> policy"},
}

func (s *Searcher) searchTerms(teamID, query string) string {
	tokens := tokenize(query)
	phrase := " " + strings.Join(tokens, " ") + " "
	for _, entry := range publicAliases {
		for _, alias := range entry.aliases {
			if strings.Contains(phrase, " "+alias+" ") {
				tokens = append(tokens, entry.term)
				break
			}
		}
	}
	if strings.Contains(phrase, " readme ") &&
		(strings.Contains(phrase, " стажировку ") || strings.Contains(phrase, " стажировки ")) {
		tokens = append(tokens, "первое <-> учебное <-> задание")
	}
	for _, entry := range s.resources[teamID] {
		for _, alias := range entry.aliases {
			if strings.Contains(phrase, alias) {
				tokens = append(tokens, entry.terms...)
				break
			}
		}
	}

	// Build the tsquery from normalized words and catalog phrases. OR keeps extra
	// words in natural-language queries from excluding otherwise relevant matches.
	const maxTerms = 64
	seen := make(map[string]bool)
	terms := make([]string, 0, min(len(tokens), maxTerms))
	for _, token := range tokens {
		if seen[token] {
			continue
		}
		seen[token] = true
		terms = append(terms, token)
		if len(terms) == maxTerms {
			break
		}
	}
	return strings.Join(terms, " | ")
}
