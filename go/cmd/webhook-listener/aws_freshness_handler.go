package main

import (
	"context"
	"crypto/hmac"
	"log/slog"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/freshness"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/webhook"
	"go.opentelemetry.io/otel/metric"
)

const (
	awsFreshnessReasonAuth      = "auth_failed"
	awsFreshnessReasonMalformed = "malformed_event"
	awsFreshnessReasonStore     = "store_failed"
	awsFreshnessReasonNone      = "none"
	awsFreshnessActionStored    = "intake_stored"
	awsFreshnessActionCoalesced = "intake_coalesced"
	awsFreshnessActionRejected  = "intake_rejected"
	awsFreshnessActionFailed    = "intake_failed"
	awsFreshnessKindUnknown     = "unknown"
)

func (h webhookHandler) handleAWSFreshnessEventBridge(w http.ResponseWriter, r *http.Request) {
	ctx, span, startedAt := h.startWebhookRequest(r.Context(), webhookProviderAWSFreshness)
	r = r.WithContext(ctx)
	result := webhookTelemetryResult{Outcome: webhookOutcomeRejected, Reason: awsFreshnessReasonMalformed}
	defer func() {
		h.finishWebhookRequest(ctx, span, startedAt, webhookProviderAWSFreshness, result)
	}()

	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		result.Reason = webhookReasonBadMethod
		h.recordAWSFreshnessEvent(r.Context(), awsFreshnessKindUnknown, awsFreshnessActionRejected)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !validAWSFreshnessToken(r, h.Config.AWSFreshnessToken) {
		result.Reason = awsFreshnessReasonAuth
		h.recordAWSFreshnessEvent(r.Context(), awsFreshnessKindUnknown, awsFreshnessActionRejected)
		http.Error(w, "token verification failed", http.StatusUnauthorized)
		return
	}
	payload, readReason, ok := h.readPostBody(w, r)
	if !ok {
		result.Reason = readReason
		h.recordAWSFreshnessEvent(r.Context(), awsFreshnessKindUnknown, awsFreshnessActionRejected)
		return
	}

	trigger, err := freshness.NormalizeEventBridge(payload)
	result.EventKind = webhookEventKindAWSFreshness(trigger.Kind)
	if err != nil {
		result.Reason = awsFreshnessReasonMalformed
		h.recordAWSFreshnessEvent(r.Context(), awsFreshnessKindUnknown, awsFreshnessActionRejected)
		http.Error(w, "unsupported or malformed AWS freshness event", http.StatusBadRequest)
		return
	}
	stored, err := h.AWSFreshnessStore.StoreTrigger(r.Context(), trigger, h.now())
	if err != nil {
		result.Outcome = webhookOutcomeFailed
		result.Reason = awsFreshnessReasonStore
		h.recordAWSFreshnessEvent(r.Context(), string(trigger.Kind), awsFreshnessActionFailed)
		h.logAWSFreshnessStoreError(r.Context(), trigger, err)
		http.Error(w, "store AWS freshness trigger", http.StatusInternalServerError)
		return
	}
	result.Outcome = webhookOutcomeStored
	result.Reason = awsFreshnessReasonNone
	result.Status = webhook.TriggerStatus(stored.Status)
	h.recordAWSFreshnessEvent(r.Context(), string(trigger.Kind), awsFreshnessIntakeAction(stored))
	writeWebhookJSON(w, http.StatusAccepted, map[string]any{
		"trigger_id": stored.TriggerID,
		"status":     stored.Status,
		"kind":       stored.Kind,
	})
}

func validAWSFreshnessToken(r *http.Request, expected string) bool {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return false
	}
	candidates := []string{
		strings.TrimSpace(r.Header.Get("X-Eshu-AWS-Freshness-Token")),
		awsFreshnessBearerToken(r.Header.Get("Authorization")),
	}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if hmac.Equal([]byte(candidate), []byte(expected)) {
			return true
		}
	}
	return false
}

func awsFreshnessBearerToken(header string) string {
	header = strings.TrimSpace(header)
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func (h webhookHandler) recordAWSFreshnessEvent(ctx context.Context, kind string, action string) {
	if h.Instruments == nil {
		return
	}
	h.Instruments.AWSFreshnessEvents.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrKind(fallbackValue(kind)),
		telemetry.AttrAction(fallbackValue(action)),
	))
}

func awsFreshnessIntakeAction(stored freshness.StoredTrigger) string {
	if stored.DuplicateCount > 0 {
		return awsFreshnessActionCoalesced
	}
	return awsFreshnessActionStored
}

func (h webhookHandler) logAWSFreshnessStoreError(ctx context.Context, trigger freshness.Trigger, err error) {
	if h.Logger == nil {
		return
	}
	h.Logger.ErrorContext(ctx, "AWS freshness trigger persistence failed",
		slog.String("kind", string(trigger.Kind)),
		slog.String("account", trigger.AccountID),
		slog.String("region", trigger.Region),
		slog.String("service", trigger.ServiceKind),
		slog.String("error", err.Error()),
	)
}

func webhookEventKindAWSFreshness(kind freshness.EventKind) webhook.EventKind {
	if kind == "" {
		return webhook.EventKind(awsFreshnessKindUnknown)
	}
	return webhook.EventKind(kind)
}

const webhookProviderAWSFreshness webhook.Provider = "aws_freshness"
