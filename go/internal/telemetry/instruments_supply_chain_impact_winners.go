// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// MetricDimensionTransition labels eshu_dp_supply_chain_impact_stale_copy_repairs_total
// with the retraction-state change of the repaired copy (#10765). The value set
// is closed: the SupplyChainImpactStaleCopyTransition* constants.
const MetricDimensionTransition = "transition"

// Stale-copy repair transition label values. They are the closed set of the
// transition label. "Active" means any suppression state other than
// advisory_retracted.
const (
	// SupplyChainImpactStaleCopyTransitionActiveToRetracted counts a copy that
	// was not retracted and a pass has now rewritten as advisory_retracted.
	SupplyChainImpactStaleCopyTransitionActiveToRetracted = "active_to_retracted"
	// SupplyChainImpactStaleCopyTransitionRetractedToActive counts a copy that
	// was advisory_retracted and a pass has now rewritten as not retracted.
	SupplyChainImpactStaleCopyTransitionRetractedToActive = "retracted_to_active"
)

// AttrTransition returns a transition attribute. v must be one of the
// SupplyChainImpactStaleCopyTransition* constants.
func AttrTransition(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionTransition, v)
}

// Winner-table action label values for eshu_dp_supply_chain_impact_winners_rows_total.
// They are the closed set of the action label.
const (
	// SupplyChainImpactWinnersActionInserted counts winner rows a resweep added.
	SupplyChainImpactWinnersActionInserted = "inserted"
	// SupplyChainImpactWinnersActionUpdated counts winner rows whose content changed.
	SupplyChainImpactWinnersActionUpdated = "updated"
	// SupplyChainImpactWinnersActionDeleted counts winner rows a resweep removed.
	SupplyChainImpactWinnersActionDeleted = "deleted"
	// SupplyChainImpactWinnersActionUnchanged counts current winners a resweep
	// compared and left untouched, so wrote no row version for.
	SupplyChainImpactWinnersActionUnchanged = "unchanged"
)

// supplyChainImpactWinnersResweepBuckets reach 300 s because the resweep is
// O(findings) and already takes tens of seconds at QA scale: an operator must see
// a statement pass the 60 s lease TTL, not only a sub-second to few-second range.
var supplyChainImpactWinnersResweepBuckets = []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 20, 30, 60, 120, 300}

// registerSupplyChainImpactWinnersInstruments registers the signals of the
// supply-chain impact winners resweep (#9774) on inst.
//
//   - eshu_dp_supply_chain_impact_winners_resweep_duration_seconds: wall time of
//     one committed resweep statement. The recompute is O(findings); a statement
//     past the 60 s lease TTL can overlap another instance.
//   - eshu_dp_supply_chain_impact_winners_rows_total{action}: winner rows by
//     inserted, updated, deleted and unchanged. Only inserted, updated and
//     deleted write a row version.
//   - eshu_dp_supply_chain_impact_winners_resweep_skipped_total: resweeps that
//     found another in flight and wrote nothing. Any rate means a resweep
//     outlasted the lease TTL.
//   - eshu_dp_supply_chain_impact_winners_watermark_age_seconds: now minus the
//     last watermark the maintainer saw, computed at scrape time. It keeps
//     climbing through a stall, when no resweep commits and no read succeeds, and
//     it never queries Postgres from a scrape. It has no value until the
//     process first sees a watermark.
//   - eshu_dp_supply_chain_impact_stale_copy_repairs_total{transition}: stored
//     finding copies a pass rewrote across the retraction boundary, by
//     active_to_retracted and retracted_to_active (#10765). It counts the
//     retraction-state flips a pass wrote, the originating scope's own flip
//     included; it does not measure how many stale copies remain.
//
// No label carries a canonical key, scope, repository or CVE.
func registerSupplyChainImpactWinnersInstruments(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.SupplyChainImpactWinnersResweepDuration, err = meter.Float64Histogram(
		"eshu_dp_supply_chain_impact_winners_resweep_duration_seconds",
		metric.WithDescription("Wall time of one committed supply-chain impact winners resweep statement. The recompute is O(findings), and a statement past the 60 s lease TTL can overlap another instance. A failed or skipped resweep records no sample; the resweep failure log carries the error."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(supplyChainImpactWinnersResweepBuckets...),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactWinnersResweepDuration histogram: %w", err)
	}
	if inst.SupplyChainImpactWinnersRows, err = meter.Int64Counter(
		"eshu_dp_supply_chain_impact_winners_rows_total",
		metric.WithDescription("Supply-chain impact winner rows a committed resweep handled, by action (inserted, updated, deleted, unchanged). Only inserted, updated and deleted write a row version; unchanged rows are compared and skipped."),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactWinnersRows counter: %w", err)
	}
	if inst.SupplyChainImpactWinnersResweepSkipped, err = meter.Int64Counter(
		"eshu_dp_supply_chain_impact_winners_resweep_skipped_total",
		metric.WithDescription("Supply-chain impact winners resweeps that found another resweep in flight, so wrote and stamped nothing. A resweep can only overlap another after outlasting the lease TTL, so any rate means statements are running past it."),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactWinnersResweepSkipped counter: %w", err)
	}
	if inst.SupplyChainImpactStaleCopyRepairs, err = meter.Int64Counter(
		"eshu_dp_supply_chain_impact_stale_copy_repairs_total",
		metric.WithDescription("Retraction-state flips a pass wrote against its stored baseline, by transition (active_to_retracted, retracted_to_active), the originating scope's own first-derivation flip included. It does not say how many stale copies remain: a stale active copy can outrank a retracted copy in the default findings view until its scope re-derives, and that remaining count is not measured (#10765)."),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactStaleCopyRepairs counter: %w", err)
	}
	inst.supplyChainImpactWinnersWatermarkUnixNano = new(atomic.Int64)
	if inst.SupplyChainImpactWinnersWatermarkAge, err = meter.Float64ObservableGauge(
		"eshu_dp_supply_chain_impact_winners_watermark_age_seconds",
		metric.WithDescription("Seconds since the supply-chain impact winners read watermark was stamped, computed at scrape time from the last watermark this process saw. It rises between resweeps and drops on each commit, and keeps climbing through a stall. Above the API freshness window the read reports stale."),
		metric.WithUnit("s"),
		metric.WithFloat64Callback(func(_ context.Context, o metric.Float64Observer) error {
			if at := inst.supplyChainImpactWinnersWatermarkUnixNano.Load(); at != 0 {
				o.Observe(time.Since(time.Unix(0, at)).Seconds())
			}
			return nil
		}),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactWinnersWatermarkAge gauge: %w", err)
	}
	return registerSupplyChainImpactWinnersContractInstruments(meter, inst)
}

// RecordSupplyChainImpactWinnersWatermark sets the watermark the age gauge ages
// from. It is safe on a nil receiver and from any goroutine.
func (i *Instruments) RecordSupplyChainImpactWinnersWatermark(at time.Time) {
	if i == nil || i.supplyChainImpactWinnersWatermarkUnixNano == nil {
		return
	}
	i.supplyChainImpactWinnersWatermarkUnixNano.Store(at.UnixNano())
}

// RecordSupplyChainImpactStaleCopyRepairs counts retraction-state flips under
// transition, which must be one of the SupplyChainImpactStaleCopyTransition*
// constants. It is safe on a nil receiver and adds nothing for a count below
// one.
func (i *Instruments) RecordSupplyChainImpactStaleCopyRepairs(ctx context.Context, transition string, repairs int64) {
	if i == nil || i.SupplyChainImpactStaleCopyRepairs == nil || repairs <= 0 {
		return
	}
	i.SupplyChainImpactStaleCopyRepairs.Add(ctx, repairs, metric.WithAttributes(AttrTransition(transition)))
}

// registerSupplyChainImpactAttachmentWait registers the #11105 counter on
// inst, labeled by domain and the closed outcome set deferred_in_flight,
// producer_failed, bound_exceeded.
func registerSupplyChainImpactAttachmentWait(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.SupplyChainImpactAttachmentWait, err = meter.Int64Counter(
		"eshu_dp_supply_chain_impact_attachment_wait_total",
		metric.WithDescription("Supply chain impact sibling-wait decisions on the same scope generation's "+
			"sbom_attestation_attachment work, by reducer domain and outcome "+
			"(deferred_in_flight/producer_failed/bound_exceeded): whether an impact pass deferred without "+
			"spending an attempt because its own attachment work was in flight, or ran without an attachment "+
			"that dead-lettered or failed, or that showed no sign of life past the bound (#11105)"),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactAttachmentWait counter: %w", err)
	}
	return nil
}

// Full-resweep start reasons for
// eshu_dp_supply_chain_impact_winners_full_resweeps_total (#11117). The value
// set is closed.
const (
	// SupplyChainImpactWinnersFullReasonNoWatermark is a full that ran because
	// no resweep had ever committed (no watermark row).
	SupplyChainImpactWinnersFullReasonNoWatermark = "no_watermark"
	// SupplyChainImpactWinnersFullReasonContractVersion is a full that ran
	// because the stored contract version was NULL or below the binary's.
	SupplyChainImpactWinnersFullReasonContractVersion = "contract_version"
	// SupplyChainImpactWinnersFullReasonKeyedCap is a keyed tick whose
	// consumed keys passed the cap, so it ran the full rebuild instead.
	SupplyChainImpactWinnersFullReasonKeyedCap = "keyed_cap"
)

// Contract-version kinds for eshu_dp_supply_chain_impact_winners_contract_version
// (#11117), the closed set of its kind label.
const (
	// SupplyChainImpactWinnersContractStored is the version the last
	// committed full recorded; 0 when none is recorded.
	SupplyChainImpactWinnersContractStored = "stored"
	// SupplyChainImpactWinnersContractBinary is this binary's version.
	SupplyChainImpactWinnersContractBinary = "binary"
)

// winnersContractPaths is the closed path set the in-flight and last-commit
// gauges report.
var winnersContractPaths = []string{
	SupplyChainImpactWinnersPathFull,
	SupplyChainImpactWinnersPathKeyed,
	SupplyChainImpactWinnersPathAudit,
}

// winnersContractState is the process-local state the #11117 gauges read at
// scrape time: the start of the resweep in flight and the last commit, by
// path, in Unix nanoseconds (0 for none), and the last contract read.
type winnersContractState struct {
	inFlight   map[string]*atomic.Int64
	lastCommit map[string]*atomic.Int64
	read       atomic.Bool
	stored     atomic.Int64
	binary     atomic.Int64
}

func newWinnersContractState() *winnersContractState {
	st := &winnersContractState{inFlight: map[string]*atomic.Int64{}, lastCommit: map[string]*atomic.Int64{}}
	for _, path := range winnersContractPaths {
		st.inFlight[path] = new(atomic.Int64)
		st.lastCommit[path] = new(atomic.Int64)
	}
	return st
}

// registerSupplyChainImpactWinnersContractInstruments registers the winners
// contract signals of #11117 on inst.
//
//   - eshu_dp_supply_chain_impact_winners_resweep_in_flight_age_seconds{path}:
//     seconds the resweep in flight on this process has run, 0 when none. A
//     full past its usual duration, or a keyed tick past seconds, is visible
//     before it commits or fails.
//   - eshu_dp_supply_chain_impact_winners_last_commit_timestamp_seconds{path}:
//     Unix time of the last committed resweep by path, distinct from the
//     watermark value it stamped. No value until the path first commits.
//   - eshu_dp_supply_chain_impact_winners_audit_last_success_age_seconds: age
//     of this process's last completed audit pass, zero candidates included.
//   - eshu_dp_supply_chain_impact_winners_contract_version{kind}: the stored
//     contract version (0 when none is recorded) and this binary's, from the
//     last contract read. No value until the first read.
//   - eshu_dp_supply_chain_impact_winners_full_resweeps_total{reason}:
//     committed fulls by start reason (no_watermark, contract_version,
//     keyed_cap). A matching-version roll adds nothing.
//   - eshu_dp_supply_chain_impact_winners_stand_downs_total: ticks this binary
//     stood down because a newer contract is stored.
//
// No label carries a canonical key, scope, repository or CVE.
func registerSupplyChainImpactWinnersContractInstruments(meter metric.Meter, inst *Instruments) error {
	st := newWinnersContractState()
	inst.supplyChainImpactWinnersContract = st
	var err error
	if inst.SupplyChainImpactWinnersInFlightAge, err = meter.Float64ObservableGauge(
		"eshu_dp_supply_chain_impact_winners_resweep_in_flight_age_seconds",
		metric.WithDescription("Seconds the supply-chain impact winners resweep in flight on this process has run, by path (full, keyed, audit); 0 when none is in flight. It shows a long full or a stuck keyed tick before it commits or fails."),
		metric.WithUnit("s"),
		metric.WithFloat64Callback(func(_ context.Context, o metric.Float64Observer) error {
			for _, path := range winnersContractPaths {
				age := 0.0
				if at := st.inFlight[path].Load(); at != 0 {
					age = time.Since(time.Unix(0, at)).Seconds()
				}
				o.Observe(age, metric.WithAttributes(AttrPath(path)))
			}
			return nil
		}),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactWinnersInFlightAge gauge: %w", err)
	}
	if inst.SupplyChainImpactWinnersLastCommit, err = meter.Float64ObservableGauge(
		"eshu_dp_supply_chain_impact_winners_last_commit_timestamp_seconds",
		metric.WithDescription("Unix time of the last supply-chain impact winners resweep this process committed, by path (full, keyed, audit). It is the commit time, not the watermark value the resweep stamped. No value until the path first commits."),
		metric.WithUnit("s"),
		metric.WithFloat64Callback(func(_ context.Context, o metric.Float64Observer) error {
			for _, path := range winnersContractPaths {
				if at := st.lastCommit[path].Load(); at != 0 {
					o.Observe(float64(at)/float64(time.Second), metric.WithAttributes(AttrPath(path)))
				}
			}
			return nil
		}),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactWinnersLastCommit gauge: %w", err)
	}
	if inst.SupplyChainImpactWinnersAuditLastSuccessAge, err = meter.Float64ObservableGauge(
		"eshu_dp_supply_chain_impact_winners_audit_last_success_age_seconds",
		metric.WithDescription("Seconds since this process last completed a supply-chain impact winners audit pass (repairs found or not), computed at scrape time. No value until the first completed pass; a value that keeps climbing past a few audit intervals means the backstop is not running."),
		metric.WithUnit("s"),
		metric.WithFloat64Callback(func(_ context.Context, o metric.Float64Observer) error {
			if at := st.lastCommit[SupplyChainImpactWinnersPathAudit].Load(); at != 0 {
				o.Observe(time.Since(time.Unix(0, at)).Seconds())
			}
			return nil
		}),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactWinnersAuditLastSuccessAge gauge: %w", err)
	}
	if inst.SupplyChainImpactWinnersContractVersion, err = meter.Int64ObservableGauge(
		"eshu_dp_supply_chain_impact_winners_contract_version",
		metric.WithDescription("Supply-chain impact winners contract version by kind: stored (what the last committed full recorded, 0 when none) and binary (this process). Stored below binary runs one full; equal runs keyed ticks; stored above binary stands this process down."),
		metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
			if st.read.Load() {
				o.Observe(st.stored.Load(), metric.WithAttributes(AttrKind(SupplyChainImpactWinnersContractStored)))
				o.Observe(st.binary.Load(), metric.WithAttributes(AttrKind(SupplyChainImpactWinnersContractBinary)))
			}
			return nil
		}),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactWinnersContractVersion gauge: %w", err)
	}
	if inst.SupplyChainImpactWinnersFullResweeps, err = meter.Int64Counter(
		"eshu_dp_supply_chain_impact_winners_full_resweeps_total",
		metric.WithDescription("Committed supply-chain impact winners full resweeps, by start reason (no_watermark, contract_version, keyed_cap). A reducer roll with an unchanged contract adds nothing."),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactWinnersFullResweeps counter: %w", err)
	}
	if inst.SupplyChainImpactWinnersStandDowns, err = meter.Int64Counter(
		"eshu_dp_supply_chain_impact_winners_stand_downs_total",
		metric.WithDescription("Supply-chain impact winners maintainer ticks that stood down because the stored contract version is above this binary's, at the decision read or under the resweep lock on a keyed tick: no full, keyed tick or audit ran. Any rate means an older binary is running beside a newer one."),
	); err != nil {
		return fmt.Errorf("register SupplyChainImpactWinnersStandDowns counter: %w", err)
	}
	return nil
}

// BeginSupplyChainImpactWinnersResweep marks a resweep on path in flight for
// the in-flight age gauge and returns the call that clears it. It is safe on a
// nil receiver; path must be one of the SupplyChainImpactWinnersPath*
// constants.
func (i *Instruments) BeginSupplyChainImpactWinnersResweep(path string) (end func()) {
	if i == nil || i.supplyChainImpactWinnersContract == nil {
		return func() {}
	}
	slot, ok := i.supplyChainImpactWinnersContract.inFlight[path]
	if !ok {
		return func() {}
	}
	slot.Store(time.Now().UnixNano())
	return func() { slot.Store(0) }
}

// RecordSupplyChainImpactWinnersCommit records at as the last commit time of
// path. It is safe on a nil receiver.
func (i *Instruments) RecordSupplyChainImpactWinnersCommit(path string, at time.Time) {
	if i == nil || i.supplyChainImpactWinnersContract == nil {
		return
	}
	if slot, ok := i.supplyChainImpactWinnersContract.lastCommit[path]; ok {
		slot.Store(at.UnixNano())
	}
}

// RecordSupplyChainImpactWinnersContract records the last contract read:
// stored is 0 when no version is recorded. It is safe on a nil receiver.
func (i *Instruments) RecordSupplyChainImpactWinnersContract(stored, binary int64) {
	if i == nil || i.supplyChainImpactWinnersContract == nil {
		return
	}
	i.supplyChainImpactWinnersContract.stored.Store(stored)
	i.supplyChainImpactWinnersContract.binary.Store(binary)
	i.supplyChainImpactWinnersContract.read.Store(true)
}

// RecordSupplyChainImpactWinnersFullResweep counts one committed full under
// reason, one of the SupplyChainImpactWinnersFullReason* constants. It is safe
// on a nil receiver.
func (i *Instruments) RecordSupplyChainImpactWinnersFullResweep(ctx context.Context, reason string) {
	if i == nil || i.SupplyChainImpactWinnersFullResweeps == nil {
		return
	}
	i.SupplyChainImpactWinnersFullResweeps.Add(ctx, 1, metric.WithAttributes(AttrReason(reason)))
}

// RecordSupplyChainImpactWinnersStandDown counts one stand-down tick. It is
// safe on a nil receiver.
func (i *Instruments) RecordSupplyChainImpactWinnersStandDown(ctx context.Context) {
	if i == nil || i.SupplyChainImpactWinnersStandDowns == nil {
		return
	}
	i.SupplyChainImpactWinnersStandDowns.Add(ctx, 1)
}

// SupplyChainImpactWinnersStagedAuditSnapshot is one read of the staged
// audit's in-memory telemetry (#11117): cumulative deferrals by closed
// reason and completed ticks, and the last completed tick's gauges.
type SupplyChainImpactWinnersStagedAuditSnapshot struct {
	Deferrals        map[string]int64
	Ticks            int64
	LastBatches      int64
	LastMaxBatch     time.Duration
	LastStagedFacts  int64
	LastDistinctKeys int64

	// The range sweep (#11131): cumulative range counters since process
	// start, the last committed range, and the persisted sweep row as last
	// seen (SweepSeen false before the first read).
	Ranges, RangeRetries                                    int64
	LockedRanges                                            int64
	RangeFacts, RangeCandidates, RangePending, RangeRepairs int64
	RangeDiscards                                           int64
	LastRange                                               time.Duration
	LastRangeFacts, LastRangeBudget                         int64
	SweepSeen                                               bool
	SweepID, SweepRangesDone                                int64
	LastSweepCompletedAt                                    time.Time
	LastSweepSeconds                                        float64
	LastSweepRanges, LastSweepFacts, LastSweepCandidates    int64
	LastSweepRepairs, LastSweepDiscards                     int64
}

// SupplyChainImpactWinnersStagedAuditSource is the staged audit recorder the
// exported instruments read at scrape time.
type SupplyChainImpactWinnersStagedAuditSource interface {
	StagedAuditSnapshot() SupplyChainImpactWinnersStagedAuditSnapshot
}

// supplyChainImpactWinnersStagedAuditReasons is the closed deferral reason
// set of the staged audit (internal/runtime/postgres StagedAuditDefer*).
var supplyChainImpactWinnersStagedAuditReasons = []string{
	"budget", "conflict_exhausted", "batch_abort",
	"timeout", "null_key", "pipe", "lock_busy", "cursor_race", "keyed_yield",
}

// RegisterSupplyChainImpactWinnersStagedAuditInstruments registers the staged
// audit's exported instruments over src (#11117 arbiter addendum item 5b),
// read at scrape time:
//
//   - eshu_dp_supply_chain_impact_winners_staged_audit_deferrals_total{reason}:
//     deferred staged-audit ticks by closed reason (budget,
//     conflict_exhausted, batch_abort). A deferred tick never reached the
//     writer and verified nothing.
//   - eshu_dp_supply_chain_impact_winners_staged_audit_ticks_total: completed
//     staged-audit read phases.
//   - eshu_dp_supply_chain_impact_winners_staged_audit_last_batches,
//     ..._last_max_batch_seconds, ..._last_staged_facts and
//     ..._last_distinct_keys: the last completed tick's batch count, slowest
//     batch, staged facts and distinct keys. No value before the first tick.
//
// Call it once per recorder.
func RegisterSupplyChainImpactWinnersStagedAuditInstruments(meter metric.Meter, src SupplyChainImpactWinnersStagedAuditSource) error {
	if meter == nil || src == nil {
		return nil
	}
	deferrals, err := meter.Int64ObservableCounter(
		"eshu_dp_supply_chain_impact_winners_staged_audit_deferrals_total",
		metric.WithDescription("Deferred supply-chain impact winners staged-audit ranges by reason (budget, conflict_exhausted, batch_abort, timeout, null_key, pipe, lock_busy, cursor_race). A deferred range committed nothing; a steady rate means the sweep is not advancing."),
	)
	if err != nil {
		return fmt.Errorf("register staged audit deferrals counter: %w", err)
	}
	ticks, err := meter.Int64ObservableCounter(
		"eshu_dp_supply_chain_impact_winners_staged_audit_ticks_total",
		metric.WithDescription("Completed supply-chain impact winners staged-audit read phases."),
	)
	if err != nil {
		return fmt.Errorf("register staged audit ticks counter: %w", err)
	}
	batches, err := meter.Int64ObservableGauge(
		"eshu_dp_supply_chain_impact_winners_staged_audit_last_batches",
		metric.WithDescription("Standby batches the last completed supply-chain impact winners staged-audit tick read (one per active scope generation)."),
	)
	if err != nil {
		return fmt.Errorf("register staged audit last batches gauge: %w", err)
	}
	maxBatch, err := meter.Float64ObservableGauge(
		"eshu_dp_supply_chain_impact_winners_staged_audit_last_max_batch_seconds",
		metric.WithDescription("Slowest standby batch of the last completed supply-chain impact winners staged-audit tick; each batch has a 25 s statement timeout."),
		metric.WithUnit("s"),
	)
	if err != nil {
		return fmt.Errorf("register staged audit last max batch gauge: %w", err)
	}
	stagedFacts, err := meter.Int64ObservableGauge(
		"eshu_dp_supply_chain_impact_winners_staged_audit_last_staged_facts",
		metric.WithDescription("Facts the last completed supply-chain impact winners staged-audit tick staged client-side (cap 1,000,000)."),
	)
	if err != nil {
		return fmt.Errorf("register staged audit last staged facts gauge: %w", err)
	}
	distinctKeys, err := meter.Int64ObservableGauge(
		"eshu_dp_supply_chain_impact_winners_staged_audit_last_distinct_keys",
		metric.WithDescription("Distinct canonical keys the last completed supply-chain impact winners staged-audit tick compacted (cap 250,000)."),
	)
	if err != nil {
		return fmt.Errorf("register staged audit last distinct keys gauge: %w", err)
	}
	_, err = meter.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		snap := src.StagedAuditSnapshot()
		for _, reason := range supplyChainImpactWinnersStagedAuditReasons {
			o.ObserveInt64(deferrals, snap.Deferrals[reason], metric.WithAttributes(AttrReason(reason)))
		}
		o.ObserveInt64(ticks, snap.Ticks)
		if snap.Ticks > 0 {
			o.ObserveInt64(batches, snap.LastBatches)
			o.ObserveFloat64(maxBatch, snap.LastMaxBatch.Seconds())
			o.ObserveInt64(stagedFacts, snap.LastStagedFacts)
			o.ObserveInt64(distinctKeys, snap.LastDistinctKeys)
		}
		return nil
	}, deferrals, ticks, batches, maxBatch, stagedFacts, distinctKeys)
	if err != nil {
		return fmt.Errorf("register staged audit callback: %w", err)
	}
	return registerSupplyChainImpactWinnersAuditSweepInstruments(meter, src)
}
