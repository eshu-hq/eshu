// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

import (
	"context"
	"strings"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/cloudruntime"
)

// TestPlanAWSDriftIncrementalRunSkipsUnchangedInput pins the first-domain
// win: identical snapshots plan a skip, never a re-derivation (#10878).
func TestPlanAWSDriftIncrementalRunSkipsUnchangedInput(t *testing.T) {
	snap := map[string]string{"arn:1": "payload-a", "arn:2": "payload-b"}
	plan := PlanAWSDriftIncrementalRun(AWSDriftIncrementalRun{
		PrevSnapshot: snap,
		NextSnapshot: map[string]string{"arn:1": "payload-a", "arn:2": "payload-b"},
		LastAuditAt:  time.Now().Add(-time.Hour),
		Now:          time.Now(),
	})
	if plan.Mode != AWSDriftIncrementalModeSkip {
		t.Fatalf("Mode = %q, want %q (reason %q)", plan.Mode, AWSDriftIncrementalModeSkip, plan.Reason)
	}
	if plan.Diff.HasChanges() {
		t.Fatalf("skip plan carries changes: %+v", plan.Diff)
	}
}

// TestPlanAWSDriftIncrementalRunNarrowsToChangedARNs pins the delta path:
// only added/changed/removed ARNs flow to the writer (#10878 decision 2).
func TestPlanAWSDriftIncrementalRunNarrowsToChangedARNs(t *testing.T) {
	plan := PlanAWSDriftIncrementalRun(AWSDriftIncrementalRun{
		PrevSnapshot: map[string]string{"arn:keep": "p1", "arn:chg": "p-old", "arn:gone": "p3"},
		NextSnapshot: map[string]string{"arn:keep": "p1", "arn:chg": "p-new", "arn:new": "p4"},
		LastAuditAt:  time.Now().Add(-time.Hour),
		Now:          time.Now(),
	})
	if plan.Mode != AWSDriftIncrementalModeDelta {
		t.Fatalf("Mode = %q, want delta (reason %q)", plan.Mode, plan.Reason)
	}
	assertStringSliceEqual(t, "added", plan.Diff.Added, []string{"arn:new"})
	assertStringSliceEqual(t, "changed", plan.Diff.Changed, []string{"arn:chg"})
	assertStringSliceEqual(t, "removed", plan.Diff.Removed, []string{"arn:gone"})
}

// TestPlanAWSDriftIncrementalRunMissingPrevReopensFull pins the
// never-skip-on-missing rule: no recorded snapshot forces a full run (#10878
// decision 4, absence re-opens).
func TestPlanAWSDriftIncrementalRunMissingPrevReopensFull(t *testing.T) {
	plan := PlanAWSDriftIncrementalRun(AWSDriftIncrementalRun{
		NextSnapshot: map[string]string{"arn:1": "p1"},
		LastAuditAt:  time.Now().Add(-time.Hour),
		Now:          time.Now(),
	})
	if plan.Mode != AWSDriftIncrementalModeFull {
		t.Fatalf("Mode = %q, want full (reason %q)", plan.Mode, plan.Reason)
	}
	if plan.Reason != AWSDriftIncrementalReasonFallbackNoSnapshot {
		t.Fatalf("Reason = %q, want %q", plan.Reason, AWSDriftIncrementalReasonFallbackNoSnapshot)
	}
}

// TestAWSDriftAuditIntervalMatchesReopenListingSQL pins the planner/listing
// twin (#11018 P1): the reopen listing's audit-stale leg must use the same
// 24h interval as the planner's audit clock, or scopes would audit on one
// cadence and reopen on another.
func TestAWSDriftAuditIntervalMatchesReopenListingSQL(t *testing.T) {
	t.Parallel()
	if awsDriftAuditInterval != 24*time.Hour {
		t.Fatalf("awsDriftAuditInterval = %v, want 24h", awsDriftAuditInterval)
	}
	want := "INTERVAL '24 hours'"
	if !strings.Contains(backfill.ListAWSDriftReopenCandidatesQuery, want) {
		t.Fatalf("reopen listing lacks %q: the audit-stale leg must twin the planner interval", want)
	}
}

// TestPlanAWSDriftIncrementalRunDailyAudit pins the #10861-D3 daily audit:
// a stale audit forces a full run even when the input did not change.
func TestPlanAWSDriftIncrementalRunDailyAudit(t *testing.T) {
	snap := map[string]string{"arn:1": "p1"}
	plan := PlanAWSDriftIncrementalRun(AWSDriftIncrementalRun{
		PrevSnapshot: snap,
		NextSnapshot: map[string]string{"arn:1": "p1"},
		LastAuditAt:  time.Now().Add(-25 * time.Hour),
		Now:          time.Now(),
	})
	if plan.Mode != AWSDriftIncrementalModeFull {
		t.Fatalf("Mode = %q, want full audit (reason %q)", plan.Mode, plan.Reason)
	}
	if plan.Reason != AWSDriftIncrementalReasonAuditDue {
		t.Fatalf("Reason = %q, want %q", plan.Reason, AWSDriftIncrementalReasonAuditDue)
	}
}

// TestFilterAddressedRowsToDiffMatchesFullMinusUnchanged pins AC2
// equivalence: the delta-filtered row set plus the untouched ARNs equals the
// full row set, and removals only retract (never widen to whole-repo).
func TestFilterAddressedRowsToDiffMatchesFullMinusUnchanged(t *testing.T) {
	rows := []cloudruntime.AddressedRow{
		{ARN: "arn:keep", ResourceType: "aws_instance"},
		{ARN: "arn:chg", ResourceType: "aws_instance"},
		{ARN: "arn:gone", ResourceType: "aws_instance"},
	}
	plan := PlanAWSDriftIncrementalRun(AWSDriftIncrementalRun{
		PrevSnapshot: map[string]string{"arn:keep": "p1", "arn:chg": "p-old", "arn:gone": "p3"},
		NextSnapshot: map[string]string{"arn:keep": "p1", "arn:chg": "p-new", "arn:new": "p4"},
		LastAuditAt:  time.Now().Add(-time.Hour),
		Now:          time.Now(),
	})
	kept := FilterAddressedRowsToDiff(rows, plan.Diff)
	if len(kept) != 2 {
		t.Fatalf("kept = %d rows, want 2 (changed + removed for retract)", len(kept))
	}
	// Removed ARNs retract through the diff's ChangedKeys (the retire
	// bound), not through the filtered rows: the helper carries no
	// removed-ARN return.
	assertStringSliceEqual(t, "diff removed", plan.Diff.Removed, []string{"arn:gone"})
	assertStringSliceEqual(t, "changed keys", plan.Diff.ChangedKeys(),
		[]string{"arn:chg", "arn:gone", "arn:new"})
	for _, row := range kept {
		if row.ARN == "arn:keep" {
			t.Fatalf("unchanged arn:keep must not flow to the delta writer")
		}
	}
}

// TestCountAuditCorrections pins the AC3 correction signal: a correction is
// an admitted candidate the delta run missed or misclassified.
func TestCountAuditCorrections(t *testing.T) {
	deltaAdmitted := []string{"arn:a"}
	fullAdmitted := []string{"arn:a", "arn:b"}
	if got := CountAuditCorrections(deltaAdmitted, fullAdmitted); got != 1 {
		t.Fatalf("CountAuditCorrections = %d, want 1", got)
	}
	if got := CountAuditCorrections(fullAdmitted, fullAdmitted); got != 0 {
		t.Fatalf("CountAuditCorrections identical = %d, want 0", got)
	}
}

// TestAWSCloudRuntimeDriftHandlerDeltaNarrowsWriter pins the handler side
// of AC2: a delta plan classifies only the diff and bounds the retire set
// to the diff's keys, so untouched ARNs are neither reclassified nor
// retired (the dead-letter never-widen rule).
func TestAWSCloudRuntimeDriftHandlerDeltaNarrowsWriter(t *testing.T) {
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
		EvidenceLoader:     loader,
		Writer:             writer,
		FencingTokenIssuer: &stubAWSCloudRuntimeDriftFencingTokenIssuer{tokens: []int64{7}},
		IncrementalPlan:    &plan,
	}
	result, err := handler.Handle(t.Context(), reducercontract.Intent{
		IntentID: "intent-delta", ScopeID: "aws:scope", GenerationID: "gen-1",
		SourceSystem: "aws", Domain: reducercontract.DomainAWSCloudRuntimeDrift, Cause: "test",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if result.Status != reducercontract.ResultStatusSucceeded {
		t.Fatalf("Handle().Status = %q, want succeeded", result.Status)
	}
	for _, arn := range writer.write.EvaluatedARNs {
		if arn == "arn:keep" {
			t.Fatalf("EvaluatedARNs = %v: untouched arn:keep must stay out of the retire bound", writer.write.EvaluatedARNs)
		}
	}
	if len(writer.write.EvaluatedARNs) != 1 || writer.write.EvaluatedARNs[0] != "arn:chg" {
		t.Fatalf("EvaluatedARNs = %v, want [arn:chg]", writer.write.EvaluatedARNs)
	}
	// The classified set is the delta claim: the writer must carry exactly
	// the changed ARN's candidate, so a broken narrowing filter (keep-all
	// rows) fails this suite, not just the helper unit test.
	if len(writer.write.Candidates) != 1 {
		t.Fatalf("Candidates = %d, want 1 (only arn:chg classified)", len(writer.write.Candidates))
	}
	for _, c := range writer.write.Candidates {
		if c.CorrelationKey == "arn:keep" {
			t.Fatalf("Candidates carry untouched arn:keep: %+v", writer.write.Candidates)
		}
	}
	if writer.write.Candidates[0].CorrelationKey != "arn:chg" {
		t.Fatalf("Candidates[0].CorrelationKey = %q, want arn:chg", writer.write.Candidates[0].CorrelationKey)
	}
	if writer.write.Summary.OrphanedResources != 1 {
		t.Fatalf("Summary.OrphanedResources = %d, want 1", writer.write.Summary.OrphanedResources)
	}
}

// TestAWSCloudRuntimeDriftHandlerSkipLoadsNothing pins the skip path: an
// unchanged input succeeds without touching the loader or writer.
func TestAWSCloudRuntimeDriftHandlerSkipLoadsNothing(t *testing.T) {
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
		EvidenceLoader:     loader,
		Writer:             writer,
		FencingTokenIssuer: &stubAWSCloudRuntimeDriftFencingTokenIssuer{tokens: []int64{7}},
		IncrementalPlan:    &plan,
	}
	result, err := handler.Handle(t.Context(), reducercontract.Intent{
		IntentID: "intent-skip", ScopeID: "aws:scope", GenerationID: "gen-1",
		SourceSystem: "aws", Domain: reducercontract.DomainAWSCloudRuntimeDrift, Cause: "test",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if result.Status != reducercontract.ResultStatusSucceeded {
		t.Fatalf("Handle().Status = %q, want succeeded", result.Status)
	}
	if loader.calls != 0 {
		t.Fatalf("loader calls = %d, want 0 on skip", loader.calls)
	}
	if writer.calls != 0 {
		t.Fatalf("writer calls = %d, want 0 on skip", writer.calls)
	}
}

type stubAWSDriftSnapshotLoader struct {
	snapshot reducercontract.AWSDriftReopenSnapshot
	calls    int
	err      error
}

func (s *stubAWSDriftSnapshotLoader) LoadAWSDriftARNSnapshot(
	_ context.Context,
	scopeID, generationID string,
) (reducercontract.AWSDriftReopenSnapshot, error) {
	s.calls++
	s.snapshot.ScopeID = scopeID
	s.snapshot.GenerationID = generationID
	return s.snapshot, s.err
}

type stubAWSDriftReopenRecorder struct {
	workItemID string
	claimedAt  time.Time
	snapshot   reducercontract.AWSDriftReopenSnapshot
	fullRun    bool
	calls      int
	err        error
}

func (s *stubAWSDriftReopenRecorder) RecordAWSDriftReopenFingerprint(
	_ context.Context,
	workItemID string,
	claimedAt time.Time,
	snapshot reducercontract.AWSDriftReopenSnapshot,
	fullRun bool,
) error {
	s.calls++
	s.workItemID = workItemID
	s.claimedAt = claimedAt
	s.snapshot = snapshot
	s.fullRun = fullRun
	return s.err
}

// TestAWSCloudRuntimeDriftHandlerRecordsSnapshotAfterWrite pins the
// first-domain reopen record: a claimed run that succeeds stores the ARN
// snapshot fenced on its claim, so the next maintenance pass can narrow to
// the diff or skip it (#10878).
func TestAWSCloudRuntimeDriftHandlerRecordsSnapshotAfterWrite(t *testing.T) {
	t.Parallel()
	claimedAt := time.Now().Add(-time.Minute).UTC()
	snapshotLoader := &stubAWSDriftSnapshotLoader{
		snapshot: reducercontract.AWSDriftReopenSnapshot{
			ARNIDs:           []string{"arn:1"},
			ARNPayloadHashes: []string{"h1"},
			Fingerprint:      "fp-1",
		},
	}
	recorder := &stubAWSDriftReopenRecorder{}
	handler := AWSCloudRuntimeDriftHandler{
		EvidenceLoader:            &stubAWSCloudRuntimeDriftEvidenceLoader{},
		Writer:                    &stubAWSCloudRuntimeDriftFindingWriter{},
		FencingTokenIssuer:        &stubAWSCloudRuntimeDriftFencingTokenIssuer{tokens: []int64{9}},
		SnapshotLoader:            snapshotLoader,
		ReopenFingerprintRecorder: recorder,
	}
	intent := reducercontract.Intent{
		IntentID: "intent-aws-record", ScopeID: "aws:scope", GenerationID: "gen-9",
		SourceSystem: "aws", Domain: reducercontract.DomainAWSCloudRuntimeDrift, Cause: "test",
	}.WithStatus(reducercontract.IntentStatusClaimed, claimedAt)
	if _, err := handler.Handle(t.Context(), intent); err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if snapshotLoader.calls != 1 {
		t.Fatalf("snapshot calls = %d, want 1", snapshotLoader.calls)
	}
	if recorder.calls != 1 {
		t.Fatalf("record calls = %d, want 1", recorder.calls)
	}
	if recorder.snapshot.Fingerprint != "fp-1" {
		t.Fatalf("recorded fingerprint = %q, want fp-1", recorder.snapshot.Fingerprint)
	}
	if !recorder.fullRun {
		t.Fatal("record fullRun = false, want true: a full-behavior run must advance the audit clock")
	}
}

// TestAWSCloudRuntimeDriftHandlerDeltaRecordPreservesAuditClock pins the
// D5 record distinction: a delta run records its snapshot without advancing
// the audit clock, so hourly deltas never defer the daily audit.
func TestAWSCloudRuntimeDriftHandlerDeltaRecordPreservesAuditClock(t *testing.T) {
	t.Parallel()
	claimedAt := time.Now().Add(-time.Minute).UTC()
	recorder := &stubAWSDriftReopenRecorder{}
	plan := PlanAWSDriftIncrementalRun(AWSDriftIncrementalRun{
		PrevSnapshot: map[string]string{"arn:1": "p1"},
		NextSnapshot: map[string]string{"arn:1": "p2"},
		LastAuditAt:  time.Now().Add(-time.Hour),
		Now:          time.Now(),
	})
	if plan.Mode != AWSDriftIncrementalModeDelta {
		t.Fatalf("plan Mode = %q, want delta", plan.Mode)
	}
	handler := AWSCloudRuntimeDriftHandler{
		EvidenceLoader:            &stubAWSCloudRuntimeDriftEvidenceLoader{},
		Writer:                    &stubAWSCloudRuntimeDriftFindingWriter{},
		FencingTokenIssuer:        &stubAWSCloudRuntimeDriftFencingTokenIssuer{tokens: []int64{9}},
		ReopenFingerprintRecorder: recorder,
		IncrementalPlan:           &plan,
		SnapshotLoader: &stubAWSDriftSnapshotLoader{
			snapshot: reducercontract.AWSDriftReopenSnapshot{Fingerprint: "fp-delta"},
		},
	}
	intent := reducercontract.Intent{
		IntentID: "intent-aws-delta-record", ScopeID: "aws:scope", GenerationID: "gen-9",
		SourceSystem: "aws", Domain: reducercontract.DomainAWSCloudRuntimeDrift, Cause: "test",
	}.WithStatus(reducercontract.IntentStatusClaimed, claimedAt)
	if _, err := handler.Handle(t.Context(), intent); err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if recorder.calls != 1 {
		t.Fatalf("record calls = %d, want 1", recorder.calls)
	}
	if recorder.fullRun {
		t.Fatal("record fullRun = true, want false: a delta run must preserve the audit clock")
	}
}

func assertStringSliceEqual(t *testing.T, name string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s = %v, want %v", name, got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("%s = %v, want %v", name, got, want)
		}
	}
}
