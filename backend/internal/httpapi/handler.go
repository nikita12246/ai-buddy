package httpapi

import (
	"net/http"

	"ai-buddy/backend/internal/knowledge"
	"ai-buddy/backend/internal/onboarding"
)

func NewHandler() http.Handler { return NewApplication(nil) }

func NewApplication(store *onboarding.Store) http.Handler {
	return NewIntegratedApplication(store, nil, nil)
}

func NewIntegratedApplication(store *onboarding.Store, answerer onboarding.Answerer, documents *knowledge.Searcher) http.Handler {
	return newApplication(store, answerer, documents, false)
}

func NewGuestApplication(store *onboarding.Store, answerer onboarding.Answerer, documents *knowledge.Searcher) http.Handler {
	return newApplication(store, answerer, documents, true)
}

func newApplication(store *onboarding.Store, answerer onboarding.Answerer, documents *knowledge.Searcher, guests bool) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, map[string]string{
			"status":  "ok",
			"service": "ai-buddy-api",
		})
	})
	if store != nil {
		registerOnboarding(mux, store, answerer, documents, guests)
	}
	return mux
}
