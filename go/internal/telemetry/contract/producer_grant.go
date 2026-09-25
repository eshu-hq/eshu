// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

// Producer-grant decision telemetry (#6726). Grant admission decides allow or
// deny for a core-owned fact kind at install, readback, activation, and on
// every emitted result. This family gives an operator one closed-cardinality
// signal that separates a grant allow from a grant deny, and says why.
//
// Instrument driven by this contract:
//
//   - eshu_dp_component_producer_grant_decisions_total (decision, stage, reason,
//     fact_kind) — one increment per grant decision. decision is allow|deny;
//     stage is install|readback|activation|emission; reason is "granted" for an
//     allow and one of the closed ProducerGrantReason* deny reasons otherwise;
//     fact_kind is a registered core fact kind (bounded by the fact-kind
//     registry) or "other". The producer id is operator-configured and
//     therefore unbounded, so it is NEVER a metric label; it rides only on the
//     span event and the log line.
//
// Nothing in this family may carry a credential, token, grant secret, scope
// value, or payload content.

// MetricProducerGrantDecisions is the decision counter name.
const MetricProducerGrantDecisions = "eshu_dp_component_producer_grant_decisions_total"

// SpanEventProducerGrantDecision names the span event recorded on the span
// active at a grant decision site (the claimed-run span for the per-emission
// recheck).
const SpanEventProducerGrantDecision = "component.producer_grant.decision"

const (
	// SpanAttrProducerGrantProducerID is the producer (component) id, carried
	// on the span event only because it is operator-configured and unbounded.
	SpanAttrProducerGrantProducerID = "eshu.producer_grant.producer_id"
	// SpanAttrProducerGrantVersion is the producer manifest version, carried
	// on the span event only.
	SpanAttrProducerGrantVersion = "eshu.producer_grant.version"
)

const (
	// LogKeyProducerGrantProducerID is the producer id on a decision log line.
	LogKeyProducerGrantProducerID = "producer_grant.producer_id"
	// LogKeyProducerGrantVersion is the producer version on a decision log line.
	LogKeyProducerGrantVersion = "producer_grant.version"
	// LogKeyProducerGrantDecision is allow or deny on a decision log line.
	LogKeyProducerGrantDecision = "producer_grant.decision"
	// LogKeyProducerGrantStage is the decision stage on a decision log line.
	LogKeyProducerGrantStage = "producer_grant.stage"
	// LogKeyProducerGrantReason is the closed decision reason on a decision
	// log line.
	LogKeyProducerGrantReason = "producer_grant.reason"
	// LogKeyProducerGrantFactKind is the core fact kind on a decision log line.
	LogKeyProducerGrantFactKind = "producer_grant.fact_kind"
)

// ProducerGrantDecision* are the closed values of the decision label.
const (
	ProducerGrantDecisionAllow = "allow"
	ProducerGrantDecisionDeny  = "deny"
)

// ProducerGrantStage* are the closed values of the stage label.
const (
	ProducerGrantStageInstall    = "install"
	ProducerGrantStageReadback   = "readback"
	ProducerGrantStageActivation = "activation"
	ProducerGrantStageEmission   = "emission"
)

// ProducerGrantReason* are the closed values of the reason label. An allow
// always uses ProducerGrantReasonGranted; every deny uses one of the others.
// ProducerGrantReasonUnknown is the recorder's fallback for a value outside
// the closed set, so a caller bug can never widen the label space.
const (
	ProducerGrantReasonGranted          = "granted"
	ProducerGrantReasonNoMatchingGrant  = "no_matching_grant"
	ProducerGrantReasonRevoked          = "revoked"
	ProducerGrantReasonExpired          = "expired"
	ProducerGrantReasonScopeMismatch    = "scope_mismatch"
	ProducerGrantReasonSchemaNotCovered = "schema_not_covered"
	ProducerGrantReasonGrantsUnreadable = "grants_unreadable"
	ProducerGrantReasonUnknown          = "unknown"
)

// ProducerGrantFactKindOther labels a fact kind that is not a registered core
// kind. The recorder folds any such value to it so the fact_kind label stays
// bounded by the fact-kind registry.
const ProducerGrantFactKindOther = "other"
