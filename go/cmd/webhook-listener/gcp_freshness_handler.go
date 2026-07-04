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

	// gcpFreshnessAuthPathSharedToken and gcpFreshnessAuthPathOIDC are the
	// bounded auth_path label values recorded on every GCP freshness event.
	// gcpFreshnessAuthPathNone marks a request that satisfied neither accepted
	// auth path. These three values are the closed label vocabulary; never
	// derive this label from request headers or claims directly.
	gcpFreshnessAuthPathSharedToken = "shared_token"
	gcpFreshnessAuthPathOIDC        = "oidc"
	gcpFreshnessAuthPathNone        = "none"
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
		h.recordGCPFreshnessAuthEvent(r.Context(), gcpFreshnessKindUnknown, gcpFreshnessActionRejected, gcpFreshnessAuthPathNone)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Two independent, fail-closed accepted auth paths: the shared token
	// (backward-compatible; also used by the documented push-forwarder) or a
	// verified Pub/Sub push OIDC token (#4659). Either is sufficient; neither
	// introduces an anonymous or partially-authenticated bypass — an absent
	// or misconfigured path simply never validates.
	authPath, authenticated := h.authenticateGCPFreshnessPush(r)
	if !authenticated {
		result.Reason = gcpFreshnessReasonAuth
		h.recordGCPFreshnessAuthEvent(r.Context(), gcpFreshnessKindUnknown, gcpFreshnessActionRejected, authPath)
		http.Error(w, "token verification failed", http.StatusUnauthorized)
		return
	}
	payload, readReason, ok := h.readPostBody(w, r)
	if !ok {
		result.Reason = readReason
		h.recordGCPFreshnessAuthEvent(r.Context(), gcpFreshnessKindUnknown, gcpFreshnessActionRejected, authPath)
		return
	}

	trigger, err := freshness.NormalizePubSubPush(payload)
	if errors.Is(err, freshness.ErrWelcomeMessage) {
		// The first delivery to a new CAI feed subscription is a benign
		// welcome message, not a malformed event. Acknowledge it (2xx) so
		// Pub/Sub does not retry it, but never store it as a trigger.
		result.Outcome = webhookOutcomeIgnored
		result.Reason = gcpFreshnessReasonNone
		h.recordGCPFreshnessAuthEvent(r.Context(), gcpFreshnessKindUnknown, gcpFreshnessActionIgnored, authPath)
		writeWebhookJSON(w, http.StatusAccepted, map[string]any{
			"status": "ignored",
			"reason": "welcome_message",
		})
		return
	}
	result.EventKind = webhookEventKindGCPFreshness(trigger.Kind)
	if err != nil {
		result.Reason = gcpFreshnessReasonMalformed
		h.recordGCPFreshnessAuthEvent(r.Context(), gcpFreshnessKindUnknown, gcpFreshnessActionRejected, authPath)
		http.Error(w, "unsupported or malformed GCP freshness event", http.StatusBadRequest)
		return
	}
	stored, err := h.GCPFreshnessStore.StoreTrigger(r.Context(), trigger, h.now())
	if err != nil {
		result.Outcome = webhookOutcomeFailed
		result.Reason = gcpFreshnessReasonStore
		h.recordGCPFreshnessAuthEvent(r.Context(), string(trigger.Kind), gcpFreshnessActionFailed, authPath)
		h.logGCPFreshnessStoreError(r.Context(), trigger, err)
		http.Error(w, "store GCP freshness trigger", http.StatusInternalServerError)
		return
	}
	result.Outcome = webhookOutcomeStored
	result.Reason = gcpFreshnessReasonNone
	result.Status = webhook.TriggerStatus(stored.Status)
	h.recordGCPFreshnessAuthEvent(r.Context(), string(trigger.Kind), gcpFreshnessIntakeAction(stored), authPath)
	writeWebhookJSON(w, http.StatusAccepted, map[string]any{
		"trigger_id": stored.TriggerID,
		"status":     stored.Status,
		"kind":       stored.Kind,
	})
}

// authenticateGCPFreshnessPush checks both accepted auth paths for the GCP
// freshness push route and reports which one (if any) succeeded, for the
// bounded auth_path telemetry label. It fails closed: if neither path
// validates, it returns (gcpFreshnessAuthPathNone, false).
//
// The shared-token check runs first because it is a fast, local, constant-time
// comparison; the OIDC check only runs (and only makes a cert-fetch network
// call, cached after the first request) when the shared token does not match,
// so a request configured for shared-token auth never pays the OIDC cost.
func (h webhookHandler) authenticateGCPFreshnessPush(r *http.Request) (string, bool) {
	if validGCPFreshnessToken(r, h.Config.GCPFreshnessToken) {
		return gcpFreshnessAuthPathSharedToken, true
	}
	if verifyGCPPushOIDC(
		r.Context(),
		r,
		h.GCPPushOIDCValidator,
		h.Config.GCPFreshnessOIDCAudience,
		h.Config.GCPFreshnessOIDCAllowedSA,
	) {
		return gcpFreshnessAuthPathOIDC, true
	}
	return gcpFreshnessAuthPathNone, false
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

// recordGCPFreshnessAuthEvent records a GCP freshness intake event with the
// bounded kind, action, and auth_path labels. auth_path is one of
// gcpFreshnessAuthPathSharedToken, gcpFreshnessAuthPathOIDC, or
// gcpFreshnessAuthPathNone — never a raw header, token, or claim value.
func (h webhookHandler) recordGCPFreshnessAuthEvent(ctx context.Context, kind string, action string, authPath string) {
	if h.Instruments == nil {
		return
	}
	h.Instruments.GCPFreshnessEvents.Add(ctx, 1, metric.WithAttributes(
		telemetry.AttrKind(fallbackValue(kind)),
		telemetry.AttrAction(fallbackValue(action)),
		telemetry.AttrAuthPath(fallbackValue(authPath)),
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
