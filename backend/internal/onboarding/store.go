package onboarding

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var (
	ErrNotFound        = errors.New("object not found")
	ErrVersionConflict = errors.New("draft version conflict")
	ErrMessageConflict = errors.New("message id already used for different text")
	ErrDraftConfirmed  = errors.New("draft already confirmed")
	ErrResource        = errors.New("resource is not available to this team")
	ErrJustification   = errors.New("justification is required")
)

type Profile struct {
	ID     string `json:"id"`
	TeamID string `json:"team_id"`
	Role   string `json:"role"`
	Name   string `json:"name"`
}

type Message struct {
	ID                 int64      `json:"id"`
	Role               string     `json:"role"`
	Text               string     `json:"text"`
	CreatedAt          time.Time  `json:"created_at"`
	SourceIDs          []string   `json:"source_ids,omitempty"`
	Sources            []Citation `json:"sources,omitempty"`
	ClarifyingQuestion *string    `json:"clarifying_question,omitempty"`
	Evidence           []Citation `json:"-"`
}

type Draft struct {
	ID            string `json:"id"`
	ToolID        string `json:"tool_id"`
	ResourceID    string `json:"resource_id"`
	Justification string `json:"justification"`
	Version       int    `json:"version"`
}

type DraftUpdate struct {
	ExpectedVersion int     `json:"expected_version"`
	ToolID          *string `json:"tool_id"`
	ResourceID      *string `json:"resource_id"`
	Justification   *string `json:"justification"`
}

type Operation struct {
	ID               string  `json:"id"`
	ConfirmedVersion int     `json:"confirmed_version"`
	DeliveryState    string  `json:"delivery_state"`
	ExternalID       *string `json:"external_id"`
	ServiceStatus    *string `json:"service_status"`
	NextStep         string  `json:"next_step"`
	NeedsManualCheck bool    `json:"needs_manual_check"`
}

type AccessRequest struct {
	Operation
	ToolID     string `json:"tool_id"`
	ResourceID string `json:"resource_id"`
}

type Conversation struct {
	ID             string          `json:"id"`
	Messages       []Message       `json:"messages"`
	Draft          *Draft          `json:"draft"`
	Operation      *Operation      `json:"operation"`
	PendingMessage *PendingMessage `json:"pending_message,omitempty"`
}

type PendingMessage struct {
	ClientID string `json:"client_message_id"`
	Text     string `json:"text"`
}

type Reply struct {
	Answer             string     `json:"answer"`
	SourceIDs          []string   `json:"source_ids"`
	Sources            []Citation `json:"sources"`
	Evidence           []Citation `json:"-"`
	ClarifyingQuestion *string    `json:"clarifying_question"`
	Draft              *Draft     `json:"draft"`
}

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) ProvisionSession(ctx context.Context, userID, token string, expires time.Time) error {
	hash := sha256.Sum256([]byte(token))
	_, err := s.db.ExecContext(ctx, `INSERT INTO app_sessions (user_id, token_hash, expires_at)
		VALUES ($1, $2, $3) ON CONFLICT (user_id) DO UPDATE
		SET token_hash = EXCLUDED.token_hash, expires_at = EXCLUDED.expires_at`, userID, hash[:], expires)
	return err
}

func (s *Store) Authenticate(ctx context.Context, token string) (Profile, error) {
	hash := sha256.Sum256([]byte(token))
	var actor Profile
	err := s.db.QueryRowContext(ctx, `SELECT u.id, u.team_id, u.role, u.name
		FROM app_sessions s JOIN app_users u ON u.id = s.user_id
		WHERE s.token_hash = $1 AND s.expires_at > now()`, hash[:]).
		Scan(&actor.ID, &actor.TeamID, &actor.Role, &actor.Name)
	return actor, notFound(err)
}

func (s *Store) StartGuestSession(ctx context.Context) (Profile, string, error) {
	actor := Profile{ID: "guest-" + rand.Text(), TeamID: "atlas", Role: "intern", Name: "Стажёр Atlas"}
	token := rand.Text()
	hash := sha256.Sum256([]byte(token))
	_, err := s.db.ExecContext(ctx, `
		WITH guest AS (
			INSERT INTO app_users (id, team_id, role, name) VALUES ($1, $2, $3, $4) RETURNING id
		)
		INSERT INTO app_sessions (user_id, token_hash, expires_at)
		SELECT id, $5, now() + interval '7 days' FROM guest`,
		actor.ID, actor.TeamID, actor.Role, actor.Name, hash[:])
	return actor, token, err
}

func (s *Store) CreateConversation(ctx context.Context, actor Profile) (string, error) {
	id := rand.Text()
	_, err := s.db.ExecContext(ctx, `INSERT INTO app_conversations (id, owner_id, owner_team, owner_role) VALUES ($1, $2, $3, $4)`, id, actor.ID, actor.TeamID, actor.Role)
	return id, err
}

func notFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

const draftColumns = `d.id, d.tool_id, d.resource_id, d.justification, d.version`
const operationColumns = `o.id, o.confirmed_version, o.delivery_state, o.external_id, o.service_status, o.attempts,
	(o.lease_until IS NOT NULL AND o.lease_until > now())`

func readDraft(row *sql.Row) (*Draft, error) {
	var d Draft
	if err := row.Scan(&d.ID, &d.ToolID, &d.ResourceID, &d.Justification, &d.Version); err != nil {
		return nil, err
	}
	return &d, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func readOperation(row rowScanner, extra ...any) (*Operation, error) {
	var o Operation
	var attempts int
	var leased bool
	fields := []any{&o.ID, &o.ConfirmedVersion, &o.DeliveryState, &o.ExternalID, &o.ServiceStatus, &attempts, &leased}
	if err := row.Scan(append(fields, extra...)...); err != nil {
		return nil, err
	}
	switch o.DeliveryState {
	case "queued":
		o.NextStep = "Заявка в очереди на отправку."
	case "sending":
		o.NextStep = "Отправляем заявку в учебный сервис."
	case "reconciling":
		o.NextStep = "Проверяем, принял ли сервис заявку."
		if attempts >= maxDeliveryAttempts && !leased {
			o.NeedsManualCheck = true
			o.NextStep = "Результат пока неизвестен. Уточните его у наставника перед повторной заявкой."
		}
	case "delivered":
		if o.ServiceStatus != nil {
			switch *o.ServiceStatus {
			case "pending_approval":
				o.NextStep = "Заявка создана. Дождитесь решения."
			case "approved":
				o.NextStep = "Учебный доступ одобрен. Откройте репозиторий команды."
			case "rejected":
				o.NextStep = "Учебная заявка отклонена. Уточните причину у наставника."
			}
		}
	}
	return &o, nil
}

func draftForConversation(ctx context.Context, tx *sql.Tx, id string) (*Draft, error) {
	draft, err := readDraft(tx.QueryRowContext(ctx, `SELECT `+draftColumns+` FROM app_drafts d WHERE d.conversation_id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return draft, err
}

func operationForDraft(ctx context.Context, tx *sql.Tx, id string) (*Operation, error) {
	operation, err := readOperation(tx.QueryRowContext(ctx, `SELECT `+operationColumns+` FROM app_operations o WHERE o.draft_id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return operation, err
}

func (s *Store) GetConversation(ctx context.Context, actor Profile, id string) (*Conversation, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	result := &Conversation{Messages: []Message{}}
	if err := tx.QueryRowContext(ctx, `SELECT id FROM app_conversations WHERE id = $1 AND owner_id = $2 AND owner_team = $3 AND owner_role = $4`, id, actor.ID, actor.TeamID, actor.Role).Scan(&result.ID); err != nil {
		return nil, notFound(err)
	}
	rows, err := tx.QueryContext(ctx, `SELECT id, role, text, created_at, reply_metadata FROM app_messages WHERE conversation_id = $1 ORDER BY id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var m Message
		var raw []byte
		if err := rows.Scan(&m.ID, &m.Role, &m.Text, &m.CreatedAt, &raw); err != nil {
			return nil, err
		}
		var metadata replyMetadata
		if err := json.Unmarshal(raw, &metadata); err != nil {
			return nil, err
		}
		m.Sources, m.ClarifyingQuestion, m.Evidence = metadata.Sources, metadata.ClarifyingQuestion, metadata.Evidence
		m.SourceIDs = sourceIDs(metadata.Sources)
		result.Messages = append(result.Messages, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result.PendingMessage, err = pendingMessage(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	result.Draft, err = draftForConversation(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if result.Draft != nil {
		result.Operation, err = operationForDraft(ctx, tx, result.Draft.ID)
		if err != nil {
			return nil, err
		}
	}
	return result, tx.Commit()
}

func ownedDraft(ctx context.Context, tx *sql.Tx, actor Profile, id string) (*Draft, error) {
	draft, err := readDraft(tx.QueryRowContext(ctx, `SELECT `+draftColumns+`
		FROM app_drafts d JOIN app_conversations c ON c.id = d.conversation_id
		WHERE d.id = $1 AND c.owner_id = $2 AND c.owner_team = $3 AND c.owner_role = $4 FOR UPDATE OF d`, id, actor.ID, actor.TeamID, actor.Role))
	return draft, notFound(err)
}

func validateResource(ctx context.Context, tx *sql.Tx, actor Profile, d *Draft) error {
	var allowed bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM app_resources
		WHERE team_id = $1 AND tool_id = $2 AND resource_id = $3)`, actor.TeamID, d.ToolID, d.ResourceID).Scan(&allowed); err != nil {
		return err
	}
	if !allowed {
		return ErrResource
	}
	return nil
}

func (s *Store) UpdateDraft(ctx context.Context, actor Profile, id string, update DraftUpdate) (*Draft, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	draft, err := ownedDraft(ctx, tx, actor, id)
	if err != nil {
		return nil, err
	}
	if draft.Version != update.ExpectedVersion {
		return nil, ErrVersionConflict
	}
	operation, err := operationForDraft(ctx, tx, draft.ID)
	if err != nil {
		return nil, err
	}
	if operation != nil {
		return nil, ErrDraftConfirmed
	}
	if update.ToolID != nil {
		draft.ToolID = *update.ToolID
	}
	if update.ResourceID != nil {
		draft.ResourceID = *update.ResourceID
	}
	if update.Justification != nil {
		draft.Justification = *update.Justification
	}
	if err := validateResource(ctx, tx, actor, draft); err != nil {
		return nil, err
	}
	draft.Version++
	_, err = tx.ExecContext(ctx, `UPDATE app_drafts SET tool_id = $2, resource_id = $3, justification = $4, version = $5 WHERE id = $1`,
		id, draft.ToolID, draft.ResourceID, draft.Justification, draft.Version)
	if err != nil {
		return nil, err
	}
	return draft, tx.Commit()
}

func (s *Store) ConfirmDraft(ctx context.Context, actor Profile, id string, version int) (*Operation, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	draft, err := ownedDraft(ctx, tx, actor, id)
	if err != nil {
		return nil, err
	}
	if draft.Version != version {
		return nil, ErrVersionConflict
	}
	operation, err := operationForDraft(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if operation != nil {
		return operation, tx.Commit()
	}
	if strings.TrimSpace(draft.Justification) == "" {
		return nil, ErrJustification
	}
	if err := validateResource(ctx, tx, actor, draft); err != nil {
		return nil, err
	}
	payload, err := json.Marshal(struct {
		UserID        string `json:"user_id"`
		TeamID        string `json:"team_id"`
		DraftID       string `json:"draft_id"`
		Version       int    `json:"version"`
		ToolID        string `json:"tool_id"`
		ResourceID    string `json:"resource_id"`
		Justification string `json:"justification"`
	}{
		UserID: actor.ID, TeamID: actor.TeamID, DraftID: draft.ID, Version: draft.Version,
		ToolID: draft.ToolID, ResourceID: draft.ResourceID, Justification: draft.Justification,
	})
	if err != nil {
		return nil, err
	}
	operation, err = readOperation(tx.QueryRowContext(ctx, `INSERT INTO app_operations AS o
		(id, draft_id, confirmed_version, idempotency_key, immutable_payload, delivery_state)
		VALUES ($1, $2, $3, $4, $5::jsonb, 'queued') RETURNING `+operationColumns,
		rand.Text(), id, version, rand.Text(), string(payload)))
	if err != nil {
		return nil, err
	}
	return operation, tx.Commit()
}

func (s *Store) GetOperation(ctx context.Context, actor Profile, id string) (*Operation, error) {
	operation, err := readOperation(s.db.QueryRowContext(ctx, `SELECT `+operationColumns+`
		FROM app_operations o JOIN app_drafts d ON d.id = o.draft_id
		JOIN app_conversations c ON c.id = d.conversation_id WHERE o.id = $1 AND c.owner_id = $2 AND c.owner_team = $3 AND c.owner_role = $4`, id, actor.ID, actor.TeamID, actor.Role))
	return operation, notFound(err)
}

func (s *Store) ListOperations(ctx context.Context, actor Profile) ([]AccessRequest, error) {
	return s.listOperations(ctx, actor, "", 0)
}

func (s *Store) listOperations(ctx context.Context, actor Profile, conversationID string, limit int) ([]AccessRequest, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+operationColumns+`, d.tool_id, d.resource_id
		FROM app_operations o JOIN app_drafts d ON d.id = o.draft_id
		JOIN app_conversations c ON c.id = d.conversation_id
		WHERE c.owner_id = $1 AND c.owner_team = $2 AND c.owner_role = $3
		  AND ($4 = '' OR c.id = $4)
		ORDER BY o.created_at DESC, o.id DESC LIMIT NULLIF($5::bigint, 0)`, actor.ID, actor.TeamID, actor.Role, conversationID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	requests := []AccessRequest{}
	for rows.Next() {
		var request AccessRequest
		operation, err := readOperation(rows, &request.ToolID, &request.ResourceID)
		if err != nil {
			return nil, err
		}
		request.Operation = *operation
		requests = append(requests, request)
	}
	return requests, rows.Err()
}
