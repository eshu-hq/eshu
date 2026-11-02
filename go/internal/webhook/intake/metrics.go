// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package intake

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/webhook"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Request-path label values. They match the values the in-tree listener used
// for the same four instruments, so the existing alert and dashboard queries
// (eshu_dp_webhook_trigger_decisions_total{status="failed"} and friends) match
// without a query change. Every value is a fixed enum: no delivery id, repo
// name, or other wire string becomes a label.
const (
	outcomeStored   = "stored"
	outcomeRejected = "rejected"
	outcomeFailed   = "failed"

	reasonNone           = "none"
	reasonAuthFailed     = "auth_failed"
	reasonBadMethod      = "bad_method"
	reasonBodyTooLarge   = "body_too_large"
	reasonInvalidBody    = "invalid_body"
	reasonEmptyBody      = "empty_body"
	reasonMalformedEvent = "malformed_event"
	reasonStoreFailed    = "store_failed"
	reasonOther          = "other"

	valueUnknown = "unknown"

	// statusFailed labels a trigger whose durable store write failed, the
	// value the EshuTriggerHandoffFailed alert selects on.
	statusFailed = string(webhook.TriggerStatusFailed)

	providerAWSFreshness = "aws_freshness"
	providerGCPFreshness = "gcp_freshness"
)

// triggerDecision carries the bounded labels of one trigger-decision series.
type triggerDecision struct {
	provider  string
	eventKind string
	decision  string
	reason    string
}

// requestObs records the request-path series for one intake request. It
// starts as a rejected malformed request, the listener's default, and the
// handler narrows it as the request clears each gate. A nil Instruments makes
// every method a cheap no-op so a host without telemetry still serves.
type requestObs struct {
	instruments *telemetry.Instruments
	ctx         context.Context
	started     time.Time
	provider    string
	outcome     string
	reason      string
}

// observe begins observation of one intake request. provider is the best
// bounded provider known before the body is decoded.
func (s *Server) observe(r *http.Request, provider string) *requestObs {
	return &requestObs{
		instruments: s.config.Instruments,
		ctx:         r.Context(),
		started:     time.Now(),
		provider:    provider,
		outcome:     outcomeRejected,
		reason:      reasonMalformedEvent,
	}
}

// reject marks the request as rejected before reaching storage.
func (o *requestObs) reject(reason string) {
	o.outcome = outcomeRejected
	o.reason = reason
}

// finish records the request counter and duration histogram. Handlers defer
// it so every return path, including panics unwinding through net/http, is
// counted exactly once.
func (o *requestObs) finish() {
	if o.instruments == nil {
		return
	}
	opts := metric.WithAttributes(
		telemetry.AttrProvider(o.provider),
		telemetry.AttrOutcome(o.outcome),
		telemetry.AttrReason(o.reason),
	)
	o.instruments.WebhookRequests.Add(o.ctx, 1, opts)
	o.instruments.WebhookRequestDuration.Record(o.ctx, time.Since(o.started).Seconds(), opts)
}

// stored records a successful durable write: the store operation, its
// duration, and one trigger decision labelled with the stored status.
func (o *requestObs) stored(d triggerDecision, startedAt time.Time, status string) {
	o.outcome = outcomeStored
	o.reason = reasonNone
	status = boundedStatus(status)
	o.recordStore(d, startedAt, outcomeStored, status)
	o.recordDecision(d, status)
}

// storeFailed records a failed durable write. The trigger decision carries
// status "failed", which feeds the freshness handoff alert.
func (o *requestObs) storeFailed(d triggerDecision, startedAt time.Time) {
	o.outcome = outcomeFailed
	o.reason = reasonStoreFailed
	o.recordStore(d, startedAt, outcomeFailed, statusFailed)
	o.recordDecision(d, statusFailed)
}

// storeUnavailable records a route whose store is not configured: the trigger
// cannot be handed off, so it counts as a failed decision, but no store
// operation ran.
func (o *requestObs) storeUnavailable(d triggerDecision) {
	o.outcome = outcomeFailed
	o.reason = reasonStoreFailed
	o.recordDecision(d, statusFailed)
}

// storeRejected records a store call the store itself refused as a conflict.
// The store worked, so only its duration and operation are counted; the
// request is a rejection, not a failed handoff.
func (o *requestObs) storeRejected(provider string, startedAt time.Time) {
	o.reject(codeDeliveryConflict)
	o.recordStore(triggerDecision{provider: provider}, startedAt, outcomeRejected, valueUnknown)
}

func (o *requestObs) recordStore(d triggerDecision, startedAt time.Time, outcome, status string) {
	if o.instruments == nil {
		return
	}
	opts := metric.WithAttributes(
		telemetry.AttrProvider(d.provider),
		telemetry.AttrOutcome(outcome),
		telemetry.AttrStatus(status),
	)
	o.instruments.WebhookStoreOperations.Add(o.ctx, 1, opts)
	o.instruments.WebhookStoreDuration.Record(o.ctx, time.Since(startedAt).Seconds(), opts)
}

func (o *requestObs) recordDecision(d triggerDecision, status string) {
	if o.instruments == nil {
		return
	}
	o.instruments.WebhookTriggerDecisions.Add(o.ctx, 1, metric.WithAttributes(
		decisionAttributes(d, status)...,
	))
}

func decisionAttributes(d triggerDecision, status string) []attribute.KeyValue {
	return []attribute.KeyValue{
		telemetry.AttrProvider(d.provider),
		telemetry.AttrEventKind(d.eventKind),
		telemetry.AttrDecision(d.decision),
		telemetry.AttrReason(d.reason),
		telemetry.AttrStatus(status),
	}
}

// boundedStatus keeps the status label to the durable lifecycle states.
func boundedStatus(status string) string {
	switch webhook.TriggerStatus(status) {
	case webhook.TriggerStatusQueued, webhook.TriggerStatusIgnored,
		webhook.TriggerStatusClaimed, webhook.TriggerStatusHandedOff,
		webhook.TriggerStatusFailed:
		return status
	default:
		return valueUnknown
	}
}

// boundedDecisionReason keeps the reason label to the normalizer's reasons.
// The intake wire carries the reason as a free string, so anything else
// collapses to "other" instead of minting a series.
func boundedDecisionReason(reason webhook.DecisionReason) string {
	switch reason {
	case "":
		return reasonNone
	case webhook.ReasonNonDefaultBranch, webhook.ReasonTagRef,
		webhook.ReasonPullRequestNotMerged, webhook.ReasonMergeRequestNotMerged,
		webhook.ReasonMissingMergeCommit, webhook.ReasonDeletedBranch:
		return string(reason)
	default:
		return reasonOther
	}
}

// boundedIncidentEventKind keeps the incident event_kind label to the event
// types the PagerDuty and Jira normalizers emit; the wire value is free text.
func boundedIncidentEventKind(kind string) string {
	switch strings.TrimSpace(kind) {
	case "incident.triggered", "incident.acknowledged", "incident.unacknowledged",
		"incident.resolved", "incident.reopened", "incident.reassigned",
		"incident.escalated", "incident.annotated", "incident.priority_updated",
		"pagerduty.webhook",
		"jira:issue_created", "jira:issue_updated", "jira:issue_deleted":
		return strings.TrimSpace(kind)
	default:
		return reasonOther
	}
}
