// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker"
	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker/sbomgenerator"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestBuildAnalyzerUsesConfiguredSBOMGenerationSource proves the cmd wiring
// still builds the SBOM analyzer from configured targets after the
// repository-walk source moved to the sbomgenerator leaf package (Refs
// #8878): the worker behavior is unchanged, only the import path moved.
func TestBuildAnalyzerUsesConfiguredSBOMGenerationSource(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "package-lock.json"), `{
		"lockfileVersion": 3,
		"packages": {
			"": {"name": "test-app", "version": "1.0.0"},
			"node_modules/left-pad": {"version": "1.3.0"}
		}
	}`)
	writeTestFile(t, filepath.Join(root, "go.mod"), `module example.test/app

require github.com/gin-gonic/gin v1.9.1
`)

	config := runtimeConfig{
		Instance: workflow.DesiredCollectorInstance{
			InstanceID: "scanner-worker-sbom",
		},
		Analyzer: scannerworker.AnalyzerSBOMGeneration,
		SBOMTargets: []sbomgenerator.RepositoryTarget{{
			ScopeID:       "scanner-worker://repository/repo-private-name",
			RootPath:      root,
			SubjectDigest: "sha256:1111111111111111111111111111111111111111111111111111111111111111",
		}},
	}
	analyzer, err := buildAnalyzer(config)
	if err != nil {
		t.Fatalf("buildAnalyzer(sbom_generation) error = %v, want nil", err)
	}

	result, err := analyzer.Analyze(context.Background(), testSBOMClaimInput(t))
	if err != nil {
		t.Fatalf("Analyze() error = %v, want nil", err)
	}
	counts := countFactKinds(result.Output.Facts)
	if got, want := counts[chain.SBOMDocumentFactKind], 1; got != want {
		t.Fatalf("document facts = %d, want %d", got, want)
	}
	if got, want := counts[chain.SBOMComponentFactKind], 2; got != want {
		t.Fatalf("component facts = %d, want %d", got, want)
	}
	if counts[facts.ScannerWorkerWarningFactKind] != 0 {
		t.Fatalf("scanner-worker warning facts = %d, want 0 for configured source", counts[facts.ScannerWorkerWarningFactKind])
	}
	if result.Usage.PeakMemoryBytes == 0 {
		t.Fatal("PeakMemoryBytes = 0, want measured input bytes")
	}
}

func testSBOMClaimInput(t *testing.T) scannerworker.ClaimInput {
	t.Helper()

	now := time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)
	item := workflow.WorkItem{
		WorkItemID:          "scanner-worker:collector-scanner:work-1",
		RunID:               "scanner-worker:run-1",
		CollectorKind:       scope.CollectorScannerWorker,
		CollectorInstanceID: "scanner-worker-sbom",
		SourceSystem:        string(scope.CollectorScannerWorker),
		ScopeID:             "scanner-worker://repository/repo-private-name",
		AcceptanceUnitID:    "repository:repo-123",
		SourceRunID:         "scanner-worker:generation-1",
		GenerationID:        "scanner-worker:generation-1",
		FairnessKey:         "scanner_worker:scanner-worker-sbom:repository",
		Status:              workflow.WorkItemStatusClaimed,
		AttemptCount:        1,
		CurrentClaimID:      "claim-1",
		CurrentFencingToken: 7,
		CurrentOwnerID:      "scanner-worker-1",
		LeaseExpiresAt:      now.Add(time.Minute),
		VisibleAt:           now.Add(-time.Minute),
		LastClaimedAt:       now,
		CreatedAt:           now.Add(-time.Hour),
		UpdatedAt:           now,
	}
	claim := workflow.Claim{
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
	target, err := scannerworker.TargetScopeFromWorkItem(item)
	if err != nil {
		t.Fatalf("TargetScopeFromWorkItem() error = %v, want nil", err)
	}
	limits, err := scannerworker.DefaultResourceLimits(scannerworker.AnalyzerSBOMGeneration)
	if err != nil {
		t.Fatalf("DefaultResourceLimits() error = %v, want nil", err)
	}
	input, err := scannerworker.NewClaimInputAt(item, claim, scannerworker.AnalyzerSBOMGeneration, target, limits, now)
	if err != nil {
		t.Fatalf("NewClaimInputAt() error = %v, want nil", err)
	}
	return input
}

func writeTestFile(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
}

func countFactKinds(values []facts.Envelope) map[string]int {
	counts := make(map[string]int)
	for _, value := range values {
		counts[value.FactKind]++
	}
	return counts
}
