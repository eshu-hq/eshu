// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/webhook"
)

const webhookReasonDeliveryConflict = "delivery_conflict"

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
	event := strings.TrimSpace(r.Header.Get("X-GitHub-Event"))
	if strings.EqualFold(event, githubEventPing) {
		result = h.writeGitHubPingAccepted(w)
		return
	}
	if event == "workflow_run" || event == "workflow_job" {
		result = h.storeGitHubActionsVerified(w, r, event, deliveryID, payload)
		return
	}
	trigger, err := webhook.NormalizeGitHub(
		event,
		deliveryID,
		payload,
		h.Config.DefaultBranch,
	)
	result = h.storeAndWrite(w, r, trigger, err)
}

func (h webhookHandler) writeGitHubPingAccepted(w http.ResponseWriter) webhookTelemetryResult {
	writeWebhookJSON(w, http.StatusAccepted, map[string]any{
		"status":   webhook.TriggerStatusIgnored,
		"decision": webhook.DecisionIgnored,
		"reason":   webhookReasonPing,
	})
	return webhookTelemetryResult{
		Outcome:   webhookOutcomeIgnored,
		Reason:    webhookReasonPing,
		EventKind: webhook.EventKind(webhookReasonPing),
		Decision:  webhook.DecisionIgnored,
		Status:    webhook.TriggerStatusIgnored,
	}
}

func (h webhookHandler) now() time.Time {
	if h.Clock != nil {
		return h.Clock()
	}
	return time.Now().UTC()
}

func (h webhookHandler) storeGitHubActionsVerified(w http.ResponseWriter, r *http.Request, event, deliveryID string, payload []byte) webhookTelemetryResult {
	result := webhookTelemetryResult{Outcome: webhookOutcomeRejected, Reason: webhookReasonMalformed, EventKind: webhook.EventKind(event)}
	trigger, err := webhook.NormalizeGitHubActions(event, deliveryID, payload)
	if err != nil {
		http.Error(w, "malformed github actions event", http.StatusBadRequest)
		return result
	}
	if h.ActionsStore == nil {
		result.Outcome = webhookOutcomeFailed
		result.Reason = webhookReasonStore
		http.Error(w, "actions delivery store unavailable", http.StatusServiceUnavailable)
		return result
	}
	stored, err := h.ActionsStore.Store(r.Context(), trigger, h.now())
	if err != nil {
		if errors.Is(err, webhook.ErrActionsDeliveryConflict) {
			result.Outcome = webhookOutcomeRejected
			result.Reason = webhookReasonDeliveryConflict
			http.Error(w, "conflicting actions delivery", http.StatusConflict)
			return result
		}
		result.Outcome = webhookOutcomeFailed
		result.Reason = webhookReasonStore
		http.Error(w, "store actions delivery", http.StatusInternalServerError)
		return result
	}
	result.Outcome = webhookOutcomeStored
	result.Reason = webhookReasonNone
	result.Status = stored.Status
	result.Decision = webhook.DecisionAccepted
	writeWebhookJSON(w, http.StatusAccepted, map[string]any{"status": stored.Status, "decision": webhook.DecisionAccepted})
	return result
}
