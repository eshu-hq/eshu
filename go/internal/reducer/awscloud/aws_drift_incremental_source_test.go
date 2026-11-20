// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/cloudruntime"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// stubAWSDriftIncrementalPlanSource returns a fixed plan plus the fresh
// snapshot the production planner would have loaded, so tests pin handler
// behavior under a per-run wired plan without a database.
type stubAWSDriftIncrementalPlanSource struct {
	plan     AWSDriftIncrementalPlan
	snapshot reducercontract.AWSDriftReopenSnapshot
	err      error
	calls    int
}

func (s *stubAWSDriftIncrementalPlanSource) PlanIncrementalRun(
	_ context.Context,
	_, _, _ string,
	_ time.Time,
) (AWSDriftIncrementalPlan, reducercontract.AWSDriftReopenSnapshot, error) {
	s.calls++
	if s.err != nil {
		return AWSDriftIncrementalPlan{}, reducercontract.AWSDriftReopenSnapshot{}, s.err
	}
	return s.plan, s.snapshot, nil
}

// stubAWSDriftRecordedSnapshotLoader returns the previously recorded reopen
// snapshot for one work item: the "previous" half of the per-run plan.
type stubAWSDriftRecordedSnapshotLoader struct {
	prev       map[string]string
	recordedAt time.Time
	ok         bool
	err        error
}

func (s *stubAWSDriftRecordedSnapshotLoader) LoadAWSDriftRecordedSnapshot(
	_ context.Context,
	_ string,
) (map[string]string, time.Time, bool, error) {
	if s.err != nil {
		return nil, time.Time{}, false, s.err
	}
	return s.prev, s.recordedAt, s.ok, nil
}

// TestAWSCloudRuntimeDriftHandlerSkipsThroughWiredPlanSource pins the
// production path: a source-built skip plan succeeds without touching the
// loader or writer, exactly like the static-field skip.
func TestAWSCloudRuntimeDriftHandlerSkipsThroughWiredPlanSource(t *testing.T) {
	t.Parallel()
	loader := &stubAWSCloudRuntimeDriftEvidenceLoader{}
	writer := &stubAWSCloudRuntimeDriftFindingWriter{}
	plan := PlanAWSDriftIncrementalRun(AWSDriftIncrementalRun{
		PrevSnapshot: map[string]string{"arn:1": "p1"},
		NextSnapshot: map[string]string{"arn:1": "p1"},
		LastAuditAt:  time.Now().Add(-time.Hour),
		Now:          time.Now(),
	})
	handler := AWSCloudRuntimeDriftHandler{
		EvidenceLoader:        loader,
		Writer:                writer,
		FencingTokenIssuer:    &stubAWSCloudRuntimeDriftFencingTokenIssuer{tokens: []int64{7}},
		IncrementalPlanSource: &stubAWSDriftIncrementalPlanSource{plan: plan},
	}
	result, err := handler.Handle(t.Context(), reducercontract.Intent{
		IntentID: "intent-src-skip", ScopeID: "aws:scope", GenerationID: "gen-1",
		SourceSystem: "aws", Domain: reducercontract.DomainAWSCloudRuntimeDrift, Cause: "test",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if result.Status != reducercontract.ResultStatusSucceeded {
		t.Fatalf("Handle().Status = %q, want succeeded", result.Status)
	}
	if loader.calls != 0 {
		t.Fatalf("loader calls = %d, want 0 on source-built skip", loader.calls)
	}
	if writer.calls != 0 {
		t.Fatalf("writer calls = %d, want 0 on source-built skip", writer.calls)
	}
}

// TestAWSCloudRuntimeDriftHandlerNarrowsThroughWiredPlanSource pins handler
// narrowing through the wired source: a source-built delta plan bounds the
// retire set to the diff's keys.
func TestAWSCloudRuntimeDriftHandlerNarrowsThroughWiredPlanSource(t *testing.T) {
	t.Parallel()
	loader := &stubAWSCloudRuntimeDriftEvidenceLoader{
		rows: []cloudruntime.AddressedRow{
			{ARN: "arn:keep", ResourceType: "aws_lambda_function", Cloud: &cloudruntime.ResourceRow{
				ARN: "arn:keep", ResourceID: "keep", ResourceType: "aws_lambda_function",
			}},
			{ARN: "arn:chg", ResourceType: "aws_lambda_function", Cloud: &cloudruntime.ResourceRow{
				ARN: "arn:chg", ResourceID: "chg", ResourceType: "aws_lambda_function",
			}},
		},
	}
	writer := &stubAWSCloudRuntimeDriftFindingWriter{}
	plan := PlanAWSDriftIncrementalRun(AWSDriftIncrementalRun{
		PrevSnapshot: map[string]string{"arn:keep": "p1", "arn:chg": "p-old"},
		NextSnapshot: map[string]string{"arn:keep": "p1", "arn:chg": "p-new"},
		LastAuditAt:  time.Now().Add(-time.Hour),
		Now:          time.Now(),
	})
	handler := AWSCloudRuntimeDriftHandler{
		EvidenceLoader:        loader,
		Writer:                writer,
		FencingTokenIssuer:    &stubAWSCloudRuntimeDriftFencingTokenIssuer{tokens: []int64{7}},
		IncrementalPlanSource: &stubAWSDriftIncrementalPlanSource{plan: plan},
	}
	result, err := handler.Handle(t.Context(), reducercontract.Intent{
		IntentID: "intent-src-delta", ScopeID: "aws:scope", GenerationID: "gen-1",
		SourceSystem: "aws", Domain: reducercontract.DomainAWSCloudRuntimeDrift, Cause: "test",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if result.Status != reducercontract.ResultStatusSucceeded {
		t.Fatalf("Handle().Status = %q, want succeeded", result.Status)
	}
	if len(writer.write.EvaluatedARNs) != 1 || writer.write.EvaluatedARNs[0] != "arn:chg" {
		t.Fatalf("EvaluatedARNs = %v, want [arn:chg]", writer.write.EvaluatedARNs)
	}
}

// TestAWSCloudRuntimeDriftHandlerSourceErrorFailsOpen pins fail-open
// production behavior: a planner error never fails the run, it takes a
// counted full run instead.
func TestAWSCloudRuntimeDriftHandlerSourceErrorFailsOpen(t *testing.T) {
	t.Parallel()
	loader := &stubAWSCloudRuntimeDriftEvidenceLoader{}
	writer := &stubAWSCloudRuntimeDriftFindingWriter{}
	source := &stubAWSDriftIncrementalPlanSource{err: errors.New("snapshot probe failed")}
	handler := AWSCloudRuntimeDriftHandler{
		EvidenceLoader:        loader,
		Writer:                writer,
		FencingTokenIssuer:    &stubAWSCloudRuntimeDriftFencingTokenIssuer{tokens: []int64{7}},
		IncrementalPlanSource: source,
	}
	result, err := handler.Handle(t.Context(), reducercontract.Intent{
		IntentID: "intent-src-err", ScopeID: "aws:scope", GenerationID: "gen-1",
		SourceSystem: "aws", Domain: reducercontract.DomainAWSCloudRuntimeDrift, Cause: "test",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil (fail open)", err)
	}
	if result.Status != reducercontract.ResultStatusSucceeded {
		t.Fatalf("Handle().Status = %q, want succeeded", result.Status)
	}
	if loader.calls != 1 {
		t.Fatalf("loader calls = %d, want 1 on fail-open full run", loader.calls)
	}
	if writer.calls != 1 {
		t.Fatalf("writer calls = %d, want 1 on fail-open full run", writer.calls)
	}
}

func plannerTestSnapshots(
	fresh reducercontract.AWSDriftReopenSnapshot,
	prev map[string]string,
	recordedAt time.Time,
	ok bool,
) (*stubAWSDriftSnapshotLoader, *stubAWSDriftRecordedSnapshotLoader) {
	return &stubAWSDriftSnapshotLoader{
			snapshot: fresh,
		}, &stubAWSDriftRecordedSnapshotLoader{
			prev:       prev,
			recordedAt: recordedAt,
			ok:         ok,
		}
}

// TestAWSDriftIncrementalPlannerDiffsPrevAgainstFresh pins the per-run
// plan construction: the previously recorded snapshot against the fresh
// load plus the audit clock.
func TestAWSDriftIncrementalPlannerDiffsPrevAgainstFresh(t *testing.T) {
	t.Parallel()
	now := time.Now()
	snapshots, recorded := plannerTestSnapshots(
		snapshotWithARNs(map[string]string{"arn:keep": "p1", "arn:chg": "p-new"}),
		map[string]string{"arn:keep": "p1", "arn:chg": "p-old"},
		now.Add(-time.Hour), true,
	)
	planner := &AWSDriftIncrementalPlanner{Snapshots: snapshots, Recorded: recorded}
	plan, fresh, err := planner.PlanIncrementalRun(t.Context(), "aws:scope", "gen-2", "work-1", now)
	if err != nil {
		t.Fatalf("PlanIncrementalRun() error = %v, want nil", err)
	}
	if plan.Mode != AWSDriftIncrementalModeDelta {
		t.Fatalf("Mode = %q, want delta", plan.Mode)
	}
	if len(plan.Diff.Changed) != 1 || plan.Diff.Changed[0] != "arn:chg" {
		t.Fatalf("Diff.Changed = %v, want [arn:chg]", plan.Diff.Changed)
	}
	if fresh.Fingerprint == "" {
		t.Fatal("fresh snapshot fingerprint is empty, want the load's fingerprint for shared-load keying")
	}
}

// TestAWSDriftIncrementalPlannerMissingPrevReopensFull pins
// never-skip-on-missing through the planner: no recorded row forces a
// full run with the fallback reason, never the audit reason.
func TestAWSDriftIncrementalPlannerMissingPrevReopensFull(t *testing.T) {
	t.Parallel()
	now := time.Now()
	snapshots, recorded := plannerTestSnapshots(
		snapshotWithARNs(map[string]string{"arn:1": "p1"}), nil, time.Time{}, false,
	)
	planner := &AWSDriftIncrementalPlanner{Snapshots: snapshots, Recorded: recorded}
	plan, _, err := planner.PlanIncrementalRun(t.Context(), "aws:scope", "gen-9", "work-new", now)
	if err != nil {
		t.Fatalf("PlanIncrementalRun() error = %v, want nil", err)
	}
	if plan.Mode != AWSDriftIncrementalModeFull {
		t.Fatalf("Mode = %q, want full", plan.Mode)
	}
	if plan.Reason != AWSDriftIncrementalReasonFallbackNoSnapshot {
		t.Fatalf("Reason = %q, want %q", plan.Reason, AWSDriftIncrementalReasonFallbackNoSnapshot)
	}
}

// TestAWSDriftIncrementalPlannerStaleAuditRunsFull pins the daily audit
// through the planner: a recorded run older than the audit interval forces
// a full run even when the input did not change.
func TestAWSDriftIncrementalPlannerStaleAuditRunsFull(t *testing.T) {
	t.Parallel()
	now := time.Now()
	snap := map[string]string{"arn:1": "p1"}
	snapshots, recorded := plannerTestSnapshots(
		snapshotWithARNs(snap), snap, now.Add(-25*time.Hour), true,
	)
	planner := &AWSDriftIncrementalPlanner{Snapshots: snapshots, Recorded: recorded}
	plan, _, err := planner.PlanIncrementalRun(t.Context(), "aws:scope", "gen-1", "work-1", now)
	if err != nil {
		t.Fatalf("PlanIncrementalRun() error = %v, want nil", err)
	}
	if plan.Mode != AWSDriftIncrementalModeFull || plan.Reason != AWSDriftIncrementalReasonAuditDue {
		t.Fatalf("plan = %+v, want full/audit_due", plan)
	}
}

// TestAWSDriftIncrementalPlannerFreshLoadErrorFailsOpen pins fail-open at
// the planner seam: an unreadable fresh load plans a counted full run and
// reports no fresh snapshot, so the run reloads directly.
func TestAWSDriftIncrementalPlannerFreshLoadErrorFailsOpen(t *testing.T) {
	t.Parallel()
	now := time.Now()
	snapshots := &stubAWSDriftSnapshotLoader{err: errors.New("snapshot query failed")}
	recorded := &stubAWSDriftRecordedSnapshotLoader{
		prev:       map[string]string{"arn:1": "p1"},
		recordedAt: now.Add(-time.Hour),
		ok:         true,
	}
	planner := &AWSDriftIncrementalPlanner{Snapshots: snapshots, Recorded: recorded}
	plan, fresh, err := planner.PlanIncrementalRun(t.Context(), "aws:scope", "gen-1", "work-1", now)
	if err != nil {
		t.Fatalf("PlanIncrementalRun() error = %v, want nil (fail open)", err)
	}
	if plan.Mode != AWSDriftIncrementalModeFull || plan.Reason != AWSDriftIncrementalReasonFallbackForced {
		t.Fatalf("plan = %+v, want full/fallback_forced", plan)
	}
	if fresh.Fingerprint != "" {
		t.Fatalf("fresh fingerprint = %q, want empty on failed load", fresh.Fingerprint)
	}
}

// TestAWSCloudRuntimeDriftHandlerRecordsWiredFreshSnapshot pins the
// record-reuse path the delta claims: with a wired plan source, a
// succeeding run records the fresh snapshot the plan was built from
// instead of reloading the same generation, so the snapshot loader stays
// quiet (#10878 review).
func TestAWSCloudRuntimeDriftHandlerRecordsWiredFreshSnapshot(t *testing.T) {
	t.Parallel()
	claimedAt := time.Now().Add(-time.Minute).UTC()
	snapshotLoader := &stubAWSDriftSnapshotLoader{
		snapshot: snapshotWithARNs(map[string]string{"arn:1": "p1"}),
	}
	recorder := &stubAWSDriftReopenRecorder{}
	handler := AWSCloudRuntimeDriftHandler{
		EvidenceLoader:            &stubAWSCloudRuntimeDriftEvidenceLoader{},
		Writer:                    &stubAWSCloudRuntimeDriftFindingWriter{},
		FencingTokenIssuer:        &stubAWSCloudRuntimeDriftFencingTokenIssuer{tokens: []int64{11}},
		SnapshotLoader:            snapshotLoader,
		ReopenFingerprintRecorder: recorder,
		IncrementalPlanSource: &stubAWSDriftIncrementalPlanSource{
			plan: AWSDriftIncrementalPlan{Mode: AWSDriftIncrementalModeDelta},
			snapshot: func() reducercontract.AWSDriftReopenSnapshot {
				snap := snapshotWithARNs(map[string]string{"arn:1": "p1"})
				snap.Fingerprint = "fp-X"
				return snap
			}(),
		},
	}
	intent := reducercontract.Intent{
		IntentID: "intent-aws-wired-record", ScopeID: "aws:scope", GenerationID: "gen-9",
		SourceSystem: "aws", Domain: reducercontract.DomainAWSCloudRuntimeDrift, Cause: "test",
	}.WithStatus(reducercontract.IntentStatusClaimed, claimedAt)
	if _, err := handler.Handle(t.Context(), intent); err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if snapshotLoader.calls != 0 {
		t.Fatalf("snapshot calls = %d, want 0 (fresh snapshot reused)", snapshotLoader.calls)
	}
	if recorder.calls != 1 {
		t.Fatalf("record calls = %d, want 1", recorder.calls)
	}
	if recorder.snapshot.Fingerprint != "fp-X" {
		t.Fatalf("recorded fingerprint = %q, want fp-X", recorder.snapshot.Fingerprint)
	}
}

func snapshotWithARNs(ids map[string]string) reducercontract.AWSDriftReopenSnapshot {
	snap := reducercontract.AWSDriftReopenSnapshot{Fingerprint: "fp-test"}
	for arn, hash := range ids {
		snap.ARNIDs = append(snap.ARNIDs, arn)
		snap.ARNPayloadHashes = append(snap.ARNPayloadHashes, hash)
	}
	return snap
}
