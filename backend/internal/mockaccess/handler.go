package mockaccess

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

const pendingApproval = "pending_approval"

var errConflict = errors.New("request conflict")

type requestPayload struct {
	UserID        string `json:"user_id"`
	TeamID        string `json:"team_id"`
	DraftID       string `json:"draft_id"`
	Version       int    `json:"version"`
	ToolID        string `json:"tool_id"`
	ResourceID    string `json:"resource_id"`
	Justification string `json:"justification"`
}

type requestStatus struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type handler struct {
	db *sql.DB
}

func NewHandler(db *sql.DB, token, decisionToken string) http.Handler {
	h := handler{db: db}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "mock-access"})
	})
	mux.HandleFunc("POST /requests", requireBearer(token, h.create))
	mux.HandleFunc("GET /requests/by-key/{key}", requireBearer(token, h.byKey))
	mux.HandleFunc("GET /requests/{id}", requireBearer(token, h.byID))
	if decisionToken == token {
		decisionToken = ""
	}
	mux.HandleFunc("PATCH /requests/{id}/decision", requireBearer(decisionToken, h.decide))
	return mux
}

func requireBearer(token string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		headers := r.Header.Values("Authorization")
		if token != "" && len(headers) == 1 {
			fields := strings.Fields(headers[0])
			if len(fields) == 2 && strings.EqualFold(fields[0], "Bearer") &&
				subtle.ConstantTimeCompare([]byte(fields[1]), []byte(token)) == 1 {
				next(w, r)
				return
			}
		}
		w.Header().Set("WWW-Authenticate", "Bearer")
		writeError(w, http.StatusUnauthorized, "unauthorized")
	}
}

func (h handler) create(w http.ResponseWriter, r *http.Request) {
	keys := r.Header.Values("Idempotency-Key")
	if len(keys) != 1 || !validKey(keys[0]) {
		writeError(w, http.StatusBadRequest, "invalid_idempotency_key")
		return
	}
	var payload requestPayload
	if !readJSON(w, r, &payload) {
		return
	}
	if !payload.valid() {
		writeError(w, http.StatusUnprocessableEntity, "invalid_payload")
		return
	}
	canonical, err := json.Marshal(payload)
	if err != nil {
		writeResult(w, http.StatusOK, requestStatus{}, err)
		return
	}
	result, created, err := h.storeRequest(r.Context(), keys[0], canonical)
	code := http.StatusOK
	if created {
		code = http.StatusCreated
	}
	writeResult(w, code, result, err)
}

func validKey(key string) bool {
	if len(key) == 0 || len(key) > 256 {
		return false
	}
	for _, c := range key {
		if c < 0x21 || c > 0x7e || c == ',' {
			return false
		}
	}
	return true
}

func (p requestPayload) valid() bool {
	for _, value := range []string{p.UserID, p.TeamID, p.DraftID, p.ToolID, p.ResourceID, p.Justification} {
		if strings.TrimSpace(value) == "" || strings.ContainsRune(value, 0) {
			return false
		}
	}
	return p.Version > 0
}

func (h handler) storeRequest(ctx context.Context, key string, canonical []byte) (requestStatus, bool, error) {
	tx, err := h.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return requestStatus{}, false, err
	}
	defer tx.Rollback()

	hash := sha256.Sum256(canonical)
	var result requestStatus
	err = tx.QueryRowContext(ctx, `
		INSERT INTO mock_access.requests (id, idempotency_key, payload_hash, payload)
		VALUES ($1, $2, $3, $4::jsonb)
		ON CONFLICT (idempotency_key) DO NOTHING
		RETURNING id, status`, rand.Text(), key, hash[:], string(canonical)).Scan(&result.ID, &result.Status)
	if err == nil {
		return result, true, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return requestStatus{}, false, err
	}

	// A separate statement sees the winner of a concurrent insert under READ COMMITTED.
	var existingHash []byte
	err = tx.QueryRowContext(ctx, `
		SELECT id, status, payload_hash FROM mock_access.requests WHERE idempotency_key = $1`,
		key).Scan(&result.ID, &result.Status, &existingHash)
	if err != nil {
		return requestStatus{}, false, err
	}
	if !bytes.Equal(existingHash, hash[:]) {
		return requestStatus{}, false, errConflict
	}
	return result, false, tx.Commit()
}

func (h handler) byKey(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if !validKey(key) {
		writeError(w, http.StatusBadRequest, "invalid_idempotency_key")
		return
	}
	var result requestStatus
	err := h.db.QueryRowContext(r.Context(),
		`SELECT id, status FROM mock_access.requests WHERE idempotency_key = $1`,
		key).Scan(&result.ID, &result.Status)
	writeResult(w, http.StatusOK, result, err)
}

func (h handler) byID(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if strings.ContainsRune(id, 0) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	var result requestStatus
	err := h.db.QueryRowContext(r.Context(),
		`SELECT id, status FROM mock_access.requests WHERE id = $1`,
		id).Scan(&result.ID, &result.Status)
	writeResult(w, http.StatusOK, result, err)
}

func (h handler) decide(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Status string `json:"status"`
	}
	if !readJSON(w, r, &input) {
		return
	}
	if input.Status != "approved" && input.Status != "rejected" {
		writeError(w, http.StatusUnprocessableEntity, "invalid_decision")
		return
	}
	id := r.PathValue("id")
	if strings.ContainsRune(id, 0) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	result, err := h.storeDecision(r.Context(), id, input.Status)
	writeResult(w, http.StatusOK, result, err)
}

func (h handler) storeDecision(ctx context.Context, id, status string) (requestStatus, error) {
	tx, err := h.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return requestStatus{}, err
	}
	defer tx.Rollback()

	var result requestStatus
	err = tx.QueryRowContext(ctx, `
		SELECT id, status FROM mock_access.requests WHERE id = $1 FOR UPDATE`,
		id).Scan(&result.ID, &result.Status)
	if err != nil {
		return requestStatus{}, err
	}
	switch result.Status {
	case status:
		return result, tx.Commit()
	case pendingApproval:
		err = tx.QueryRowContext(ctx, `
			UPDATE mock_access.requests SET status = $2 WHERE id = $1 RETURNING status`,
			id, status).Scan(&result.Status)
		if err != nil {
			return requestStatus{}, err
		}
		return result, tx.Commit()
	default:
		return requestStatus{}, errConflict
	}
}

func readJSON(w http.ResponseWriter, r *http.Request, value any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	err := decoder.Decode(value)
	if err == nil {
		err = decoder.Decode(new(json.RawMessage))
		if err == io.EOF {
			return true
		}
	}
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, "body_too_large")
		return false
	}
	writeError(w, http.StatusBadRequest, "invalid_json")
	return false
}

func writeResult(w http.ResponseWriter, code int, result requestStatus, err error) {
	switch {
	case err == nil:
		writeJSON(w, code, result)
	case errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, "not_found")
	case errors.Is(err, errConflict):
		writeError(w, http.StatusConflict, "conflict")
	default:
		writeError(w, http.StatusInternalServerError, "internal_error")
	}
}

func writeError(w http.ResponseWriter, code int, kind string) {
	writeJSON(w, code, map[string]string{"error": kind})
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}
