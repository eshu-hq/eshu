// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/webhook"
	"go.opentelemetry.io/otel/trace"
)

const webhookReasonUnsupported = "unsupported_event"

func (h webhookHandler) handlePagerDuty(w http.ResponseWriter, r *http.Request) {
	ctx, span, startedAt := h.startWebhookRequest(r.Context(), webhook.ProviderPagerDuty)
	r = r.WithContext(ctx)
	result := webhookTelemetryResult{Outcome: webhookOutcomeRejected, Reason: webhookReasonMalformed}
	defer func() {
		h.finishWebhookRequest(ctx, span, startedAt, webhook.ProviderPagerDuty, result)
	}()

	payload, readReason, ok := h.readPostBody(w, r)
	if !ok {
		result.Reason = readReason
		return
	}
	if err := webhook.VerifyPagerDutySignature(payload, h.Config.PagerDutySecret, r.Header.Get("X-PagerDuty-Signature")); err != nil {
		result.Reason = webhookReasonAuth
		http.Error(w, "signature verification failed", http.StatusUnauthorized)
		return
	}
	deliveryID := strings.TrimSpace(firstNonEmpty(
		r.Header.Get("X-Webhook-Id"),
		r.Header.Get("X-Request-Id"),
	))
	trigger, err := webhook.NormalizePagerDutyIncidentFreshness(
		h.Config.PagerDutyScopeID,
		deliveryID,
		payload,
		h.now(),
	)
	if err != nil && deliveryID == "" && strings.Contains(err.Error(), "event_id") {
		result.Reason = webhookReasonDelivery
		http.Error(w, "missing delivery id", http.StatusBadRequest)
		return
	}
	result = h.storeIncidentFreshnessAndWrite(w, r, trigger, err)
}

func (h webhookHandler) handleJira(w http.ResponseWriter, r *http.Request) {
	ctx, span, startedAt := h.startWebhookRequest(r.Context(), webhook.ProviderJira)
	r = r.WithContext(ctx)
	result := webhookTelemetryResult{Outcome: webhookOutcomeRejected, Reason: webhookReasonMalformed}
	defer func() {
		h.finishWebhookRequest(ctx, span, startedAt, webhook.ProviderJira, result)
	}()

	payload, readReason, ok := h.readPostBody(w, r)
	if !ok {
		result.Reason = readReason
		return
	}
	if err := webhook.VerifyJiraSignature(payload, h.Config.JiraSecret, r.Header.Get("X-Hub-Signature")); err != nil {
		result.Reason = webhookReasonAuth
		http.Error(w, "signature verification failed", http.StatusUnauthorized)
		return
	}
	deliveryID := strings.TrimSpace(firstNonEmpty(
		r.Header.Get("X-Atlassian-Webhook-Identifier"),
		r.Header.Get("X-Request-Id"),
	))
	if deliveryID == "" {
		result.Reason = webhookReasonDelivery
		http.Error(w, "missing delivery id", http.StatusBadRequest)
		return
	}
	trigger, err := webhook.NormalizeJiraIncidentFreshness(
		h.Config.JiraScopeID,
		deliveryID,
		payload,
		h.now(),
	)
	result = h.storeIncidentFreshnessAndWrite(w, r, trigger, err)
}

func (h webhookHandler) storeIncidentFreshnessAndWrite(
	w http.ResponseWriter,
	r *http.Request,
	trigger webhook.IncidentFreshnessTrigger,
	normalizeErr error,
) webhookTelemetryResult {
	result := webhookTelemetryResult{
		Outcome:   webhookOutcomeRejected,
		Reason:    webhookReasonMalformed,
		EventKind: webhook.EventKind(trigger.EventKind),
		Decision:  webhook.DecisionAccepted,
	}
	if normalizeErr != nil {
		if errors.Is(normalizeErr, webhook.ErrUnsupportedIncidentFreshnessEvent) {
			result.Reason = webhookReasonUnsupported
			http.Error(w, "unsupported webhook event", http.StatusBadRequest)
			return result
		}
		http.Error(w, "unsupported or malformed webhook event", http.StatusBadRequest)
		return result
	}
	storeCtx, span, startedAt := h.startIncidentFreshnessStore(r.Context(), trigger)
	stored, err := h.IncidentFreshnessStore.StoreIncidentFreshnessTrigger(storeCtx, trigger, h.now())
	if err != nil {
		result.Outcome = webhookOutcomeFailed
		result.Reason = webhookReasonStore
		h.finishWebhookStore(storeCtx, span, startedAt, trigger.Provider, result)
		h.logIncidentFreshnessStoreError(r.Context(), trigger, err)
		http.Error(w, "store incident freshness trigger", http.StatusInternalServerError)
		return result
	}
	result.Outcome = webhookOutcomeStored
	result.Reason = webhookReasonNone
	result.Status = stored.Status
	h.finishWebhookStore(storeCtx, span, startedAt, trigger.Provider, result)
	h.recordIncidentFreshnessDecision(storeCtx, trigger, stored)
	writeWebhookJSON(w, http.StatusAccepted, map[string]any{
		"trigger_id": stored.TriggerID,
		"status":     stored.Status,
		"decision":   webhook.DecisionAccepted,
		"reason":     webhookReasonNone,
	})
	return result
}

func (h webhookHandler) startIncidentFreshnessStore(
	ctx context.Context,
	trigger webhook.IncidentFreshnessTrigger,
) (context.Context, trace.Span, time.Time) {
	return h.startWebhookStore(ctx, webhook.Trigger{
		Provider:  trigger.Provider,
		EventKind: webhook.EventKind(trigger.EventKind),
		Decision:  webhook.DecisionAccepted,
	})
}

func (h webhookHandler) recordIncidentFreshnessDecision(
	ctx context.Context,
	trigger webhook.IncidentFreshnessTrigger,
	stored webhook.StoredIncidentFreshnessTrigger,
) {
	h.recordWebhookDecision(ctx, webhook.Trigger{
		Provider:  trigger.Provider,
		EventKind: webhook.EventKind(trigger.EventKind),
		Decision:  webhook.DecisionAccepted,
	}, webhook.StoredTrigger{
		Trigger: webhook.Trigger{
			Provider:  trigger.Provider,
			EventKind: webhook.EventKind(trigger.EventKind),
			Decision:  webhook.DecisionAccepted,
		},
		Status: stored.Status,
	})
}

func (h webhookHandler) logIncidentFreshnessStoreError(
	ctx context.Context,
	trigger webhook.IncidentFreshnessTrigger,
	err error,
) {
	if h.Logger == nil {
		return
	}
	h.Logger.ErrorContext(
		ctx, "incident freshness trigger persistence failed",
		slog.String("provider", string(trigger.Provider)),
		slog.String("event_kind", trigger.EventKind),
		slog.String("error", err.Error()),
	)
}
