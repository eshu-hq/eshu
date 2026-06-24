// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbomgenerator

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestAnalyzerAggregatesRepeatedComponentMissingIdentityWarnings(t *testing.T) {
	t.Parallel()

	input := testClaimInput(t)
	components := []Component{
		{PURL: "pkg:npm/kept@1.0.0", Name: "kept", Version: "1.0.0"},
	}
	for range 25 {
		components = append(components, Component{Type: "library"})
	}
	source := &stubSource{
		inventory: Inventory{
			SubjectDigest: "sha256:11111111111111111111111111111111111111111111111111111111111111aa",
			Components:    components,
		},
	}
	analyzer := Analyzer{Source: source, Now: testClock}

	result, err := analyzer.Analyze(context.Background(), input)
	if err != nil {
		t.Fatalf("Analyze() error = %v, want nil", err)
	}
	warnings := warningFactsWithReason(result.Output.Facts, WarningReasonComponentMissingIdentity)
	if got, want := len(warnings), 1; got != want {
		t.Fatalf("component_missing_identity warnings = %d, want %d", got, want)
	}
	warning := warnings[0]
	if got, want := warning.Payload["occurrence_count"], 25; got != want {
		t.Fatalf("occurrence_count = %#v, want %#v", got, want)
	}
	samples, ok := warning.Payload["sample_component_indexes"].([]int)
	if !ok {
		t.Fatalf("sample_component_indexes = %T, want []int", warning.Payload["sample_component_indexes"])
	}
	if got, want := len(samples), 5; got != want {
		t.Fatalf("sample_component_indexes len = %d, want %d", got, want)
	}
	if got, want := samples[0], 1; got != want {
		t.Fatalf("first sample index = %d, want %d", got, want)
	}
	if summary, _ := warning.Payload["summary"].(string); !strings.Contains(summary, "25 components missing") {
		t.Fatalf("summary = %q, want aggregate count", summary)
	}
	counts := countFactKinds(result.Output.Facts)
	if got, want := counts[facts.SBOMComponentFactKind], 1; got != want {
		t.Fatalf("component facts = %d, want %d", got, want)
	}
	if err := scannerworker.ValidateFactOutput(input, result.Output); err != nil {
		t.Fatalf("ValidateFactOutput() error = %v, want nil", err)
	}
}

func TestAnalyzerAllowsAggregatedMissingIdentityWarningsWithinFactLimit(t *testing.T) {
	t.Parallel()

	input := testClaimInput(t)
	input.Limits.MaxFacts = 3
	components := make([]Component, 0, 26)
	for range 25 {
		components = append(components, Component{Type: "library"})
	}
	components = append(components, Component{PURL: "pkg:npm/kept@1.0.0", Name: "kept", Version: "1.0.0"})
	source := &stubSource{
		inventory: Inventory{
			SubjectDigest: "sha256:11111111111111111111111111111111111111111111111111111111111111aa",
			Components:    components,
		},
	}
	analyzer := Analyzer{Source: source, Now: testClock}

	result, err := analyzer.Analyze(context.Background(), input)
	if err != nil {
		t.Fatalf("Analyze() error = %v, want nil when aggregate output fits max_facts", err)
	}
	if got, want := len(result.Output.Facts), 3; got != want {
		t.Fatalf("facts len = %d, want %d", got, want)
	}
	if err := scannerworker.ValidateFactOutput(input, result.Output); err != nil {
		t.Fatalf("ValidateFactOutput() error = %v, want nil", err)
	}
}

func TestAnalyzerAllowsSingleComponentAtExactFactLimit(t *testing.T) {
	t.Parallel()

	input := testClaimInput(t)
	input.Limits.MaxFacts = 2
	source := &stubSource{
		inventory: Inventory{
			SubjectDigest: "sha256:11111111111111111111111111111111111111111111111111111111111111aa",
			Components: []Component{
				{PURL: "pkg:npm/kept@1.0.0", Name: "kept", Version: "1.0.0"},
			},
		},
	}
	analyzer := Analyzer{Source: source, Now: testClock}

	result, err := analyzer.Analyze(context.Background(), input)
	if err != nil {
		t.Fatalf("Analyze() error = %v, want nil when exact output fits max_facts", err)
	}
	if got, want := len(result.Output.Facts), 2; got != want {
		t.Fatalf("facts len = %d, want %d", got, want)
	}
	if err := scannerworker.ValidateFactOutput(input, result.Output); err != nil {
		t.Fatalf("ValidateFactOutput() error = %v, want nil", err)
	}
}

func warningFactsWithReason(envelopes []facts.Envelope, reason string) []facts.Envelope {
	out := make([]facts.Envelope, 0)
	for _, env := range envelopes {
		if env.FactKind != facts.SBOMWarningFactKind {
			continue
		}
		if got, _ := env.Payload["reason"].(string); got == reason {
			out = append(out, env)
		}
	}
	return out
}

func BenchmarkAnalyzerAggregatedMissingIdentityWarnings(b *testing.B) {
	input := benchmarkClaimInput(b)
	components := make([]Component, 0, 10001)
	components = append(components, Component{PURL: "pkg:npm/kept@1.0.0", Name: "kept", Version: "1.0.0"})
	for range 10000 {
		components = append(components, Component{Type: "library"})
	}
	source := &stubSource{
		inventory: Inventory{
			SubjectDigest: "sha256:11111111111111111111111111111111111111111111111111111111111111aa",
			Components:    components,
		},
	}
	analyzer := Analyzer{Source: source, Now: testClock}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		result, err := analyzer.Analyze(context.Background(), input)
		if err != nil {
			b.Fatalf("Analyze() error = %v, want nil", err)
		}
		counts := countFactKinds(result.Output.Facts)
		if counts[facts.SBOMWarningFactKind] != 1 {
			b.Fatalf("warning facts = %d, want 1", counts[facts.SBOMWarningFactKind])
		}
	}
}

func benchmarkClaimInput(b *testing.B) scannerworker.ClaimInput {
	b.Helper()

	now := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	item := workflow.WorkItem{
		WorkItemID:          "scanner-worker:collector-scanner:work-benchmark",
		RunID:               "scanner-worker:run-benchmark",
		CollectorKind:       scope.CollectorScannerWorker,
		CollectorInstanceID: "collector-scanner",
		SourceSystem:        string(scope.CollectorScannerWorker),
		ScopeID:             "scanner-worker://repository/repo-private-name",
		AcceptanceUnitID:    "repository:repo-123",
		SourceRunID:         "scanner-worker:generation-benchmark",
		GenerationID:        "scanner-worker:generation-benchmark",
		FairnessKey:         "scanner_worker:collector-scanner:repository",
		Status:              workflow.WorkItemStatusClaimed,
		AttemptCount:        1,
		CurrentClaimID:      "claim-benchmark",
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
	target := scannerworker.TargetScope{
		Kind:             scannerworker.TargetRepository,
		ScopeID:          item.ScopeID,
		AcceptanceUnitID: item.AcceptanceUnitID,
		SourceRunID:      item.SourceRunID,
		GenerationID:     item.GenerationID,
		LocatorHash:      "sha256:6b1f0b588fce9b40d6f56e4b5d6f3ef9d76c3ee6f2c2b66f7f4b3b6fb2c5c111",
	}
	limits := scannerworker.ResourceLimits{
		CPUMillis:     2000,
		MemoryBytes:   1 << 30,
		Timeout:       10 * time.Minute,
		MaxInputBytes: 2 << 30,
		MaxFiles:      250000,
		MaxFacts:      50000,
	}
	input, err := scannerworker.NewClaimInput(item, claim, scannerworker.AnalyzerSBOMGeneration, target, limits)
	if err != nil {
		b.Fatalf("NewClaimInput() error = %v, want nil", err)
	}
	return input
}
