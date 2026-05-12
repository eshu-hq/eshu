package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/eshu-hq/eshu/go/internal/webhook"
)

type triggerStore interface {
	StoreTrigger(context.Context, webhook.Trigger, time.Time) (webhook.StoredTrigger, error)
}

type webhookHandler struct {
	Config webhookListenerConfig
	Store  triggerStore
	Clock  func() time.Time
	Logger *slog.Logger
}

func newWebhookMux(handler webhookHandler) (*http.ServeMux, error) {
	if handler.Store == nil {
		return nil, fmt.Errorf("webhook trigger store is required")
	}
	mux := http.NewServeMux()
	if handler.Config.GitHubSecret != "" {
		mux.HandleFunc(handler.Config.GitHubPath, handler.handleGitHub)
	}
	if handler.Config.GitLabToken != "" {
		mux.HandleFunc(handler.Config.GitLabPath, handler.handleGitLab)
	}
	return mux, nil
}

func (h webhookHandler) handleGitHub(w http.ResponseWriter, r *http.Request) {
	payload, ok := h.readPostBody(w, r)
	if !ok {
		return
	}
	if err := webhook.VerifyGitHubSignature(payload, h.Config.GitHubSecret, r.Header.Get("X-Hub-Signature-256")); err != nil {
		http.Error(w, "signature verification failed", http.StatusUnauthorized)
		return
	}

	trigger, err := webhook.NormalizeGitHub(
		r.Header.Get("X-GitHub-Event"),
		r.Header.Get("X-GitHub-Delivery"),
		payload,
		h.Config.DefaultBranch,
	)
	h.storeAndWrite(w, r, trigger, err)
}

func (h webhookHandler) handleGitLab(w http.ResponseWriter, r *http.Request) {
	payload, ok := h.readPostBody(w, r)
	if !ok {
		return
	}
	if err := webhook.VerifyGitLabToken(h.Config.GitLabToken, r.Header.Get("X-Gitlab-Token")); err != nil {
		http.Error(w, "token verification failed", http.StatusUnauthorized)
		return
	}

	deliveryID := firstNonEmpty(
		r.Header.Get("Idempotency-Key"),
		r.Header.Get("X-Gitlab-Webhook-UUID"),
		r.Header.Get("X-Gitlab-Event-UUID"),
		r.Header.Get("X-Request-Id"),
	)
	trigger, err := webhook.NormalizeGitLab(
		r.Header.Get("X-Gitlab-Event"),
		deliveryID,
		payload,
		h.Config.DefaultBranch,
	)
	h.storeAndWrite(w, r, trigger, err)
}

func (h webhookHandler) readPostBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return nil, false
	}
	limited := http.MaxBytesReader(w, r.Body, h.Config.MaxRequestBodyBytes)
	defer func() { _ = limited.Close() }()
	payload, err := io.ReadAll(limited)
	if err != nil {
		http.Error(w, "invalid request body", http.StatusRequestEntityTooLarge)
		return nil, false
	}
	if len(payload) == 0 {
		http.Error(w, "empty request body", http.StatusBadRequest)
		return nil, false
	}
	return payload, true
}

func (h webhookHandler) storeAndWrite(w http.ResponseWriter, r *http.Request, trigger webhook.Trigger, normalizeErr error) {
	if normalizeErr != nil {
		http.Error(w, "unsupported or malformed webhook event", http.StatusBadRequest)
		return
	}
	stored, err := h.Store.StoreTrigger(r.Context(), trigger, h.now())
	if err != nil {
		if h.Logger != nil {
			h.Logger.ErrorContext(r.Context(), "webhook trigger persistence failed",
				slog.String("provider", string(trigger.Provider)),
				slog.String("event_kind", string(trigger.EventKind)),
				slog.String("decision", string(trigger.Decision)),
				slog.String("reason", string(trigger.Reason)),
				slog.String("error", err.Error()),
			)
		}
		http.Error(w, "store webhook trigger", http.StatusInternalServerError)
		return
	}
	writeWebhookJSON(w, http.StatusAccepted, map[string]any{
		"trigger_id": stored.TriggerID,
		"status":     stored.Status,
		"decision":   stored.Decision,
		"reason":     stored.Reason,
	})
}

func (h webhookHandler) now() time.Time {
	if h.Clock != nil {
		return h.Clock()
	}
	return time.Now().UTC()
}

func writeWebhookJSON(w http.ResponseWriter, status int, payload map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
