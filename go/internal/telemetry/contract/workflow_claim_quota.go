// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

// Claim-quota telemetry (#8471). Per-component generation quotas throttle a
// component even on an idle system; without these signals a throttled system
// is indistinguishable from a slow one. The budget is enforced per
// (component, scope) per hour: each scope row independently allows up to the
// limit, so a component with items in N scopes can grant up to N times the
// limit per hour. scope_id is per-operator data and therefore unbounded, so
// it is NEVER a metric label.
//
// Instruments driven by this contract:
//
//   - eshu_dp_workflow_claim_quota_exceeded_total (collector_kind,
//     quota_kind) — one increment per claim that found eligible work but
//     granted nothing because the quota blocked it.
//   - eshu_dp_workflow_claim_quota_unreadable_fallback_total
//     (collector_kind) — one increment per admitted floor-rate fallback
//     call, after the floor budget check passes, because the quota state
//     was unreadable. Calls denied by an exhausted floor tick nothing.
const (
	// MetricWorkflowClaimQuotaExceeded is the quota-throttle counter name.
	MetricWorkflowClaimQuotaExceeded = "eshu_dp_workflow_claim_quota_exceeded_total"
	// MetricWorkflowClaimQuotaUnreadableFallback is the floor-fallback
	// counter name.
	MetricWorkflowClaimQuotaUnreadableFallback = "eshu_dp_workflow_claim_quota_unreadable_fallback_total"
	// MetricDimensionQuotaKind labels quota instruments with the closed
	// quota-kind set below.
	MetricDimensionQuotaKind = "quota_kind"
)

// QuotaKind* are the closed values of the quota_kind label.
const (
	// QuotaKindGenerationsPerHour is the only runtime-enforced quota kind
	// today; the manifest's facts-per-generation bound has no
	// per-generation emitted-fact counter yet (follow-up on #8471).
	QuotaKindGenerationsPerHour = "generations_per_hour"
)

const (
	// LogKeyQuotaCollectorKind is the collector kind on a quota log line.
	LogKeyQuotaCollectorKind = "quota.collector_kind"
	// LogKeyQuotaKind is the quota kind on a quota log line.
	LogKeyQuotaKind = "quota.quota_kind"
	// LogKeyQuotaLimit is the enforced limit on a quota log line.
	LogKeyQuotaLimit = "quota.limit"
	// LogKeyQuotaScope is reserved for future use: scope_id is
	// per-operator data and therefore unbounded, so it must never become
	// a metric label, and no log line emits this key today — the floor
	// fallback never learns the scope (its budget is per component by
	// design). Kept registered so the frozen key set stays stable.
	LogKeyQuotaScope = "quota.scope_id"
)
