package knowledge

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
)

func TestSearchTerms(t *testing.T) {
	searcher := NewSearcher(nil, []Resource{
		{TeamID: "atlas", ToolID: "GitLab", ResourceID: "atlas_repo", Title: "Хранилище", Aliases: []string{"Гит Лаб"}},
		{TeamID: "orbit", ToolID: "GitLab", ResourceID: "orbit_secret", Aliases: []string{"Гит Лаб"}},
	})
	tests := []struct {
		query string
		want  string
	}{
		{"Где ГИТ-ЛАБ?", "где | гит | лаб | gitlab | atlas <-> repo | хранилище"},
		{"супергит лаб", "супергит | лаб"},
		{"гит лаборатория", "гит | лаборатория"},
		{"Гит | гит <-> ! : *", "гит"},
		{"Материалы по инженерным подходам", "материалы | по | инженерным | подходам | принципы <-> разработки"},
	}
	for _, test := range tests {
		t.Run(test.query, func(t *testing.T) {
			if got := searcher.searchTerms("atlas", test.query); got != test.want {
				t.Fatalf("terms = %q, want %q", got, test.want)
			}
		})
	}
}

func TestSearchSkipsDatabaseWithoutActorOrTerms(t *testing.T) {
	searcher := NewSearcher(nil, nil)
	ctx := context.Background()
	for _, actor := range []Actor{{TeamID: "atlas"}, {Role: "intern"}, {TeamID: " ", Role: "intern"}} {
		if _, err := searcher.Search(ctx, actor, "gitlab", 5); !errors.Is(err, ErrInvalidActor) {
			t.Fatalf("actor %+v returned %v, want ErrInvalidActor", actor, err)
		}
	}
	chunks, err := searcher.Search(ctx, Actor{TeamID: "atlas", Role: "intern"}, "?!", 5)
	if err != nil || chunks == nil || len(chunks) != 0 {
		t.Fatalf("empty search returned %#v, %v", chunks, err)
	}
}

func TestSearchBindsActorAndLimitsAndScansNullableURL(t *testing.T) {
	for _, test := range []struct {
		requested int
		want      int64
	}{
		{-5, 1},
		{5, 5},
		{50, 10},
	} {
		db := sql.OpenDB(queryStub{query: func(query string, args []driver.NamedValue) (driver.Rows, error) {
			want := []any{"доступ | gitlab", "atlas", "intern", test.want}
			got := make([]any, 0, len(args))
			for _, arg := range args {
				got = append(got, arg.Value)
			}
			if !reflect.DeepEqual(got, want) || strings.Contains(query, "atlas") || strings.Contains(query, "gitlab") {
				t.Fatalf("query %q, args %#v, want bound args %#v", query, got, want)
			}
			return &chunkRows{}, nil
		}})
		t.Cleanup(func() { _ = db.Close() })
		chunks, err := NewSearcher(db, nil).Search(context.Background(), Actor{TeamID: "atlas", Role: "intern"}, "доступ GitLab", test.requested)
		if err != nil {
			t.Fatal(err)
		}
		want := []Chunk{{ChunkID: "chunk", DocumentID: "doc", Title: "GitLab", Text: "body", Version: "1", SourceID: "demo", SourceKind: "demo_policy"}}
		if !reflect.DeepEqual(chunks, want) {
			t.Fatalf("chunks = %#v, want %#v", chunks, want)
		}
	}
}

func TestGetDocumentBindsDocumentIDAndAppliesSearchACL(t *testing.T) {
	documentID := "doc' OR '1' = '1"
	db := sql.OpenDB(queryStub{query: func(query string, args []driver.NamedValue) (driver.Rows, error) {
		want := []driver.NamedValue{
			{Ordinal: 1, Value: documentID},
			{Ordinal: 2, Value: "atlas"},
			{Ordinal: 3, Value: "intern"},
		}
		if !reflect.DeepEqual(args, want) || strings.Contains(query, documentID) {
			t.Fatalf("query %q, args %#v, want bound args %#v", query, args, want)
		}
		if !strings.Contains(query, "WHERE document_id = $1\n  AND "+actorACL) ||
			!strings.Contains(searchSQL, "AND "+actorACL) ||
			!strings.Contains(actorACL, "scope = 'team' AND team_id = $2 AND $3 = ANY(allowed_roles)") {
			t.Fatalf("document and search SQL must use the same scoped predicate: %q", query)
		}
		return &chunkRows{}, nil
	}})
	t.Cleanup(func() { _ = db.Close() })

	chunks, err := NewSearcher(db, nil).GetDocument(context.Background(), Actor{TeamID: "atlas", Role: "intern"}, documentID)
	if err != nil || len(chunks) != 1 || chunks[0].DocumentID != "doc" {
		t.Fatalf("GetDocument returned %#v, %v", chunks, err)
	}
}

func TestGetDocumentUnavailable(t *testing.T) {
	db := sql.OpenDB(queryStub{query: func(string, []driver.NamedValue) (driver.Rows, error) {
		return &chunkRows{read: true}, nil
	}})
	t.Cleanup(func() { _ = db.Close() })
	actor := Actor{TeamID: "atlas", Role: "intern"}
	for _, id := range []string{"missing", "orbit-only"} {
		chunks, err := NewSearcher(db, nil).GetDocument(context.Background(), actor, id)
		if chunks != nil || !errors.Is(err, ErrDocumentNotFound) {
			t.Fatalf("GetDocument(%q) returned %#v, %v", id, chunks, err)
		}
	}
}

func TestGetDocumentSkipsDatabaseWithInvalidActorOrBlankID(t *testing.T) {
	searcher := NewSearcher(nil, nil)
	ctx := context.Background()
	if _, err := searcher.GetDocument(ctx, Actor{TeamID: "atlas"}, "doc"); !errors.Is(err, ErrInvalidActor) {
		t.Fatalf("incomplete actor returned %v", err)
	}
	if _, err := searcher.GetDocument(ctx, Actor{TeamID: "atlas", Role: "intern"}, " "); !errors.Is(err, ErrDocumentNotFound) {
		t.Fatalf("blank document ID returned %v", err)
	}
}

type queryStub struct {
	query func(string, []driver.NamedValue) (driver.Rows, error)
}

func (s queryStub) Connect(context.Context) (driver.Conn, error) { return s, nil }
func (s queryStub) Driver() driver.Driver                        { return nil }
func (s queryStub) Close() error                                 { return nil }
func (s queryStub) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected Prepare")
}
func (s queryStub) Begin() (driver.Tx, error) {
	return nil, errors.New("unexpected Begin")
}
func (s queryStub) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	return s.query(query, args)
}

type chunkRows struct {
	read bool
}

func (r *chunkRows) Columns() []string {
	return []string{"chunk_id", "document_id", "title", "text", "source_url", "version", "source_id", "source_kind"}
}

func (r *chunkRows) Close() error { return nil }

func (r *chunkRows) Next(values []driver.Value) error {
	if r.read {
		return io.EOF
	}
	r.read = true
	copy(values, []driver.Value{"chunk", "doc", "GitLab", "body", nil, "1", "demo", "demo_policy"})
	return nil
}
