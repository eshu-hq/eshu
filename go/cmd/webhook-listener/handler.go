package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/webhook"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

type triggerStore interface {
	StoreTrigger(context.Context, webhook.Trigger, time.Time) (webhook.StoredTrigger, error)
}

type webhookHandler struct {
	Config      webhookListenerConfig
	Store       triggerStore
	Clock       func() time.Time
	Logger      *slog.Logger
	Instruments *telemetry.Instruments
	Tracer      trace.Tracer
}

type webhookTelemetryResult struct {
	Outcome   string
	Reason    string
	EventKind webhook.EventKind
	Decision  webhook.Decision
	Status    webhook.TriggerStatus
}

const (
	webhookOutcomeStored   = "stored"
	webhookOutcomeRejected = "rejected"
	webhookOutcomeFailed   = "failed"
	webhookReasonNone      = "none"
	webhookReasonBadMethod = "bad_method"
	webhookReasonTooLarge  = "body_too_large"
	webhookReasonBadBody   = "invalid_body"
	webhookReasonEmptyBody = "empty_body"
	webhookReasonAuth      = "auth_failed"
	webhookReasonDelivery  = "missing_delivery_id"
	webhookReasonMalformed = "malformed_event"
	webhookReasonStore     = "store_failed"
	webhookStatusUnknown   = "unknown"
	webhookValueUnknown    = "unknown"
)

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
	if handler.Config.BitbucketSecret != "" {
		mux.HandleFunc(handler.Config.BitbucketPath, handler.handleBitbucket)
	}
	return mux, nil
}

func (h webhookHandler) handleGitHub(w http.ResponseWriter, r *http.Request) {
	ctx, span, startedAt := h.startWebhookRequest(r.Context(), webhook.ProviderGitHub)
	r = r.WithContext(ctx)
	result := webhookTelemetryResult{Outcome: webhookOutcomeRejected, Reason: webhookReasonMalformed}
	defer func() {
		h.finishWebhookRequest(ctx, span, startedAt, webhook.ProviderGitHub, result)
	}()

	payload, readReason, ok := h.readPostBody(w, r)
	if !ok {
		result.Reason = readReason
		return
	}
	if err := webhook.VerifyGitHubSignature(payload, h.Config.GitHubSecret, r.Header.Get("X-Hub-Signature-256")); err != nil {
		result.Reason = webhookReasonAuth
		http.Error(w, "signature verification failed", http.StatusUnauthorized)
		return
	}

	deliveryID := strings.TrimSpace(r.Header.Get("X-GitHub-Delivery"))
	if deliveryID == "" {
		result.Reason = webhookReasonDelivery
		http.Error(w, "missing delivery id", http.StatusBadRequest)
		return
	}
	trigger, err := webhook.NormalizeGitHub(
		r.Header.Get("X-GitHub-Event"),
		deliveryID,
		payload,
		h.Config.DefaultBranch,
	)
	result = h.storeAndWrite(w, r, trigger, err)
}

func (h webhookHandler) handleGitLab(w http.ResponseWriter, r *http.Request) {
	ctx, span, startedAt := h.startWebhookRequest(r.Context(), webhook.ProviderGitLab)
	r = r.WithContext(ctx)
	result := webhookTelemetryResult{Outcome: webhookOutcomeRejected, Reason: webhookReasonMalformed}
	defer func() {
		h.finishWebhookRequest(ctx, span, startedAt, webhook.ProviderGitLab, result)
	}()

	payload, readReason, ok := h.readPostBody(w, r)
	if !ok {
		result.Reason = readReason
		return
	}
	if err := webhook.VerifyGitLabToken(h.Config.GitLabToken, r.Header.Get("X-Gitlab-Token")); err != nil {
		result.Reason = webhookReasonAuth
		http.Error(w, "token verification failed", http.StatusUnauthorized)
		return
	}

	deliveryID := firstNonEmpty(
		r.Header.Get("Idempotency-Key"),
		r.Header.Get("X-Gitlab-Webhook-UUID"),
		r.Header.Get("X-Gitlab-Event-UUID"),
		r.Header.Get("X-Request-Id"),
	)
	deliveryID = strings.TrimSpace(deliveryID)
	if deliveryID == "" {
		result.Reason = webhookReasonDelivery
		http.Error(w, "missing delivery id", http.StatusBadRequest)
		return
	}
	trigger, err := webhook.NormalizeGitLab(
		r.Header.Get("X-Gitlab-Event"),
		deliveryID,
		payload,
		h.Config.DefaultBranch,
	)
	result = h.storeAndWrite(w, r, trigger, err)
}

func (h webhookHandler) handleBitbucket(w http.ResponseWriter, r *http.Request) {
	ctx, span, startedAt := h.startWebhookRequest(r.Context(), webhook.ProviderBitbucket)
	r = r.WithContext(ctx)
	result := webhookTelemetryResult{Outcome: webhookOutcomeRejected, Reason: webhookReasonMalformed}
	defer func() {
		h.finishWebhookRequest(ctx, span, startedAt, webhook.ProviderBitbucket, result)
	}()

	payload, readReason, ok := h.readPostBody(w, r)
	if !ok {
		result.Reason = readReason
		return
	}
	if err := webhook.VerifyBitbucketSignature(payload, h.Config.BitbucketSecret, r.Header.Get("X-Hub-Signature")); err != nil {
		result.Reason = webhookReasonAuth
		http.Error(w, "signature verification failed", http.StatusUnauthorized)
		return
	}

	deliveryID := firstNonEmpty(
		r.Header.Get("X-Request-UUID"),
		r.Header.Get("X-Hook-UUID"),
	)
	deliveryID = strings.TrimSpace(deliveryID)
	if deliveryID == "" {
		result.Reason = webhookReasonDelivery
		http.Error(w, "missing delivery id", http.StatusBadRequest)
		return
	}
	trigger, err := webhook.NormalizeBitbucket(
		r.Header.Get("X-Event-Key"),
		deliveryID,
		payload,
		h.Config.DefaultBranch,
	)
	result = h.storeAndWrite(w, r, trigger, err)
}

func (h webhookHandler) readPostBody(w http.ResponseWriter, r *http.Request) ([]byte, string, bool) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return nil, webhookReasonBadMethod, false
	}
	limited := http.MaxBytesReader(w, r.Body, h.Config.MaxRequestBodyBytes)
	defer func() { _ = limited.Close() }()
	payload, err := io.ReadAll(limited)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return nil, webhookReasonTooLarge, false
		}
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return nil, webhookReasonBadBody, false
	}
	if len(payload) == 0 {
		http.Error(w, "empty request body", http.StatusBadRequest)
		return nil, webhookReasonEmptyBody, false
	}
	return payload, webhookReasonNone, true
}

func (h webhookHandler) storeAndWrite(
	w http.ResponseWriter,
	r *http.Request,
	trigger webhook.Trigger,
	normalizeErr error,
) webhookTelemetryResult {
	result := webhookTelemetryResult{
		Outcome:   webhookOutcomeRejected,
		Reason:    webhookReasonMalformed,
		EventKind: trigger.EventKind,
		Decision:  trigger.Decision,
	}
	if normalizeErr != nil {
		http.Error(w, "unsupported or malformed webhook event", http.StatusBadRequest)
		return result
	}
	storeCtx, span, startedAt := h.startWebhookStore(r.Context(), trigger)
	stored, err := h.Store.StoreTrigger(storeCtx, trigger, h.now())
	if err != nil {
		result.Outcome = webhookOutcomeFailed
		result.Reason = webhookReasonStore
		h.finishWebhookStore(storeCtx, span, startedAt, trigger.Provider, result)
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
		return result
	}
	result.Outcome = webhookOutcomeStored
	result.Reason = webhookReasonNone
	result.Status = stored.Status
	h.finishWebhookStore(storeCtx, span, startedAt, trigger.Provider, result)
	h.recordWebhookDecision(storeCtx, trigger, stored)
	writeWebhookJSON(w, http.StatusAccepted, map[string]any{
		"trigger_id": stored.TriggerID,
		"status":     stored.Status,
		"decision":   stored.Decision,
		"reason":     stored.Reason,
	})
	return result
}

func (h webhookHandler) now() time.Time {
	if h.Clock != nil {
		return h.Clock()
	}
	return time.Now().UTC()
}

func (h webhookHandler) startWebhookRequest(
	ctx context.Context,
	provider webhook.Provider,
) (context.Context, trace.Span, time.Time) {
	startedAt := h.now()
	if h.Tracer == nil {
		return ctx, nil, startedAt
	}
	ctx, span := h.Tracer.Start(ctx, telemetry.SpanWebhookHandle,
		trace.WithAttributes(telemetry.AttrProvider(providerValue(provider))),
	)
	return ctx, span, startedAt
}

func (h webhookHandler) finishWebhookRequest(
	ctx context.Context,
	span trace.Span,
	startedAt time.Time,
	provider webhook.Provider,
	result webhookTelemetryResult,
) {
	attrs := []attribute.KeyValue{
		telemetry.AttrProvider(providerValue(provider)),
		telemetry.AttrOutcome(fallbackValue(result.Outcome)),
		telemetry.AttrReason(fallbackValue(result.Reason)),
	}
	if span != nil {
		span.SetAttributes(append(attrs,
			telemetry.AttrEventKind(eventKindValue(result.EventKind)),
			telemetry.AttrDecision(decisionValue(result.Decision)),
			telemetry.AttrStatus(statusValue(result.Status)),
		)...)
		span.End()
	}
	if h.Instruments == nil {
		return
	}
	opts := metric.WithAttributes(attrs...)
	h.Instruments.WebhookRequests.Add(ctx, 1, opts)
	h.Instruments.WebhookRequestDuration.Record(ctx, h.durationSeconds(startedAt), opts)
}

func (h webhookHandler) startWebhookStore(
	ctx context.Context,
	trigger webhook.Trigger,
) (context.Context, trace.Span, time.Time) {
	startedAt := h.now()
	if h.Tracer == nil {
		return ctx, nil, startedAt
	}
	ctx, span := h.Tracer.Start(ctx, telemetry.SpanWebhookStore,
		trace.WithAttributes(
			telemetry.AttrProvider(providerValue(trigger.Provider)),
			telemetry.AttrEventKind(eventKindValue(trigger.EventKind)),
			telemetry.AttrDecision(decisionValue(trigger.Decision)),
		),
	)
	return ctx, span, startedAt
}

func (h webhookHandler) finishWebhookStore(
	ctx context.Context,
	span trace.Span,
	startedAt time.Time,
	provider webhook.Provider,
	result webhookTelemetryResult,
) {
	attrs := []attribute.KeyValue{
		telemetry.AttrProvider(providerValue(provider)),
		telemetry.AttrOutcome(fallbackValue(result.Outcome)),
		telemetry.AttrStatus(statusValue(result.Status)),
	}
	if span != nil {
		span.SetAttributes(attrs...)
		span.End()
	}
	if h.Instruments == nil {
		return
	}
	opts := metric.WithAttributes(attrs...)
	h.Instruments.WebhookStoreOperations.Add(ctx, 1, opts)
	h.Instruments.WebhookStoreDuration.Record(ctx, h.durationSeconds(startedAt), opts)
}

func (h webhookHandler) recordWebhookDecision(
	ctx context.Context,
	trigger webhook.Trigger,
	stored webhook.StoredTrigger,
) {
	if h.Instruments == nil {
		return
	}
	h.Instruments.WebhookTriggerDecisions.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrProvider(providerValue(trigger.Provider)),
		telemetry.AttrEventKind(eventKindValue(trigger.EventKind)),
		telemetry.AttrDecision(decisionValue(trigger.Decision)),
		telemetry.AttrReason(reasonValue(trigger.Reason)),
		telemetry.AttrStatus(statusValue(stored.Status)),
	))
}

func (h webhookHandler) durationSeconds(startedAt time.Time) float64 {
	duration := h.now().Sub(startedAt).Seconds()
	if duration < 0 {
		return 0
	}
	return duration
}

func providerValue(provider webhook.Provider) string {
	if provider == "" {
		return webhookValueUnknown
	}
	return string(provider)
}

func eventKindValue(eventKind webhook.EventKind) string {
	if eventKind == "" {
		return webhookValueUnknown
	}
	return string(eventKind)
}

func decisionValue(decision webhook.Decision) string {
	if decision == "" {
		return webhookValueUnknown
	}
	return string(decision)
}

func statusValue(status webhook.TriggerStatus) string {
	if status == "" {
		return webhookStatusUnknown
	}
	return string(status)
}

func reasonValue(reason webhook.DecisionReason) string {
	if reason == "" {
		return webhookReasonNone
	}
	return string(reason)
}

func fallbackValue(value string) string {
	if value == "" {
		return webhookValueUnknown
	}
	return value
}

func writeWebhookJSON(w http.ResponseWriter, status int, payload map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
