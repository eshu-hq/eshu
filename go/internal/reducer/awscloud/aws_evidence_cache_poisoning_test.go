// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/cloudruntime"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

type sequencedAWSCloudRuntimeDriftEvidenceLoader struct {
	responses [][]cloudruntime.AddressedRow
	calls     int
}

func (s *sequencedAWSCloudRuntimeDriftEvidenceLoader) LoadAWSCloudRuntimeDriftEvidence(
	context.Context,
	string,
	string,
) ([]cloudruntime.AddressedRow, error) {
	idx := s.calls
	s.calls++
	if idx >= len(s.responses) {
		idx = len(s.responses) - 1
	}
	return append([]cloudruntime.AddressedRow(nil), s.responses[idx]...), nil
}

func awsDriftPoisoningOrphanRow(arn string) cloudruntime.AddressedRow {
	return cloudruntime.AddressedRow{
		ARN:          arn,
		ResourceType: "aws_ec2_instance",
		Cloud: &cloudruntime.ResourceRow{
			ARN:          arn,
			ResourceID:   "i-0123456789abcdef0",
			ResourceType: "aws_ec2_instance",
			ScopeID:      "aws:123456789012:us-east-1",
			Attributes:   map[string]string{"ami": "ami-runtime-drifted"},
		},
	}
}

func awsDriftPoisoningImageDriftRow(arn string) cloudruntime.AddressedRow {
	return cloudruntime.AddressedRow{
		ARN:          arn,
		ResourceType: "aws_ec2_instance",
		Cloud: &cloudruntime.ResourceRow{
			ARN:          arn,
			ResourceID:   "i-0123456789abcdef0",
			ResourceType: "aws_ec2_instance",
			ScopeID:      "aws:123456789012:us-east-1",
			Attributes:   map[string]string{"ami": "ami-runtime-drifted"},
		},
		State: &cloudruntime.ResourceRow{
			ARN:          arn,
			Address:      "aws_instance.app",
			ResourceType: "aws_instance",
			ScopeID:      "state_snapshot:s3:prod",
			Attributes:   map[string]string{"ami": "ami-declared-expected"},
		},
		Config: &cloudruntime.ResourceRow{
			ARN:          arn,
			Address:      "aws_instance.app",
			ResourceType: "aws_instance",
			ScopeID:      "git:infra",
		},
	}
}

// TestAWSCloudRuntimeDriftHandlerDoesNotPoisonEvidenceCacheOnStatePendingDefer
// pins the #10878 + #5848 interaction: a load taken while a state_snapshot
// scope is still pending returns incomplete cross-scope evidence (State == nil)
// and defers. It must never poison the shared AWS-fingerprint-keyed
// EvidenceCache, so the post-activation retry reloads fresh state and admits
// image_version_drift instead of cached orphaned_cloud_resource.
func TestAWSCloudRuntimeDriftHandlerDoesNotPoisonEvidenceCacheOnStatePendingDefer(t *testing.T) {
	t.Parallel()

	const (
		scopeID = "aws:123456789012:us-east-1"
		genID   = "gen-aws-1"
		arn     = "arn:aws:ec2:us-east-1:123456789012:instance/i-0123456789abcdef0"
	)
	now := time.Date(2026, time.November, 24, 12, 0, 0, 0, time.UTC)

	loader := &sequencedAWSCloudRuntimeDriftEvidenceLoader{
		responses: [][]cloudruntime.AddressedRow{
			{awsDriftPoisoningOrphanRow(arn)},
			{awsDriftPoisoningImageDriftRow(arn)},
		},
	}
	writer := &stubAWSCloudRuntimeDriftFindingWriter{}
	readiness := &stubAWSCloudRuntimeDriftReadinessChecker{pending: true}
	cache := NewAWSDriftEvidenceCache(DefaultAWSDriftEvidenceCacheMaxBytes)
	snapshots := &stubAWSDriftSnapshotLoader{
		snapshot: reducercontract.AWSDriftReopenSnapshot{
			ARNIDs:           []string{arn},
			ARNPayloadHashes: []string{"aws-payload-hash-1"},
			Fingerprint:      "fp-aws-only-1",
		},
	}
	recorded := &stubAWSDriftRecordedSnapshotLoader{ok: false}
	planner := &AWSDriftIncrementalPlanner{Snapshots: snapshots, Recorded: recorded}

	handler := AWSCloudRuntimeDriftHandler{
		EvidenceLoader:        loader,
		Writer:                writer,
		ReadinessChecker:      readiness,
		FencingTokenIssuer:    &stubAWSCloudRuntimeDriftFencingTokenIssuer{tokens: []int64{1, 2}},
		Now:                   func() time.Time { return now },
		IncrementalPlanSource: planner,
		EvidenceCache:         cache,
	}

	intent := reducercontract.Intent{
		IntentID:       "work-aws-drift-1",
		ScopeID:        scopeID,
		GenerationID:   genID,
		SourceSystem:   "aws",
		Domain:         reducercontract.DomainAWSCloudRuntimeDrift,
		Cause:          "aws runtime resource facts observed",
		EnqueuedAt:     now,
		CycleStartedAt: now,
	}

	// Attempt 1: Terraform state_snapshot scope is still pending -> must defer
	// without poisoning EvidenceCache.
	_, err := handler.Handle(t.Context(), intent)
	if err == nil {
		t.Fatal("attempt 1 Handle() error = nil, want state-pending defer error")
	}
	if class := reducerErrorFailureClass(t, err); class != AWSCloudRuntimeDriftStatePendingFailureClass {
		t.Fatalf("attempt 1 FailureClass() = %q, want %q", class, AWSCloudRuntimeDriftStatePendingFailureClass)
	}
	if writer.calls != 0 {
		t.Fatalf("attempt 1 writer.calls = %d, want 0", writer.calls)
	}
	if loader.calls != 1 {
		t.Fatalf("attempt 1 loader.calls = %d, want 1", loader.calls)
	}

	// Attempt 2: state_snapshot has activated (pending=false), AWS-only
	// fingerprint is identical -> must reload fresh evidence and write
	// image_version_drift, never cached orphaned_cloud_resource.
	readiness.pending = false
	now = now.Add(5 * time.Second)
	result, err := handler.Handle(t.Context(), intent)
	if err != nil {
		t.Fatalf("attempt 2 Handle() error = %v, want nil", err)
	}
	if result.Status != reducercontract.ResultStatusSucceeded {
		t.Fatalf("attempt 2 Status = %q, want succeeded", result.Status)
	}
	if loader.calls != 2 {
		t.Fatalf("attempt 2 loader.calls = %d, want 2 (must not serve poisoned EvidenceCache entry)", loader.calls)
	}
	if writer.calls != 1 {
		t.Fatalf("attempt 2 writer.calls = %d, want 1", writer.calls)
	}
	if writer.write.Summary.ImageVersionDriftResources != 1 || writer.write.Summary.OrphanedResources != 0 {
		t.Fatalf("attempt 2 Summary = %+v, want ImageVersionDriftResources=1 OrphanedResources=0", writer.write.Summary)
	}
	if len(writer.write.Candidates) != 1 || awsCloudRuntimeFindingKind(writer.write.Candidates[0]) != string(cloudruntime.FindingKindImageVersionDrift) {
		t.Fatalf("attempt 2 Candidates = %+v, want single %q candidate", writer.write.Candidates, cloudruntime.FindingKindImageVersionDrift)
	}
}

// TestAWSCloudRuntimeDriftHandlerInvalidatesCachedEvidenceOnAuditAndReopen
// pins that full re-derivations triggered by a daily audit (audit_due), a
// producer-activation reopen whose recorded fingerprint was cleared
// (fallback_no_snapshot), or a retry after a write error never reuse a stale
// AWS-only-keyed EvidenceCache entry from an earlier run in the same process.
func TestAWSCloudRuntimeDriftHandlerInvalidatesCachedEvidenceOnAuditAndReopen(t *testing.T) {
	t.Parallel()

	const (
		scopeID = "aws:123456789012:us-east-1"
		genID   = "gen-aws-1"
		arn     = "arn:aws:ec2:us-east-1:123456789012:instance/i-0123456789abcdef0"
	)

	t.Run("audit_due reloads fresh state", func(t *testing.T) {
		t.Parallel()
		now := time.Date(2026, time.November, 24, 12, 0, 0, 0, time.UTC)
		loader := &sequencedAWSCloudRuntimeDriftEvidenceLoader{
			responses: [][]cloudruntime.AddressedRow{
				{awsDriftPoisoningOrphanRow(arn)},
				{awsDriftPoisoningImageDriftRow(arn)},
			},
		}
		writer := &stubAWSCloudRuntimeDriftFindingWriter{}
		cache := NewAWSDriftEvidenceCache(DefaultAWSDriftEvidenceCacheMaxBytes)
		snapshots := &stubAWSDriftSnapshotLoader{
			snapshot: reducercontract.AWSDriftReopenSnapshot{
				ARNIDs:           []string{arn},
				ARNPayloadHashes: []string{"aws-payload-hash-1"},
				Fingerprint:      "fp-aws-only-1",
			},
		}
		recorded := &stubAWSDriftRecordedSnapshotLoader{ok: false}
		handler := AWSCloudRuntimeDriftHandler{
			EvidenceLoader:        loader,
			Writer:                writer,
			FencingTokenIssuer:    &stubAWSCloudRuntimeDriftFencingTokenIssuer{tokens: []int64{1, 2}},
			Now:                   func() time.Time { return now },
			IncrementalPlanSource: &AWSDriftIncrementalPlanner{Snapshots: snapshots, Recorded: recorded},
			EvidenceCache:         cache,
		}
		intent := reducercontract.Intent{
			IntentID: "work-audit-1", ScopeID: scopeID, GenerationID: genID,
			SourceSystem: "aws", Domain: reducercontract.DomainAWSCloudRuntimeDrift, Cause: "test",
		}

		// Run 1 populates EvidenceCache with State == nil (orphaned_cloud_resource).
		if _, err := handler.Handle(t.Context(), intent); err != nil {
			t.Fatalf("run 1 Handle() error = %v, want nil", err)
		}
		if writer.write.Summary.OrphanedResources != 1 {
			t.Fatalf("run 1 Summary = %+v, want OrphanedResources=1", writer.write.Summary)
		}

		// Run 2: 25h later, same AWS snapshot, audit_due -> must invalidate
		// cached entry and reload fresh state producing image_version_drift.
		recorded.ok = true
		recorded.prev = map[string]string{arn: "aws-payload-hash-1"}
		recorded.lastAuditAt = now
		now = now.Add(25 * time.Hour)

		if _, err := handler.Handle(t.Context(), intent); err != nil {
			t.Fatalf("run 2 Handle() error = %v, want nil", err)
		}
		if loader.calls != 2 {
			t.Fatalf("run 2 loader.calls = %d, want 2 (audit_due must invalidate cached entry)", loader.calls)
		}
		if writer.write.Summary.ImageVersionDriftResources != 1 || writer.write.Summary.OrphanedResources != 0 {
			t.Fatalf("run 2 Summary = %+v, want ImageVersionDriftResources=1 OrphanedResources=0", writer.write.Summary)
		}
	})

	t.Run("producer_activation reopen with cleared fingerprint reloads fresh state", func(t *testing.T) {
		t.Parallel()
		now := time.Date(2026, time.November, 24, 12, 0, 0, 0, time.UTC)
		loader := &sequencedAWSCloudRuntimeDriftEvidenceLoader{
			responses: [][]cloudruntime.AddressedRow{
				{awsDriftPoisoningOrphanRow(arn)},
				{awsDriftPoisoningImageDriftRow(arn)},
			},
		}
		writer := &stubAWSCloudRuntimeDriftFindingWriter{}
		cache := NewAWSDriftEvidenceCache(DefaultAWSDriftEvidenceCacheMaxBytes)
		snapshots := &stubAWSDriftSnapshotLoader{
			snapshot: reducercontract.AWSDriftReopenSnapshot{
				ARNIDs:           []string{arn},
				ARNPayloadHashes: []string{"aws-payload-hash-1"},
				Fingerprint:      "fp-aws-only-1",
			},
		}
		recorded := &stubAWSDriftRecordedSnapshotLoader{ok: false}
		handler := AWSCloudRuntimeDriftHandler{
			EvidenceLoader:        loader,
			Writer:                writer,
			FencingTokenIssuer:    &stubAWSCloudRuntimeDriftFencingTokenIssuer{tokens: []int64{1, 2}},
			Now:                   func() time.Time { return now },
			IncrementalPlanSource: &AWSDriftIncrementalPlanner{Snapshots: snapshots, Recorded: recorded},
			EvidenceCache:         cache,
		}
		intent := reducercontract.Intent{
			IntentID: "work-reopen-1", ScopeID: scopeID, GenerationID: genID,
			SourceSystem: "aws", Domain: reducercontract.DomainAWSCloudRuntimeDrift, Cause: "test",
		}

		// Run 1 populates EvidenceCache with State == nil.
		if _, err := handler.Handle(t.Context(), intent); err != nil {
			t.Fatalf("run 1 Handle() error = %v, want nil", err)
		}
		if writer.write.Summary.OrphanedResources != 1 {
			t.Fatalf("run 1 Summary = %+v, want OrphanedResources=1", writer.write.Summary)
		}

		// Run 2: producer activation cleared aws_drift_reopen_fingerprints
		// (recorded.ok == false -> fallback_no_snapshot), same AWS snapshot ->
		// must invalidate cached entry and reload fresh state.
		recorded.ok = false
		now = now.Add(10 * time.Second)

		if _, err := handler.Handle(t.Context(), intent); err != nil {
			t.Fatalf("run 2 Handle() error = %v, want nil", err)
		}
		if loader.calls != 2 {
			t.Fatalf("run 2 loader.calls = %d, want 2 (reopen must invalidate cached entry)", loader.calls)
		}
		if writer.write.Summary.ImageVersionDriftResources != 1 || writer.write.Summary.OrphanedResources != 0 {
			t.Fatalf("run 2 Summary = %+v, want ImageVersionDriftResources=1 OrphanedResources=0", writer.write.Summary)
		}
	})

	t.Run("write error invalidates cached entry for retry", func(t *testing.T) {
		t.Parallel()
		now := time.Date(2026, time.November, 24, 12, 0, 0, 0, time.UTC)
		loader := &sequencedAWSCloudRuntimeDriftEvidenceLoader{
			responses: [][]cloudruntime.AddressedRow{
				{awsDriftPoisoningOrphanRow(arn)},
				{awsDriftPoisoningImageDriftRow(arn)},
			},
		}
		writer := &stubAWSCloudRuntimeDriftFindingWriter{err: errors.New("transient write failure")}
		cache := NewAWSDriftEvidenceCache(DefaultAWSDriftEvidenceCacheMaxBytes)
		planSource := &stubAWSDriftIncrementalPlanSource{
			plan: AWSDriftIncrementalPlan{
				Mode:   AWSDriftIncrementalModeDelta,
				Reason: AWSDriftIncrementalReasonChanged,
				Diff:   reducercontract.ARNRecordDiff{Changed: []string{arn}},
			},
			snapshot: reducercontract.AWSDriftReopenSnapshot{
				ARNIDs:           []string{arn},
				ARNPayloadHashes: []string{"aws-payload-hash-1"},
				Fingerprint:      "fp-aws-only-delta",
			},
		}
		handler := AWSCloudRuntimeDriftHandler{
			EvidenceLoader:        loader,
			Writer:                writer,
			FencingTokenIssuer:    &stubAWSCloudRuntimeDriftFencingTokenIssuer{tokens: []int64{1, 2}},
			Now:                   func() time.Time { return now },
			IncrementalPlanSource: planSource,
			EvidenceCache:         cache,
		}
		intent := reducercontract.Intent{
			IntentID: "work-write-err-1", ScopeID: scopeID, GenerationID: genID,
			SourceSystem: "aws", Domain: reducercontract.DomainAWSCloudRuntimeDrift, Cause: "test",
		}

		if _, err := handler.Handle(t.Context(), intent); err == nil {
			t.Fatal("run 1 Handle() error = nil, want write error")
		}
		writer.err = nil
		if _, err := handler.Handle(t.Context(), intent); err != nil {
			t.Fatalf("run 2 Handle() error = %v, want nil", err)
		}
		if loader.calls != 2 {
			t.Fatalf("run 2 loader.calls = %d, want 2 (write error must invalidate cached entry)", loader.calls)
		}
		if writer.write.Summary.ImageVersionDriftResources != 1 {
			t.Fatalf("run 2 Summary = %+v, want ImageVersionDriftResources=1", writer.write.Summary)
		}
	})
}

// TestAWSDriftEvidenceCacheInvalidateDropsStoredEntryAndUpdatesBudget pins
// Invalidate's contract: dropping a stored flight removes the entry and
// returns its bytes to storedBytes so peers are not evicted, a missing key or
// nil receiver is a safe no-op, and an active in-flight leader (zero storedAt)
// is preserved so concurrent callers still coalesce.
func TestAWSDriftEvidenceCacheInvalidateDropsStoredEntryAndUpdatesBudget(t *testing.T) {
	t.Parallel()

	var nilCache *AWSDriftEvidenceCache
	nilCache.Invalidate("scope", "gen-1", "fp-1")

	cache := NewAWSDriftEvidenceCache(256)
	var loads atomic.Int64
	load := func(_ context.Context) ([]cloudruntime.AddressedRow, int64, error) {
		n := loads.Add(1)
		if n == 1 {
			return []cloudruntime.AddressedRow{{ARN: "arn:v1"}}, 128, nil
		}
		return []cloudruntime.AddressedRow{{ARN: "arn:v2"}}, 128, nil
	}

	ctx := t.Context()
	if _, _, err := cache.LoadShared(ctx, "scope", "gen-1", "fp-1", load); err != nil {
		t.Fatalf("load fp-1 err = %v", err)
	}
	if _, _, err := cache.LoadShared(ctx, "scope", "gen-1", "fp-2", load); err != nil {
		t.Fatalf("load fp-2 err = %v", err)
	}
	if cache.storedBytes != 256 {
		t.Fatalf("storedBytes before Invalidate = %d, want 256", cache.storedBytes)
	}

	// Missing key is a no-op.
	cache.Invalidate("scope", "gen-1", "fp-missing")
	if cache.storedBytes != 256 {
		t.Fatalf("storedBytes after missing Invalidate = %d, want 256", cache.storedBytes)
	}

	// Dropping fp-1 frees 128 bytes so loading fp-3 does not evict fp-2.
	cache.Invalidate("scope", "gen-1", "fp-1")
	if cache.storedBytes != 128 {
		t.Fatalf("storedBytes after Invalidate(fp-1) = %d, want 128", cache.storedBytes)
	}
	if _, _, err := cache.LoadShared(ctx, "scope", "gen-1", "fp-3", load); err != nil {
		t.Fatalf("load fp-3 err = %v", err)
	}
	if _, shared, err := cache.LoadShared(ctx, "scope", "gen-1", "fp-2", load); err != nil {
		t.Fatalf("reload fp-2 err = %v", err)
	} else if !shared {
		t.Fatal("fp-2 was evicted despite Invalidate(fp-1) freeing budget")
	}

	// Reloading invalidated fp-1 must miss and invoke loader again.
	rows, shared, err := cache.LoadShared(ctx, "scope", "gen-1", "fp-1", load)
	if err != nil {
		t.Fatalf("reload fp-1 err = %v", err)
	}
	if shared {
		t.Fatal("reloaded fp-1 shared = true, want false after Invalidate")
	}
	if len(rows) != 1 || rows[0].ARN != "arn:v2" {
		t.Fatalf("reloaded fp-1 rows = %+v, want [arn:v2]", rows)
	}

	// Active in-flight leader must not be deleted by Invalidate so concurrent
	// waiters still coalesce on it.
	inFlightCache := NewAWSDriftEvidenceCache(1 << 20)
	started := make(chan struct{})
	release := make(chan struct{})
	var startedOnce sync.Once
	var inflightLoads atomic.Int64
	blockingLoad := func(_ context.Context) ([]cloudruntime.AddressedRow, int64, error) {
		inflightLoads.Add(1)
		startedOnce.Do(func() { close(started) })
		<-release
		return []cloudruntime.AddressedRow{{ARN: "arn:inflight"}}, 64, nil
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if _, _, err := inFlightCache.LoadShared(ctx, "scope", "gen-2", "fp-live", blockingLoad); err != nil {
			t.Errorf("leader LoadShared err = %v", err)
		}
	}()
	<-started

	inFlightCache.Invalidate("scope", "gen-2", "fp-live")

	waiterShared := make(chan bool, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, shared, err := inFlightCache.LoadShared(ctx, "scope", "gen-2", "fp-live", blockingLoad)
		if err != nil {
			t.Errorf("waiter LoadShared err = %v", err)
		}
		waiterShared <- shared
	}()

	close(release)
	wg.Wait()
	if !<-waiterShared {
		t.Fatal("waiter shared = false, want true (Invalidate must preserve active in-flight leader)")
	}
	if got := inflightLoads.Load(); got != 1 {
		t.Fatalf("inflightLoads = %d, want 1", got)
	}
	if inFlightCache.storedBytes != 64 {
		t.Fatalf("inFlightCache.storedBytes = %d, want 64 after leader stored", inFlightCache.storedBytes)
	}
	inFlightCache.Invalidate("scope", "gen-2", "fp-live")
	if inFlightCache.storedBytes != 0 {
		t.Fatalf("inFlightCache.storedBytes = %d, want 0 after post-store Invalidate", inFlightCache.storedBytes)
	}
}
