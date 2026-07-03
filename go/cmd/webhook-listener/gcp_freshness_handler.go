// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"crypto/hmac"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud/freshness"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/webhook"
	log "github.com/eshu-hq/eshu/go/pkg/log"
	"go.opentelemetry.io/otel/metric"
)

const (
	gcpFreshnessReasonAuth      = "auth_failed"
	gcpFreshnessReasonMalformed = "malformed_event"
	gcpFreshnessReasonStore     = "store_failed"
	gcpFreshnessReasonNone      = "none"
	gcpFreshnessActionStored    = "intake_stored"
	gcpFreshnessActionCoalesced = "intake_coalesced"
	gcpFreshnessActionIgnored   = "intake_ignored"
	gcpFreshnessActionRejected  = "intake_rejected"
	gcpFreshnessActionFailed    = "intake_failed"
	gcpFreshnessKindUnknown     = "unknown"
)

func (h webhookHandler) handleGCPFreshnessPubSubPush(w http.ResponseWriter, r *http.Request) {
	ctx, span, startedAt := h.startWebhookRequest(r.Context(), webhookProviderGCPFreshness)
	r = r.WithContext(ctx)
	result := webhookTelemetryResult{Outcome: webhookOutcomeRejected, Reason: gcpFreshnessReasonMalformed}
	defer func() {
		h.finishWebhookRequest(ctx, span, startedAt, webhookProviderGCPFreshness, result)
	}()

	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		result.Reason = webhookReasonBadMethod
		h.recordGCPFreshnessEvent(r.Context(), gcpFreshnessKindUnknown, gcpFreshnessActionRejected)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// TODO(#4339): add Pub/Sub push OIDC token verification (audience +
	// service-account allowlist) as a second accepted auth path once the
	// dedicated security review for this endpoint lands. Until then the
	// shared X-Eshu-GCP-Freshness-Token below is the sole required auth
	// mechanism — there is no anonymous or partially-authenticated path, so
	// this stays fail-closed.
	if !validGCPFreshnessToken(r, h.Config.GCPFreshnessToken) {
		result.Reason = gcpFreshnessReasonAuth
		h.recordGCPFreshnessEvent(r.Context(), gcpFreshnessKindUnknown, gcpFreshnessActionRejected)
		http.Error(w, "token verification failed", http.StatusUnauthorized)
		return
	}
	payload, readReason, ok := h.readPostBody(w, r)
	if !ok {
		result.Reason = readReason
		h.recordGCPFreshnessEvent(r.Context(), gcpFreshnessKindUnknown, gcpFreshnessActionRejected)
		return
	}

	trigger, err := freshness.NormalizePubSubPush(payload)
	if errors.Is(err, freshness.ErrWelcomeMessage) {
		// The first delivery to a new CAI feed subscription is a benign
		// welcome message, not a malformed event. Acknowledge it (2xx) so
		// Pub/Sub does not retry it, but never store it as a trigger.
		result.Outcome = webhookOutcomeIgnored
		result.Reason = gcpFreshnessReasonNone
		h.recordGCPFreshnessEvent(r.Context(), gcpFreshnessKindUnknown, gcpFreshnessActionIgnored)
		writeWebhookJSON(w, http.StatusAccepted, map[string]any{
			"status": "ignored",
			"reason": "welcome_message",
		})
		return
	}
	result.EventKind = webhookEventKindGCPFreshness(trigger.Kind)
	if err != nil {
		result.Reason = gcpFreshnessReasonMalformed
		h.recordGCPFreshnessEvent(r.Context(), gcpFreshnessKindUnknown, gcpFreshnessActionRejected)
		http.Error(w, "unsupported or malformed GCP freshness event", http.StatusBadRequest)
		return
	}
	stored, err := h.GCPFreshnessStore.StoreTrigger(r.Context(), trigger, h.now())
	if err != nil {
		result.Outcome = webhookOutcomeFailed
		result.Reason = gcpFreshnessReasonStore
		h.recordGCPFreshnessEvent(r.Context(), string(trigger.Kind), gcpFreshnessActionFailed)
		h.logGCPFreshnessStoreError(r.Context(), trigger, err)
		http.Error(w, "store GCP freshness trigger", http.StatusInternalServerError)
		return
	}
	result.Outcome = webhookOutcomeStored
	result.Reason = gcpFreshnessReasonNone
	result.Status = webhook.TriggerStatus(stored.Status)
	h.recordGCPFreshnessEvent(r.Context(), string(trigger.Kind), gcpFreshnessIntakeAction(stored))
	writeWebhookJSON(w, http.StatusAccepted, map[string]any{
		"trigger_id": stored.TriggerID,
		"status":     stored.Status,
		"kind":       stored.Kind,
	})
}

// validGCPFreshnessToken reports whether the request carries the configured
// shared GCP freshness token, via either the X-Eshu-GCP-Freshness-Token
// header or an Authorization: Bearer header. It fails closed: an empty
// expected token never validates, matching validAWSFreshnessToken.
func validGCPFreshnessToken(r *http.Request, expected string) bool {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return false
	}
	candidates := []string{
		strings.TrimSpace(r.Header.Get("X-Eshu-GCP-Freshness-Token")),
		gcpFreshnessBearerToken(r.Header.Get("Authorization")),
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

func gcpFreshnessBearerToken(header string) string {
	header = strings.TrimSpace(header)
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

// verifyGCPPushOIDC always returns false.
//
// TODO(#4339): implement real Pub/Sub push OIDC token verification (audience
// + service-account allowlist) once the security review for default-on push
// auth lands. Until then this path is stubbed and fails closed: every
// request must authenticate via the shared X-Eshu-GCP-Freshness-Token
// instead.
func verifyGCPPushOIDC(_ *http.Request) bool {
	return false
}

func (h webhookHandler) recordGCPFreshnessEvent(ctx context.Context, kind string, action string) {
	if h.Instruments == nil {
		return
	}
	h.Instruments.GCPFreshnessEvents.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrKind(fallbackValue(kind)),
		telemetry.AttrAction(fallbackValue(action)),
	))
}

func gcpFreshnessIntakeAction(stored freshness.StoredTrigger) string {
	if stored.DuplicateCount > 0 {
		return gcpFreshnessActionCoalesced
	}
	return gcpFreshnessActionStored
}

func (h webhookHandler) logGCPFreshnessStoreError(ctx context.Context, trigger freshness.Trigger, err error) {
	if h.Logger == nil {
		return
	}
	h.Logger.ErrorContext(
		ctx, "GCP freshness trigger persistence failed",
		slog.String("kind", string(trigger.Kind)),
		slog.String("parent_scope_kind", string(trigger.ParentScopeKind)),
		slog.String("parent_scope_id", trigger.ParentScopeID),
		slog.String("asset_type", trigger.AssetType),
		log.Err(err),
	)
}

func webhookEventKindGCPFreshness(kind freshness.EventKind) webhook.EventKind {
	if kind == "" {
		return webhook.EventKind(gcpFreshnessKindUnknown)
	}
	return webhook.EventKind(kind)
}

const webhookProviderGCPFreshness webhook.Provider = "gcp_freshness"
