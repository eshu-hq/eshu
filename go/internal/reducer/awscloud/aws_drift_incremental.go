// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

import (
	"context"
	"sort"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/cloudruntime"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Incremental modes for one aws_cloud_runtime_drift run (#10878, first
// domain). skip re-derives nothing, delta narrows the writer to the diff,
// full re-derives and rewrites the whole generation (audit or fallback).
const (
	// AWSDriftIncrementalModeSkip leaves the run's truth untouched: the ARN
	// snapshot matches the last succeeded run and no audit is due.
	AWSDriftIncrementalModeSkip = "skip"
	// AWSDriftIncrementalModeDelta narrows the run to the ARN diff:
	// added/changed ARNs are (re)classified, removed ARNs retract.
	AWSDriftIncrementalModeDelta = "delta"
	// AWSDriftIncrementalModeFull re-derives the whole generation: the
	// daily #10861-D3 audit or a counted fail-open fallback.
	AWSDriftIncrementalModeFull = "full"
)

// Closed reason labels for the incremental plan (#10861-D3 (b) counting).
const (
	// AWSDriftIncrementalReasonInputUnchanged skips: snapshots identical,
	// audit fresh.
	AWSDriftIncrementalReasonInputUnchanged = "input_unchanged"
	// AWSDriftIncrementalReasonChanged narrows: the diff is non-empty.
	AWSDriftIncrementalReasonChanged = "changed"
	// AWSDriftIncrementalReasonAuditDue audits: the daily full run is due.
	AWSDriftIncrementalReasonAuditDue = "audit_due"
	// AWSDriftIncrementalReasonFallbackNoSnapshot fails open: no recorded
	// snapshot exists, so absence re-opens (#10878 decision 4).
	AWSDriftIncrementalReasonFallbackNoSnapshot = "fallback_no_snapshot"
	// AWSDriftIncrementalReasonFallbackForced fails open: the caller forced
	// a full run (probe error, unreadable signal).
	AWSDriftIncrementalReasonFallbackForced = "fallback_forced"
)

// awsDriftAuditInterval is the #10861-D3 daily audit cadence: one full run
// per domain per day, with corrections reported.
const awsDriftAuditInterval = 24 * time.Hour

// AWSDriftIncrementalRun is one handler invocation's change signal: the
// ARN-to-payload-identity snapshots around the run plus the audit clock.
// Snapshots map ARN to the payload identity the evidence loader read for
// it (the AWS fact plus the matched state/config identity), exactly the
// keys contract.DiffARNRecords compares. A nil PrevSnapshot means no
// recorded snapshot exists.
type AWSDriftIncrementalRun struct {
	PrevSnapshot map[string]string
	NextSnapshot map[string]string
	LastAuditAt  time.Time
	Now          time.Time
	ForceFullRun bool
}

// AWSDriftIncrementalPlan is the handler's narrowed work for one run.
type AWSDriftIncrementalPlan struct {
	Mode   string
	Reason string
	Diff   reducercontract.ARNRecordDiff
}

// PlanAWSDriftIncrementalRun narrows one run to its change signal (#10878
// decision 2). Precedence: a forced full run wins (counted fallback), then
// the daily audit, then missing-snapshot reopen, then skip-or-delta on the
// ARN diff. It never reports skip-on-missing: absence re-opens.
func PlanAWSDriftIncrementalRun(run AWSDriftIncrementalRun) AWSDriftIncrementalPlan {
	now := run.Now
	if now.IsZero() {
		now = time.Now()
	}
	if run.ForceFullRun {
		return AWSDriftIncrementalPlan{Mode: AWSDriftIncrementalModeFull, Reason: AWSDriftIncrementalReasonFallbackForced}
	}
	if run.LastAuditAt.IsZero() || now.Sub(run.LastAuditAt) >= awsDriftAuditInterval {
		return AWSDriftIncrementalPlan{Mode: AWSDriftIncrementalModeFull, Reason: AWSDriftIncrementalReasonAuditDue}
	}
	if len(run.PrevSnapshot) == 0 && len(run.NextSnapshot) == 0 {
		return AWSDriftIncrementalPlan{Mode: AWSDriftIncrementalModeSkip, Reason: AWSDriftIncrementalReasonInputUnchanged}
	}
	if len(run.PrevSnapshot) == 0 {
		return AWSDriftIncrementalPlan{Mode: AWSDriftIncrementalModeFull, Reason: AWSDriftIncrementalReasonFallbackNoSnapshot}
	}
	diff := reducercontract.DiffARNRecords(run.PrevSnapshot, run.NextSnapshot)
	if !diff.HasChanges() {
		return AWSDriftIncrementalPlan{Mode: AWSDriftIncrementalModeSkip, Reason: AWSDriftIncrementalReasonInputUnchanged}
	}
	return AWSDriftIncrementalPlan{Mode: AWSDriftIncrementalModeDelta, Reason: AWSDriftIncrementalReasonChanged, Diff: diff}
}

// FilterAddressedRowsToDiff narrows loaded evidence rows to the ARN diff
// for a delta run: added and changed ARNs are (re)classified, removed ARNs
// flow through so the generation-authoritative retire removes their stale
// findings. Unchanged ARNs never reach the delta writer. The second return
// is the sorted removed-ARN list for the retract path. A full run ignores
// this helper and classifies every row.
func FilterAddressedRowsToDiff(
	rows []cloudruntime.AddressedRow,
	diff reducercontract.ARNRecordDiff,
) (kept []cloudruntime.AddressedRow, removed []string) {
	changed := make(map[string]struct{}, len(diff.Added)+len(diff.Changed)+len(diff.Removed))
	for _, arn := range diff.Added {
		changed[arn] = struct{}{}
	}
	for _, arn := range diff.Changed {
		changed[arn] = struct{}{}
	}
	for _, arn := range diff.Removed {
		changed[arn] = struct{}{}
	}
	for _, row := range rows {
		if _, ok := changed[row.ARN]; ok {
			kept = append(kept, row)
		}
	}
	removed = append([]string(nil), diff.Removed...)
	sort.Strings(removed)
	return kept, removed
}

// AWSDriftRecordedSnapshotLoader reads this run's previously recorded
// reopen snapshot: the row recordReopenSnapshot wrote for an earlier claim
// of the same work item, plus when it was recorded (the audit clock).
// ok=false means no row exists: absence re-opens, never skips (#10878
// decision 4). IngestionStore implements it against
// aws_drift_reopen_fingerprints.
type AWSDriftRecordedSnapshotLoader interface {
	LoadAWSDriftRecordedSnapshot(
		ctx context.Context,
		workItemID string,
	) (prev map[string]string, recordedAt time.Time, ok bool, err error)
}

// AWSDriftIncrementalPlanSource builds one run's incremental plan in
// production: the previously recorded snapshot against the fresh load plus
// the audit clock. Handle consults it per run when IncrementalPlan is nil;
// the static field stays as the test override. The returned snapshot is
// the fresh load the plan was built from (empty when the load failed and
// the plan fell back to full): Handle reuses it for the reopen record and
// for shared-load keying instead of loading the same generation twice.
type AWSDriftIncrementalPlanSource interface {
	PlanIncrementalRun(
		ctx context.Context,
		scopeID string,
		generationID string,
		workItemID string,
		now time.Time,
	) (plan AWSDriftIncrementalPlan, fresh reducercontract.AWSDriftReopenSnapshot, err error)
}

// AWSDriftIncrementalPlanner is the production AWSDriftIncrementalPlanSource:
// previous recorded snapshot (this work item's row, the audit clock) versus
// the fresh snapshot load. Any failure fails open to a counted full run,
// never to a skip: a missing recorded row forces fallback_no_snapshot
// (absence re-opens), any load error forces fallback_forced.
type AWSDriftIncrementalPlanner struct {
	Snapshots AWSDriftSnapshotLoader
	Recorded  AWSDriftRecordedSnapshotLoader
}

// PlanIncrementalRun implements AWSDriftIncrementalPlanSource.
func (p *AWSDriftIncrementalPlanner) PlanIncrementalRun(
	ctx context.Context,
	scopeID string,
	generationID string,
	workItemID string,
	now time.Time,
) (AWSDriftIncrementalPlan, reducercontract.AWSDriftReopenSnapshot, error) {
	if p == nil || p.Snapshots == nil || p.Recorded == nil {
		return AWSDriftIncrementalPlan{
			Mode: AWSDriftIncrementalModeFull, Reason: AWSDriftIncrementalReasonFallbackForced,
		}, reducercontract.AWSDriftReopenSnapshot{}, nil
	}
	prev, recordedAt, ok, err := p.Recorded.LoadAWSDriftRecordedSnapshot(ctx, workItemID)
	if err != nil {
		// An unreadable previous snapshot fails open to a counted full
		// run, never to a skip.
		return fullFallbackPlan(now), reducercontract.AWSDriftReopenSnapshot{}, nil
	}
	if !ok {
		// No recorded row: a new work item with no previous input to
		// diff. Absence re-opens, never skips.
		return p.planFirstRun(ctx, scopeID, generationID, now)
	}
	fresh, err := p.Snapshots.LoadAWSDriftARNSnapshot(ctx, scopeID, generationID)
	if err != nil {
		return fullFallbackPlan(now), reducercontract.AWSDriftReopenSnapshot{}, nil
	}
	// The audit clock is the last successful run's record time: a scope
	// quiet longer than the audit interval takes a full audit on its next
	// run. A scope running deltas within the day defers its audit; the
	// follow-up for a last-full-run (rather than last-run) clock is
	// tracked on the lane issue.
	return PlanAWSDriftIncrementalRun(AWSDriftIncrementalRun{
		PrevSnapshot: prev,
		NextSnapshot: snapshotToARNMap(fresh),
		LastAuditAt:  recordedAt,
		Now:          now,
	}), fresh, nil
}

// planFirstRun plans a work item with no recorded snapshot: full, always.
// A non-empty fresh load takes the plan's own missing-snapshot branch; an
// empty one takes a forced full so the run records the empty baseline and
// the next maintenance pass converges instead of reopening forever.
func (p *AWSDriftIncrementalPlanner) planFirstRun(
	ctx context.Context,
	scopeID string,
	generationID string,
	now time.Time,
) (AWSDriftIncrementalPlan, reducercontract.AWSDriftReopenSnapshot, error) {
	fresh, err := p.Snapshots.LoadAWSDriftARNSnapshot(ctx, scopeID, generationID)
	if err != nil {
		return fullFallbackPlan(now), reducercontract.AWSDriftReopenSnapshot{}, nil
	}
	if len(fresh.ARNIDs) == 0 {
		return fullFallbackPlan(now), fresh, nil
	}
	// No prior run, so no audit is overdue: the full run below
	// establishes the audit baseline. LastAuditAt: now keeps the plan's
	// own missing-snapshot branch ahead of its audit branch.
	return PlanAWSDriftIncrementalRun(AWSDriftIncrementalRun{
		NextSnapshot: snapshotToARNMap(fresh),
		LastAuditAt:  now,
		Now:          now,
	}), fresh, nil
}

// fullFallbackPlan is the counted fail-open full run for an unreadable
// signal (probe error, unreadable snapshot): full, never skip.
func fullFallbackPlan(now time.Time) AWSDriftIncrementalPlan {
	return PlanAWSDriftIncrementalRun(AWSDriftIncrementalRun{
		ForceFullRun: true,
		Now:          now,
	})
}

// snapshotToARNMap renders a recorded reopen snapshot's parallel ARN
// arrays back into the ARN-to-payload-identity map the plan diffs. A
// shorter hashes slice (a corrupt row) drops the unhashable ARNs from the
// next set: they diff as removed, retracting rather than skipping.
func snapshotToARNMap(snap reducercontract.AWSDriftReopenSnapshot) map[string]string {
	out := make(map[string]string, len(snap.ARNIDs))
	for i, arn := range snap.ARNIDs {
		if i < len(snap.ARNPayloadHashes) {
			out[arn] = snap.ARNPayloadHashes[i]
		}
	}
	return out
}

// resolveIncrementalPlan returns the plan governing one Handle call plus
// the fresh snapshot it was built from (hasFresh=false when there is none
// to reuse). The static IncrementalPlan wins when set (tests); otherwise
// the IncrementalPlanSource builds the plan per run (production); both nil
// keeps the full-generation behavior. A source error fails open to a
// counted full run, never to a skip and never to a run error.
func (h AWSCloudRuntimeDriftHandler) resolveIncrementalPlan(
	ctx context.Context,
	intent reducercontract.Intent,
	now time.Time,
) (plan *AWSDriftIncrementalPlan, fresh reducercontract.AWSDriftReopenSnapshot, hasFresh bool) {
	if h.IncrementalPlan != nil {
		return h.IncrementalPlan, reducercontract.AWSDriftReopenSnapshot{}, false
	}
	if h.IncrementalPlanSource == nil {
		return nil, reducercontract.AWSDriftReopenSnapshot{}, false
	}
	built, snapshot, err := h.IncrementalPlanSource.PlanIncrementalRun(
		ctx, intent.ScopeID, intent.GenerationID, intent.IntentID, now,
	)
	if err != nil {
		fallback := fullFallbackPlan(now)
		return &fallback, reducercontract.AWSDriftReopenSnapshot{}, false
	}
	if snapshot.Fingerprint == "" {
		return &built, reducercontract.AWSDriftReopenSnapshot{}, false
	}
	return &built, snapshot, true
}

// loadDriftEvidenceShared loads one run's evidence rows, sharing the load
// across concurrent workers on the same input version through the
// EvidenceCache when one is wired and the run has a fresh snapshot
// fingerprint to key it on. Without either it loads directly.
func (h AWSCloudRuntimeDriftHandler) loadDriftEvidenceShared(
	ctx context.Context,
	intent reducercontract.Intent,
	inputVersion string,
) ([]cloudruntime.AddressedRow, error) {
	if h.EvidenceCache == nil || inputVersion == "" {
		return h.EvidenceLoader.LoadAWSCloudRuntimeDriftEvidence(ctx, intent.ScopeID, intent.GenerationID)
	}
	rows, _, err := h.EvidenceCache.LoadShared(
		ctx, intent.ScopeID, intent.GenerationID, inputVersion,
		func(ctx context.Context) ([]cloudruntime.AddressedRow, int64, error) {
			loaded, err := h.EvidenceLoader.LoadAWSCloudRuntimeDriftEvidence(ctx, intent.ScopeID, intent.GenerationID)
			if err != nil {
				return nil, 0, err
			}
			return loaded, EstimateAWSDriftEvidenceBytes(loaded), nil
		},
	)
	return rows, err
}

// estimateARNBytesOverhead is the conservative per-row allowance for Go
// struct, slice, and map headers the measurable byte sum below does not
// cover. The estimate biases OVER the true in-memory size on purpose: an
// overestimate passes a merely large generation through observably (safe),
// while an underestimate would cache a pathological one and evict the
// fleet's peers (the failure the budget exists to prevent).
const estimateARNBytesOverhead = 1024

// EstimateAWSDriftEvidenceBytes estimates one evidence load's in-memory
// bytes for cache budget accounting: every measurable string, map entry,
// and slice element plus a per-row overhead allowance. It is a budgeting
// heuristic, not a billing meter; struct padding and map-bucket internals
// are covered by the overhead, not measured.
func EstimateAWSDriftEvidenceBytes(rows []cloudruntime.AddressedRow) int64 {
	var total int64
	stringSliceBytes := func(ss []string) int64 {
		var n int64
		for _, s := range ss {
			n += int64(len(s))
		}
		return n
	}
	stringMapBytes := func(m map[string]string) int64 {
		var n int64
		for k, v := range m {
			n += int64(len(k) + len(v))
		}
		return n
	}
	resourceBytes := func(r *cloudruntime.ResourceRow) int64 {
		if r == nil {
			return 0
		}
		return int64(len(r.ARN)+len(r.ResourceID)+len(r.ResourceType)+len(r.Address)+len(r.ScopeID)) +
			stringMapBytes(r.Tags) + stringMapBytes(r.Attributes) +
			stringSliceBytes(r.ContainerImages) + estimateARNBytesOverhead
	}
	for _, row := range rows {
		total += int64(len(row.ARN)+len(row.ResourceType)+len(row.ManagementStatus)+len(row.RecommendedAction)) +
			stringSliceBytes(row.MissingEvidence) + stringSliceBytes(row.WarningFlags) +
			resourceBytes(row.Cloud) + resourceBytes(row.State) + resourceBytes(row.Config) +
			estimateARNBytesOverhead
	}
	return total
}

// recordIncrementalSkip counts a skip-plan run (#10878 AC6). Nil
// instruments emit nothing: the skip itself is the behavior, the counter
// is its observability.
func (h AWSCloudRuntimeDriftHandler) recordIncrementalSkip(ctx context.Context, plan *AWSDriftIncrementalPlan) {
	if h.Instruments == nil {
		return
	}
	h.Instruments.AWSDriftIncrementalSkipped.Add(ctx, 1,
		metric.WithAttributes(attribute.String("reason", plan.Reason)))
}

// recordIncrementalClassified counts the keys one classifying run covered
// (#10878 AC6): the diff size on delta runs, the generation size on full
// runs. A full run taken for a fallback reason also counts the D3 (b)
// fallback, so a stuck-unreadable signal surfaces as sustained nonzero.
func (h AWSCloudRuntimeDriftHandler) recordIncrementalClassified(ctx context.Context, plan *AWSDriftIncrementalPlan, keys int) {
	if h.Instruments == nil || plan == nil {
		return
	}
	h.Instruments.AWSDriftIncrementalChangedKeys.Add(ctx, int64(keys),
		metric.WithAttributes(attribute.String("mode", plan.Mode)))
	switch plan.Reason {
	case AWSDriftIncrementalReasonFallbackNoSnapshot, AWSDriftIncrementalReasonFallbackForced:
		h.Instruments.AWSDriftIncrementalFallbackFull.Add(ctx, 1,
			metric.WithAttributes(attribute.String("reason", plan.Reason)))
	}
}

// RecordAWSDriftAuditCorrections counts the admitted candidates a daily
// audit found a delta run missed (full-minus-delta on correlation keys,
// via CountAuditCorrections). The audit caller owns the comparison; this
// is its closed-label emit path (#10878 AC3).
func RecordAWSDriftAuditCorrections(ctx context.Context, instruments *telemetry.Instruments, corrections int) {
	if instruments == nil || corrections <= 0 {
		return
	}
	instruments.AWSDriftIncrementalAuditCorrections.Add(ctx, int64(corrections))
}

// CountAuditCorrections counts AC3 corrections: admitted correlation keys
// the delta run missed. Keys use the candidate correlation key so a
// reclassification of the same ARN to a different finding kind counts too.
// Order-independent; duplicates collapse.
func CountAuditCorrections(deltaAdmitted, fullAdmitted []string) int {
	seen := make(map[string]struct{}, len(deltaAdmitted))
	for _, key := range deltaAdmitted {
		seen[key] = struct{}{}
	}
	missed := 0
	counted := make(map[string]struct{})
	for _, key := range fullAdmitted {
		if _, ok := seen[key]; ok {
			continue
		}
		if _, dup := counted[key]; dup {
			continue
		}
		counted[key] = struct{}{}
		missed++
	}
	return missed
}
