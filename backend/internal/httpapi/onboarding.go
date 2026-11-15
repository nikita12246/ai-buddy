package httpapi

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"unicode/utf8"

	"ai-buddy/backend/internal/knowledge"
	"ai-buddy/backend/internal/onboarding"
)

const guestCookie = "aibuddy_guest"

func registerOnboarding(mux *http.ServeMux, store *onboarding.Store, answerer onboarding.Answerer, documents *knowledge.Searcher, guests bool) {
	type endpoint func(http.ResponseWriter, *http.Request, onboarding.Profile)
	protected := func(next endpoint) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			var token string
			auth := r.Header.Get("Authorization")
			if auth != "" {
				fields := strings.Fields(auth)
				if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") || len(fields[1]) > 512 {
					unauthorized(w)
					return
				}
				token = fields[1]
			} else if guests {
				if cookie, err := r.Cookie(guestCookie); err == nil && len(cookie.Value) <= 512 {
					token = cookie.Value
				}
			}
			err := onboarding.ErrNotFound
			var actor onboarding.Profile
			if token != "" {
				actor, err = store.Authenticate(r.Context(), token)
			}
			if errors.Is(err, onboarding.ErrNotFound) && guests && auth == "" && r.Method == http.MethodPost && r.URL.Path == "/api/session" {
				actor, token, err = store.StartGuestSession(r.Context())
				if err == nil {
					http.SetCookie(w, &http.Cookie{
						Name: guestCookie, Value: token, Path: "/api", MaxAge: 7 * 24 * 3600,
						HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode,
					})
				}
			}
			if errors.Is(err, onboarding.ErrNotFound) {
				unauthorized(w)
				return
			}
			if err != nil {
				storeError(w, err)
				return
			}
			next(w, r, actor)
		}
	}

	profile := protected(func(w http.ResponseWriter, r *http.Request, actor onboarding.Profile) {
		writeJSON(w, http.StatusOK, actor)
	})
	mux.HandleFunc("GET /api/me", profile)
	if guests {
		mux.HandleFunc("POST /api/session", profile)
	}
	mux.HandleFunc("POST /api/conversations", protected(func(w http.ResponseWriter, r *http.Request, actor onboarding.Profile) {
		id, err := store.CreateConversation(r.Context(), actor)
		writeResult(w, http.StatusCreated, map[string]string{"id": id}, err)
	}))
	mux.HandleFunc("GET /api/conversations/{id}", protected(func(w http.ResponseWriter, r *http.Request, actor onboarding.Profile) {
		result, err := store.GetConversation(r.Context(), actor, r.PathValue("id"))
		if err == nil {
			for i := range result.Messages {
				message := &result.Messages[i]
				var visible bool
				visible, err = canReadEvidence(r, actor, answerer, message.Evidence)
				if err != nil {
					break
				}
				if !visible {
					message.Text = hiddenAnswer
					message.SourceIDs, message.Sources, message.ClarifyingQuestion = nil, nil, nil
				}
			}
		}
		writeResult(w, http.StatusOK, result, err)
	}))
	mux.HandleFunc("POST /api/conversations/{id}/messages", protected(func(w http.ResponseWriter, r *http.Request, actor onboarding.Profile) {
		var input struct {
			ClientMessageID string `json:"client_message_id"`
			Text            string `json:"text"`
		}
		if !readJSON(w, r, &input) {
			return
		}
		input.ClientMessageID = strings.TrimSpace(input.ClientMessageID)
		input.Text = strings.TrimSpace(input.Text)
		if input.ClientMessageID == "" || len(input.ClientMessageID) > 128 || strings.ContainsRune(input.ClientMessageID, 0) ||
			input.Text == "" || strings.ContainsRune(input.Text, 0) || utf8.RuneCountInString(input.Text) > 4000 {
			writeError(w, http.StatusUnprocessableEntity, "invalid_fields", "Укажите идентификатор и текст сообщения.")
			return
		}
		result, err := store.AddMessage(r.Context(), actor, r.PathValue("id"), input.ClientMessageID, input.Text, answerer)
		if err == nil {
			var visible bool
			visible, err = canReadEvidence(r, actor, answerer, result.Evidence)
			if err == nil && !visible {
				result.Answer = hiddenAnswer
				result.SourceIDs, result.Sources, result.ClarifyingQuestion = []string{}, []onboarding.Citation{}, nil
			}
		}
		writeResult(w, http.StatusOK, result, err)
	}))
	mux.HandleFunc("PATCH /api/drafts/{id}", protected(func(w http.ResponseWriter, r *http.Request, actor onboarding.Profile) {
		var input onboarding.DraftUpdate
		if !readJSON(w, r, &input) {
			return
		}
		if !validUpdate(&input) {
			writeError(w, http.StatusUnprocessableEntity, "invalid_fields", "Укажите версию и корректные поля черновика.")
			return
		}
		result, err := store.UpdateDraft(r.Context(), actor, r.PathValue("id"), input)
		writeResult(w, http.StatusOK, result, err)
	}))
	mux.HandleFunc("POST /api/drafts/{id}/confirm", protected(func(w http.ResponseWriter, r *http.Request, actor onboarding.Profile) {
		var input struct {
			Version int `json:"version"`
		}
		if !readJSON(w, r, &input) {
			return
		}
		if input.Version < 1 {
			writeError(w, http.StatusUnprocessableEntity, "invalid_fields", "Укажите версию черновика.")
			return
		}
		result, err := store.ConfirmDraft(r.Context(), actor, r.PathValue("id"), input.Version)
		writeResult(w, http.StatusOK, result, err)
	}))
	mux.HandleFunc("GET /api/operations", protected(func(w http.ResponseWriter, r *http.Request, actor onboarding.Profile) {
		result, err := store.ListOperations(r.Context(), actor)
		writeResult(w, http.StatusOK, map[string]any{"operations": result}, err)
	}))
	mux.HandleFunc("GET /api/operations/{id}", protected(func(w http.ResponseWriter, r *http.Request, actor onboarding.Profile) {
		result, err := store.GetOperation(r.Context(), actor, r.PathValue("id"))
		writeResult(w, http.StatusOK, result, err)
	}))
	if documents != nil {
		mux.HandleFunc("GET /api/documents", protected(func(w http.ResponseWriter, r *http.Request, actor onboarding.Profile) {
			result, err := documents.ListDocuments(r.Context(), knowledge.Actor{TeamID: actor.TeamID, Role: actor.Role})
			writeResult(w, http.StatusOK, map[string]any{"documents": result}, err)
		}))
		mux.HandleFunc("GET /api/documents/{id}", protected(func(w http.ResponseWriter, r *http.Request, actor onboarding.Profile) {
			id := r.PathValue("id")
			if strings.ContainsRune(id, 0) {
				storeError(w, onboarding.ErrNotFound)
				return
			}
			chunks, err := documents.GetDocument(r.Context(), knowledge.Actor{TeamID: actor.TeamID, Role: actor.Role}, id)
			if errors.Is(err, knowledge.ErrDocumentNotFound) {
				err = onboarding.ErrNotFound
			}
			writeResult(w, http.StatusOK, map[string]any{"document_id": id, "chunks": chunks}, err)
		}))
	}
}

const hiddenAnswer = "Ответ скрыт. Его источники изменились или стали недоступны."

func canReadEvidence(r *http.Request, actor onboarding.Profile, answerer onboarding.Answerer, evidence []onboarding.Citation) (bool, error) {
	if len(evidence) == 0 {
		return true, nil
	}
	if answerer == nil {
		return false, nil
	}
	return answerer.VerifyEvidence(r.Context(), actor, evidence)
}

func validUpdate(input *onboarding.DraftUpdate) bool {
	if input.ExpectedVersion < 1 || (input.ToolID == nil && input.ResourceID == nil && input.Justification == nil) {
		return false
	}
	for _, id := range []*string{input.ToolID, input.ResourceID} {
		if id != nil {
			*id = strings.TrimSpace(*id)
			if *id == "" || len(*id) > 128 || strings.ContainsRune(*id, 0) {
				return false
			}
		}
	}
	if input.Justification != nil {
		*input.Justification = strings.TrimSpace(*input.Justification)
		return !strings.ContainsRune(*input.Justification, 0) && utf8.RuneCountInString(*input.Justification) <= 1000
	}
	return true
}

func readJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err == nil && decoder.Decode(new(json.RawMessage)) == io.EOF {
		return true
	}
	writeError(w, http.StatusUnprocessableEntity, "invalid_json", "Ожидается один JSON-объект с полями запроса.")
	return false
}

func writeResult(w http.ResponseWriter, status int, result any, err error) {
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, status, result)
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	writeError(w, http.StatusUnauthorized, "unauthenticated", "Войдите в учебную сессию.")
}

func storeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, onboarding.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "Объект не найден.")
	case errors.Is(err, onboarding.ErrVersionConflict):
		writeError(w, http.StatusConflict, "version_conflict", "Черновик изменился. Обновите его перед подтверждением.")
	case errors.Is(err, onboarding.ErrMessageConflict):
		writeError(w, http.StatusConflict, "message_conflict", "Идентификатор сообщения уже использован для другого текста.")
	case errors.Is(err, onboarding.ErrDraftConfirmed):
		writeError(w, http.StatusConflict, "draft_confirmed", "Черновик уже подтверждён.")
	case errors.Is(err, onboarding.ErrResource):
		writeError(w, http.StatusUnprocessableEntity, "resource_unavailable", "Ресурс недоступен для вашей команды.")
	case errors.Is(err, onboarding.ErrGenerationInProgress):
		writeError(w, http.StatusConflict, "generation_in_progress", "Дождитесь текущего ответа и повторите запрос.")
	case errors.Is(err, onboarding.ErrAssistantTimeout):
		writeError(w, http.StatusGatewayTimeout, "assistant_timeout", "Помощник не успел ответить. Сообщение сохранено, попробуйте ещё раз.")
	case errors.Is(err, onboarding.ErrAssistantUnavailable):
		writeError(w, http.StatusServiceUnavailable, "assistant_unavailable", "Помощник временно недоступен. Сообщение сохранено, попробуйте ещё раз.")
	case errors.Is(err, onboarding.ErrJustification):
		writeError(w, http.StatusUnprocessableEntity, "justification_required", "Укажите обоснование перед подтверждением.")
	default:
		requestID := rand.Text()
		// Database errors can include input values. Log only the type and correlation ID.
		log.Printf("request %s: storage error %T", requestID, err)
		writeErrorWithID(w, http.StatusInternalServerError, "internal_error", "Не удалось обработать запрос.", requestID)
	}
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeErrorWithID(w, status, code, message, rand.Text())
}

func writeErrorWithID(w http.ResponseWriter, status int, code, message, id string) {
	writeJSON(w, status, map[string]any{
		"error":      map[string]string{"code": code, "message": message},
		"request_id": id,
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
