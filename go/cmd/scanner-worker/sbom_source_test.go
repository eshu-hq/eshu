package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

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
		SBOMTargets: []sbomTargetConfig{{
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
	if got, want := counts[facts.SBOMDocumentFactKind], 1; got != want {
		t.Fatalf("document facts = %d, want %d", got, want)
	}
	if got, want := counts[facts.SBOMComponentFactKind], 2; got != want {
		t.Fatalf("component facts = %d, want %d", got, want)
	}
	if counts[facts.ScannerWorkerWarningFactKind] != 0 {
		t.Fatalf("scanner-worker warning facts = %d, want 0 for configured source", counts[facts.ScannerWorkerWarningFactKind])
	}
	if result.Usage.PeakMemoryBytes == 0 {
		t.Fatal("PeakMemoryBytes = 0, want measured input bytes")
	}
}

func TestRepositorySBOMSourceEnforcesFileLimitWithoutLeakingPaths(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "package-lock.json"), `{"lockfileVersion":3,"packages":{}}`)
	writeTestFile(t, filepath.Join(root, "go.mod"), "module example.test/app\n")

	source, err := newRepositorySBOMSource([]sbomTargetConfig{{
		ScopeID:  "scanner-worker://repository/repo-private-name",
		RootPath: root,
	}})
	if err != nil {
		t.Fatalf("newRepositorySBOMSource() error = %v, want nil", err)
	}
	input := testSBOMClaimInput(t)
	input.Limits.MaxFiles = 1

	_, err = source.Collect(context.Background(), input)
	if err == nil {
		t.Fatal("Collect() error = nil, want file limit failure")
	}
	var failure scannerworker.AnalyzerFailure
	if !errors.As(err, &failure) {
		t.Fatalf("Collect() error = %T, want scannerworker.AnalyzerFailure", err)
	}
	if got, want := failure.FailureClass(), scannerworker.FailureClassFileLimitExceeded; got != want {
		t.Fatalf("FailureClass = %q, want %q", got, want)
	}
	if got := err.Error(); containsAny(got, root, "package-lock.json", "go.mod") {
		t.Fatalf("Collect() error leaked private target detail: %q", got)
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

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if needle != "" && strings.Contains(value, needle) {
			return true
		}
	}
	return false
}
