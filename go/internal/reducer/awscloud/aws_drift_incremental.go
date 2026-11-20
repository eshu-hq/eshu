// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

import (
	"sort"
	"time"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/cloudruntime"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
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
