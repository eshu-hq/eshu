// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scannerworker

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestAnalyzerProfilesRouteHeavyAnalyzersToScannerWorkers(t *testing.T) {
	t.Parallel()

	for _, analyzer := range []AnalyzerKind{
		AnalyzerSBOMGeneration,
		AnalyzerImageUnpacking,
		AnalyzerSourceAnalysis,
		AnalyzerOSPackageExtraction,
		AnalyzerSecretScan,
		AnalyzerLicenseScan,
		AnalyzerMisconfigurationScan,
	} {
		lane, ok := AnalyzerLane(analyzer)
		if !ok {
			t.Fatalf("AnalyzerLane(%q) ok = false, want true", analyzer)
		}
		if lane != LaneScannerWorker {
			t.Fatalf("AnalyzerLane(%q) = %q, want %q", analyzer, lane, LaneScannerWorker)
		}
	}

	for _, analyzer := range []AnalyzerKind{
		AnalyzerVulnerabilityMatching,
		AnalyzerCoverageReadiness,
		AnalyzerSecurityPriority,
	} {
		lane, ok := AnalyzerLane(analyzer)
		if !ok {
			t.Fatalf("AnalyzerLane(%q) ok = false, want true", analyzer)
		}
		if lane != LaneReducer {
			t.Fatalf("AnalyzerLane(%q) = %q, want %q", analyzer, lane, LaneReducer)
		}
	}
}

func TestNewClaimInputCopiesWorkflowClaimBoundary(t *testing.T) {
	t.Parallel()

	item := testScannerWorkItem()
	claim := testScannerClaim(item)
	target := testTargetScope(item)
	limits := testResourceLimits()

	input, err := NewClaimInput(item, claim, AnalyzerSBOMGeneration, target, limits)
	if err != nil {
		t.Fatalf("NewClaimInput() error = %v, want nil", err)
	}
	if input.WorkItemID != item.WorkItemID {
		t.Fatalf("WorkItemID = %q, want %q", input.WorkItemID, item.WorkItemID)
	}
	if input.ClaimID != claim.ClaimID {
		t.Fatalf("ClaimID = %q, want %q", input.ClaimID, claim.ClaimID)
	}
	if input.FencingToken != claim.FencingToken {
		t.Fatalf("FencingToken = %d, want %d", input.FencingToken, claim.FencingToken)
	}
	if input.Target.ScopeID != item.ScopeID {
		t.Fatalf("Target.ScopeID = %q, want %q", input.Target.ScopeID, item.ScopeID)
	}
	if input.Limits.MemoryBytes != limits.MemoryBytes {
		t.Fatalf("Limits.MemoryBytes = %d, want %d", input.Limits.MemoryBytes, limits.MemoryBytes)
	}
}

func TestNewClaimInputRejectsNonScannerWorkflowItems(t *testing.T) {
	t.Parallel()

	item := testScannerWorkItem()
	item.CollectorKind = scope.CollectorVulnerabilityIntelligence
	claim := testScannerClaim(item)

	_, err := NewClaimInput(item, claim, AnalyzerSBOMGeneration, testTargetScope(item), testResourceLimits())
	if err == nil {
		t.Fatal("NewClaimInput() error = nil, want non-scanner rejection")
	}
	if got, want := err.Error(), "scanner_worker"; !strings.Contains(got, want) {
		t.Fatalf("NewClaimInput() error = %q, want substring %q", got, want)
	}
}

func TestNewClaimInputRejectsUnsafeTargetLocatorHash(t *testing.T) {
	t.Parallel()

	item := testScannerWorkItem()
	claim := testScannerClaim(item)
	target := testTargetScope(item)
	target.LocatorHash = "/Users/private/repo"

	_, err := NewClaimInput(item, claim, AnalyzerSBOMGeneration, target, testResourceLimits())
	if err == nil {
		t.Fatal("NewClaimInput() error = nil, want unsafe locator hash rejection")
	}
	if got, want := err.Error(), "sha256"; !strings.Contains(got, want) {
		t.Fatalf("NewClaimInput() error = %q, want substring %q", got, want)
	}
}

func TestNewClaimInputRejectsLeaseMismatch(t *testing.T) {
	t.Parallel()

	item := testScannerWorkItem()
	claim := testScannerClaim(item)
	claim.LeaseExpiresAt = claim.LeaseExpiresAt.Add(time.Minute)

	_, err := NewClaimInput(item, claim, AnalyzerSBOMGeneration, testTargetScope(item), testResourceLimits())
	if err == nil {
		t.Fatal("NewClaimInput() error = nil, want lease mismatch rejection")
	}
	if got, want := err.Error(), "lease_expires_at"; !strings.Contains(got, want) {
		t.Fatalf("NewClaimInput() error = %q, want substring %q", got, want)
	}
}

func TestNewClaimInputAtRejectsExpiredClaim(t *testing.T) {
	t.Parallel()

	item := testScannerWorkItem()
	claim := testScannerClaim(item)
	observedAt := claim.LeaseExpiresAt.Add(time.Nanosecond)

	_, err := NewClaimInputAt(item, claim, AnalyzerSBOMGeneration, testTargetScope(item), testResourceLimits(), observedAt)
	if err == nil {
		t.Fatal("NewClaimInputAt() error = nil, want expired claim rejection")
	}
	if got, want := err.Error(), "expired"; !strings.Contains(got, want) {
		t.Fatalf("NewClaimInputAt() error = %q, want substring %q", got, want)
	}
}

func TestValidateFactOutputRejectsSilentCleanOutput(t *testing.T) {
	t.Parallel()

	input := testClaimInput(t)
	output := FactOutput{TargetCount: 1}

	err := ValidateFactOutput(input, output)
	if err == nil {
		t.Fatal("ValidateFactOutput() error = nil, want silent clean output rejection")
	}
}

func TestValidateFactOutputRejectsReducerFacts(t *testing.T) {
	t.Parallel()

	input := testClaimInput(t)
	output := FactOutput{
		TargetCount: 1,
		ResultCount: 1,
		Facts: []facts.Envelope{{
			FactID:           "fact-reducer",
			ScopeID:          input.Target.ScopeID,
			GenerationID:     input.GenerationID,
			FactKind:         "reducer_supply_chain_impact_finding",
			StableFactKey:    "reducer-finding",
			SchemaVersion:    "1.0.0",
			CollectorKind:    string(scope.CollectorScannerWorker),
			FencingToken:     input.FencingToken,
			SourceConfidence: "reported",
			ObservedAt:       input.ObservedAt,
			Payload:          map[string]any{"status": "affected"},
		}},
	}

	err := ValidateFactOutput(input, output)
	if err == nil {
		t.Fatal("ValidateFactOutput() error = nil, want reducer fact rejection")
	}
	if got, want := err.Error(), "reducers own user-facing findings"; !strings.Contains(got, want) {
		t.Fatalf("ValidateFactOutput() error = %q, want substring %q", got, want)
	}
}

func TestValidateFactOutputAcceptsSourceFactsWithFencing(t *testing.T) {
	t.Parallel()

	input := testClaimInput(t)
	output := FactOutput{
		TargetCount: 1,
		ResultCount: 1,
		Facts: []facts.Envelope{{
			FactID:           "fact-analysis",
			ScopeID:          input.Target.ScopeID,
			GenerationID:     input.GenerationID,
			FactKind:         facts.ScannerWorkerAnalysisFactKind,
			StableFactKey:    "scanner-worker-analysis",
			SchemaVersion:    facts.ScannerWorkerSchemaVersionV1,
			CollectorKind:    string(scope.CollectorScannerWorker),
			FencingToken:     input.FencingToken,
			SourceConfidence: "reported",
			ObservedAt:       input.ObservedAt,
			Payload: map[string]any{
				"analyzer": string(input.Analyzer),
				"target":   string(input.Target.Kind),
			},
			SourceRef: facts.Ref{
				SourceSystem: string(scope.CollectorScannerWorker),
				ScopeID:      input.Target.ScopeID,
				GenerationID: input.GenerationID,
				FactKey:      "scanner-worker-analysis",
			},
		}},
	}

	if err := ValidateFactOutput(input, output); err != nil {
		t.Fatalf("ValidateFactOutput() error = %v, want nil", err)
	}
}

func TestFailurePayloadDoesNotExposeRawTargetScope(t *testing.T) {
	t.Parallel()

	input := testClaimInput(t)
	payload, err := FailurePayloadFor(input, FailureDeadLetter, "memory_limit_exceeded", ResourceUsage{
		CPUSeconds:      7.5,
		PeakMemoryBytes: 3 << 30,
	})
	if err != nil {
		t.Fatalf("FailurePayloadFor() error = %v, want nil", err)
	}
	if payload.TargetLocatorHash != input.Target.LocatorHash {
		t.Fatalf("TargetLocatorHash = %q, want %q", payload.TargetLocatorHash, input.Target.LocatorHash)
	}
	if strings.Contains(payload.String(), input.Target.ScopeID) {
		t.Fatalf("FailurePayload leaked raw scope id %q in %q", input.Target.ScopeID, payload.String())
	}
	if payload.Retryable {
		t.Fatal("Retryable = true, want false for dead-letter payload")
	}
}

func TestFailurePayloadRejectsUnboundedFailureClass(t *testing.T) {
	t.Parallel()

	input := testClaimInput(t)
	_, err := FailurePayloadFor(input, FailureDeadLetter, "panic: /Users/private/repo", ResourceUsage{})
	if err == nil {
		t.Fatal("FailurePayloadFor() error = nil, want unbounded failure class rejection")
	}
	if got, want := err.Error(), "failure_class"; !strings.Contains(got, want) {
		t.Fatalf("FailurePayloadFor() error = %q, want substring %q", got, want)
	}
}

func testClaimInput(t *testing.T) ClaimInput {
	t.Helper()

	item := testScannerWorkItem()
	claim := testScannerClaim(item)
	input, err := NewClaimInput(item, claim, AnalyzerSBOMGeneration, testTargetScope(item), testResourceLimits())
	if err != nil {
		t.Fatalf("NewClaimInput() error = %v, want nil", err)
	}
	return input
}

func testScannerWorkItem() workflow.WorkItem {
	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	return workflow.WorkItem{
		WorkItemID:          "scanner-worker:collector-scanner:work-1",
		RunID:               "scanner-worker:run-1",
		CollectorKind:       scope.CollectorScannerWorker,
		CollectorInstanceID: "collector-scanner",
		SourceSystem:        string(scope.CollectorScannerWorker),
		ScopeID:             "scanner-worker://repository/repo-private-name",
		AcceptanceUnitID:    "repository:repo-123",
		SourceRunID:         "scanner-worker:generation-1",
		GenerationID:        "scanner-worker:generation-1",
		FairnessKey:         "scanner_worker:collector-scanner:repository",
		Status:              workflow.WorkItemStatusClaimed,
		AttemptCount:        2,
		CurrentClaimID:      "claim-1",
		CurrentFencingToken: 7,
		CurrentOwnerID:      "scanner-worker-1",
		LeaseExpiresAt:      now.Add(time.Minute),
		VisibleAt:           now.Add(-time.Minute),
		LastClaimedAt:       now,
		CreatedAt:           now.Add(-time.Hour),
		UpdatedAt:           now,
	}
}

func testScannerClaim(item workflow.WorkItem) workflow.Claim {
	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	return workflow.Claim{
		ClaimID:        item.CurrentClaimID,
		WorkItemID:     item.WorkItemID,
		FencingToken:   item.CurrentFencingToken,
		OwnerID:        item.CurrentOwnerID,
		Status:         workflow.ClaimStatusActive,
		ClaimedAt:      now,
		HeartbeatAt:    now,
		LeaseExpiresAt: now.Add(time.Minute),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

func testTargetScope(item workflow.WorkItem) TargetScope {
	return TargetScope{
		Kind:             TargetRepository,
		ScopeID:          item.ScopeID,
		AcceptanceUnitID: item.AcceptanceUnitID,
		SourceRunID:      item.SourceRunID,
		GenerationID:     item.GenerationID,
		LocatorHash:      "sha256:6b1f0b588fce9b40d6f56e4b5d6f3ef9d76c3ee6f2c2b66f7f4b3b6fb2c5c111",
	}
}

func testResourceLimits() ResourceLimits {
	return ResourceLimits{
		CPUMillis:     2000,
		MemoryBytes:   1 << 30,
		Timeout:       10 * time.Minute,
		MaxInputBytes: 2 << 30,
		MaxFiles:      250000,
		MaxFacts:      50000,
	}
}
