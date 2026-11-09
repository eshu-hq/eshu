// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"sync"
	"time"
)

// Staged audit operator metric names. Dimensions stay closed: deferrals carry
// only the reason, ticks carry only counts, and member identity travels in
// the tick record, never as a metric label.
const (
	MetricStagedAuditDeferralsTotal   = "eshu_dp_staged_audit_deferrals_total"
	MetricStagedAuditBatchCount       = "eshu_dp_staged_audit_batch_count"
	MetricStagedAuditMaxBatchDuration = "eshu_dp_staged_audit_max_batch_duration_seconds"
	MetricStagedAuditStagedFacts      = "eshu_dp_staged_audit_staged_facts"
	MetricStagedAuditDistinctKeys     = "eshu_dp_staged_audit_distinct_keys"
	MetricStagedAuditVerifyDiscards   = "eshu_dp_staged_audit_verify_discards_total"
	StagedAuditTelemetryReasonLabel   = "reason"
)

// StagedAuditTelemetry is the in-memory operator surface for the staged audit
// tick: a deferral counter split by closed reason, the last tick's batch
// count, max batch duration, and staged gauges, the member identity with
// replay lag at tick start and end, and the writer-verify discard rate.
type StagedAuditTelemetry struct {
	mu               sync.Mutex
	deferrals        map[StagedAuditDeferralReason]int64
	ticks            int64
	lastBatchCount   int
	lastMaxBatch     time.Duration
	lastStagedFacts  int
	lastDistinctKeys int
	lastMember       string
	lastLagStart     time.Duration
	lastLagEnd       time.Duration
	verifies         int64
	discards         int64
	unresolvable     int64
}

// NewStagedAuditTelemetry builds an empty telemetry recorder.
func NewStagedAuditTelemetry() *StagedAuditTelemetry {
	return &StagedAuditTelemetry{deferrals: make(map[StagedAuditDeferralReason]int64)}
}

// ObserveDeferral counts one deferred tick under its closed reason.
func (t *StagedAuditTelemetry) ObserveDeferral(reason StagedAuditDeferralReason) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	switch reason {
	case StagedAuditDeferBudget, StagedAuditDeferConflictExhausted, StagedAuditDeferBatchAbort:
	default:
		reason = StagedAuditDeferBatchAbort
	}
	t.deferrals[reason]++
}

// ObserveTick records one completed tick's gauges and member identity.
func (t *StagedAuditTelemetry) ObserveTick(result StagedAuditTickResult) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ticks++
	t.lastBatchCount = result.Batches
	t.lastMaxBatch = result.MaxBatchDuration
	t.lastStagedFacts = result.StagedFacts
	t.lastDistinctKeys = result.DistinctKeys
	t.lastMember = result.MemberID
	t.lastLagStart = result.ReplayLagStart
	t.lastLagEnd = result.ReplayLagEnd
}

// ObserveVerify counts one writer-side verify and its discards. False
// positives the writer discards are not capture holes.
func (t *StagedAuditTelemetry) ObserveVerify(discarded int64) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.verifies++
	t.discards += discarded
}

// ObserveUnresolvable counts candidate keys whose identifier could not be
// resolved because their fact vanished mid-pass. The writer verification
// re-derives winners from facts, so a key with no facts cannot produce a
// repair; the count keeps the skip visible instead of silent.
func (t *StagedAuditTelemetry) ObserveUnresolvable(n int) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.unresolvable += int64(n)
}

// Unresolvable reports keys skipped for vanished facts across all ticks.
func (t *StagedAuditTelemetry) Unresolvable() int64 {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.unresolvable
}

// Deferrals reports deferred ticks for one closed reason.
func (t *StagedAuditTelemetry) Deferrals(reason StagedAuditDeferralReason) int64 {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.deferrals[reason]
}

// Ticks reports completed ticks.
func (t *StagedAuditTelemetry) Ticks() int64 {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.ticks
}

// LastBatchCount reports the last tick's batch count.
func (t *StagedAuditTelemetry) LastBatchCount() int {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.lastBatchCount
}

// LastMaxBatchDuration reports the last tick's slowest batch.
func (t *StagedAuditTelemetry) LastMaxBatchDuration() time.Duration {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.lastMaxBatch
}

// LastStagedFacts reports the last tick's staged-fact gauge.
func (t *StagedAuditTelemetry) LastStagedFacts() int {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.lastStagedFacts
}

// LastDistinctKeys reports the last tick's distinct-key gauge.
func (t *StagedAuditTelemetry) LastDistinctKeys() int {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.lastDistinctKeys
}

// LastMember reports the last tick's member identity.
func (t *StagedAuditTelemetry) LastMember() string {
	if t == nil {
		return ""
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.lastMember
}

// LastReplayLagStart reports the last tick's replay lag at tick start.
func (t *StagedAuditTelemetry) LastReplayLagStart() time.Duration {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.lastLagStart
}

// LastReplayLagEnd reports the last tick's replay lag at tick end.
func (t *StagedAuditTelemetry) LastReplayLagEnd() time.Duration {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.lastLagEnd
}

// DiscardRate reports writer-verify discards per verify, or 0 with no verify.
func (t *StagedAuditTelemetry) DiscardRate() float64 {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.verifies == 0 {
		return 0
	}
	return float64(t.discards) / float64(t.verifies)
}
