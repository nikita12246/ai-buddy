package onboarding

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type messageClaim struct {
	id        int64
	token     string
	history   []Message
	operation *Operation
	cached    *Reply
}

func (s *Store) AddMessage(ctx context.Context, actor Profile, id, clientID, text string, answerer Answerer) (*Reply, error) {
	statusQuestion := isStatusQuestion(text)
	claim, err := s.claimMessage(ctx, actor, id, clientID, text, answerer != nil && !statusQuestion)
	if err != nil {
		return nil, err
	}
	if claim.cached != nil {
		return claim.cached, nil
	}
	var generated *Generation
	if statusQuestion {
		generated, err = s.requestStatus(ctx, actor, id)
	} else if answerer == nil {
		generated, err = s.demoResponse(ctx, actor, text)
	} else {
		generationContext, cancel := context.WithTimeout(ctx, 50*time.Second)
		generated, err = answerer.Generate(generationContext, actor, text, claim.history, claim.operation)
		cancel()
	}
	if err == nil && (generated == nil || strings.TrimSpace(generated.Answer) == "" || strings.ContainsRune(generated.Answer, 0)) {
		err = ErrAssistantUnavailable
	}
	if err != nil {
		s.releaseClaim(ctx, claim)
		return nil, err
	}
	if generated.DraftProposal != nil {
		// Only carry over an exact excerpt of the user's reason. A generated
		// paraphrase must be checked and entered by the user before confirming.
		reason := strings.TrimSpace(generated.DraftProposal.Justification)
		if !strings.Contains(text, reason) {
			generated.DraftProposal.Justification = ""
		}
	}
	reply, err := s.finishMessage(ctx, actor, id, clientID, claim, generated)
	if err != nil {
		s.releaseClaim(ctx, claim)
	}
	return reply, err
}

func isStatusQuestion(text string) bool {
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) })
	switch strings.Join(words, " ") {
	case "проверить статус заявки", "какой статус заявки", "какой статус моей заявки",
		"покажи статус заявки", "что с моей заявкой", "что с заявкой":
		return true
	}
	return false
}

func (s *Store) requestStatus(ctx context.Context, actor Profile, conversationID string) (*Generation, error) {
	requests, err := s.listOperations(ctx, actor, conversationID, 2)
	if err != nil {
		return nil, err
	}
	if len(requests) == 0 {
		requests, err = s.listOperations(ctx, actor, "", 2)
		if err != nil {
			return nil, err
		}
	}
	switch len(requests) {
	case 0:
		return &Generation{Answer: "У тебя пока нет подтверждённых заявок. Подготовь черновик и подтверди отправку."}, nil
	case 1:
		request := requests[0]
		header := "На момент проверки · " + request.ResourceID
		if request.ExternalID != nil {
			header += " · заявка " + *request.ExternalID
		}
		return &Generation{Answer: header + "\n" + request.NextStep}, nil
	default:
		return &Generation{Answer: "У тебя несколько заявок. Открой «Доступы», чтобы выбрать нужную и посмотреть её статус."}, nil
	}
}

func lockConversation(ctx context.Context, tx *sql.Tx, actor Profile, id string) error {
	var owned string
	return notFound(tx.QueryRowContext(ctx, `SELECT c.id FROM app_conversations c
		JOIN app_users u ON u.id = c.owner_id AND u.team_id = c.owner_team AND u.role = c.owner_role
		WHERE c.id = $1 AND c.owner_id = $2 AND c.owner_team = $3 AND c.owner_role = $4 FOR UPDATE OF c`,
		id, actor.ID, actor.TeamID, actor.Role).Scan(&owned))
}

func (s *Store) claimMessage(ctx context.Context, actor Profile, id, clientID, text string, withHistory bool) (*messageClaim, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := lockConversation(ctx, tx, actor, id); err != nil {
		return nil, err
	}
	claim := &messageClaim{token: rand.Text()}
	var previous string
	err = tx.QueryRowContext(ctx, `SELECT id, text FROM app_messages WHERE conversation_id = $1 AND client_message_id = $2 AND role = 'user'`,
		id, clientID).Scan(&claim.id, &previous)
	if err == nil {
		if previous != text {
			return nil, ErrMessageConflict
		}
		claim.cached, err = savedReply(ctx, tx, id, clientID)
		if err != nil || claim.cached != nil {
			if err != nil {
				return nil, err
			}
			return claim, tx.Commit()
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	var busy bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM app_messages
		WHERE conversation_id = $1 AND generation_token IS NOT NULL AND generation_until > now())`, id).Scan(&busy); err != nil {
		return nil, err
	}
	if busy {
		return nil, ErrGenerationInProgress
	}
	if claim.id == 0 {
		if err := tx.QueryRowContext(ctx, `INSERT INTO app_messages (conversation_id, client_message_id, role, text)
			VALUES ($1, $2, 'user', $3) RETURNING id`, id, clientID, text).Scan(&claim.id); err != nil {
			return nil, err
		}
	}
	if withHistory {
		claim.history, err = recentQuestions(ctx, tx, id, claim.id)
		if err != nil {
			return nil, err
		}
		draft, err := draftForConversation(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		if draft != nil {
			claim.operation, err = operationForDraft(ctx, tx, draft.ID)
			if err != nil {
				return nil, err
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE app_messages SET generation_token = $2, generation_until = now() + interval '60 seconds' WHERE id = $1`,
		claim.id, claim.token); err != nil {
		return nil, err
	}
	return claim, tx.Commit()
}

func savedReply(ctx context.Context, tx *sql.Tx, id, clientID string) (*Reply, error) {
	var answer string
	var raw []byte
	err := tx.QueryRowContext(ctx, `SELECT text, reply_metadata FROM app_messages
		WHERE conversation_id = $1 AND client_message_id = $2 AND role = 'assistant'`, id, clientID).Scan(&answer, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var metadata replyMetadata
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return nil, err
	}
	draft, err := draftForConversation(ctx, tx, id)
	return makeReply(answer, metadata, draft), err
}

func makeReply(answer string, metadata replyMetadata, draft *Draft) *Reply {
	if metadata.Sources == nil {
		metadata.Sources = []Citation{}
	}
	return &Reply{Answer: answer, SourceIDs: sourceIDs(metadata.Sources), Sources: metadata.Sources,
		Evidence: metadata.Evidence, ClarifyingQuestion: metadata.ClarifyingQuestion, Draft: draft}
}

func pendingMessage(ctx context.Context, tx *sql.Tx, conversationID string) (*PendingMessage, error) {
	var message PendingMessage
	err := tx.QueryRowContext(ctx, `WITH latest_question AS (
		SELECT client_message_id, text FROM app_messages
		WHERE conversation_id = $1 AND role = 'user'
		ORDER BY id DESC LIMIT 1
	)
	SELECT q.client_message_id, q.text FROM latest_question q
	WHERE NOT EXISTS (
		SELECT 1 FROM app_messages a
		WHERE a.conversation_id = $1 AND a.client_message_id = q.client_message_id AND a.role = 'assistant'
	)`, conversationID).Scan(&message.ClientID, &message.Text)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &message, nil
}

func recentQuestions(ctx context.Context, tx *sql.Tx, conversationID string, beforeID int64) ([]Message, error) {
	rows, err := tx.QueryContext(ctx, `SELECT text FROM app_messages
		WHERE conversation_id = $1 AND role = 'user' AND id < $2 ORDER BY id DESC LIMIT 6`, conversationID, beforeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var history []Message
	for rows.Next() {
		m := Message{Role: "user"}
		if err := rows.Scan(&m.Text); err != nil {
			return nil, err
		}
		history = append(history, m)
	}
	slices.Reverse(history)
	return history, rows.Err()
}

func (s *Store) releaseClaim(ctx context.Context, claim *messageClaim) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	_, _ = s.db.ExecContext(ctx, `UPDATE app_messages SET generation_token = NULL, generation_until = NULL
		WHERE id = $1 AND generation_token = $2`, claim.id, claim.token)
}

func (s *Store) finishMessage(ctx context.Context, actor Profile, id, clientID string, claim *messageClaim, generated *Generation) (*Reply, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := lockConversation(ctx, tx, actor, id); err != nil {
		return nil, err
	}
	var active bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM app_messages
		WHERE id = $1 AND generation_token = $2 AND generation_until > now())`, claim.id, claim.token).Scan(&active); err != nil {
		return nil, err
	}
	if !active {
		return nil, ErrGenerationInProgress
	}
	draft, err := draftForConversation(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if draft == nil && generated.DraftProposal != nil {
		proposal := generated.DraftProposal
		candidate := &Draft{ID: rand.Text(), ToolID: proposal.ToolID, ResourceID: proposal.ResourceID,
			Justification: strings.TrimSpace(proposal.Justification), Version: 1}
		if utf8.RuneCountInString(candidate.Justification) <= 1000 && !strings.ContainsRune(candidate.Justification, 0) {
			switch err := validateResource(ctx, tx, actor, candidate); {
			case err == nil:
				if _, err := tx.ExecContext(ctx, `INSERT INTO app_drafts (id, conversation_id, tool_id, resource_id, justification, version)
					VALUES ($1, $2, $3, $4, $5, 1)`, candidate.ID, id, candidate.ToolID, candidate.ResourceID, candidate.Justification); err != nil {
					return nil, err
				}
				draft = candidate
			case !errors.Is(err, ErrResource):
				return nil, err
			}
		}
	}
	metadata := replyMetadata{Sources: generated.Sources, ClarifyingQuestion: generated.ClarifyingQuestion, Evidence: generated.Evidence}
	raw, err := json.Marshal(metadata)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO app_messages (conversation_id, client_message_id, role, text, reply_metadata)
		VALUES ($1, $2, 'assistant', $3, $4::jsonb)`, id, clientID, generated.Answer, string(raw)); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE app_messages SET generation_token = NULL, generation_until = NULL
		WHERE id = $1 AND generation_token = $2`, claim.id, claim.token); err != nil {
		return nil, err
	}
	return makeReply(generated.Answer, metadata, draft), tx.Commit()
}

func (s *Store) demoResponse(ctx context.Context, actor Profile, text string) (*Generation, error) {
	result := &Generation{Answer: "Учебный режим поддерживает сценарий запроса доступа к GitLab своей команды."}
	tokens := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	if !slices.Contains(tokens, "gitlab") && !slices.Contains(tokens, "гитлаб") {
		return result, nil
	}
	var resourceID string
	err := s.db.QueryRowContext(ctx, `SELECT resource_id FROM app_resources WHERE team_id = $1 AND tool_id = 'gitlab'
		AND (SELECT count(*) FROM app_resources WHERE team_id = $1 AND tool_id = 'gitlab') = 1`, actor.TeamID).Scan(&resourceID)
	if errors.Is(err, sql.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	result.Answer = "Учебный режим. Черновик доступен в диалоге. Проверьте ресурс и укажите обоснование перед подтверждением."
	result.DraftProposal = &DraftProposal{ToolID: "gitlab", ResourceID: resourceID}
	return result, nil
}
