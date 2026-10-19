// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

// Claim-quota telemetry (#8471). Per-component generation quotas throttle a
// component even on an idle system; without these signals a throttled system
// is indistinguishable from a slow one. scope_id is per-operator data and
// therefore unbounded, so it is NEVER a metric label; it rides only on the
// fallback log line.
//
// Instruments driven by this contract:
//
//   - eshu_dp_workflow_claim_quota_exceeded_total (collector_kind,
//     quota_kind) — one increment per claim that found eligible work but
//     granted nothing because the quota blocked it.
//   - eshu_dp_workflow_claim_quota_unreadable_fallback_total
//     (collector_kind) — one increment per claim that took the floor-rate
//     fallback because the quota state was unreadable.
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
	// LogKeyQuotaScope is the scope on the floor-fallback log line (logs
	// only, never a metric label).
	LogKeyQuotaScope = "quota.scope_id"
)
