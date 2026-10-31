// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scannerworker

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

type recordingDerivedResultCommitter struct {
	called     bool
	mutation   workflow.ClaimMutation
	scope      scope.IngestionScope
	generation scope.ScopeGeneration
	facts      []facts.Envelope
}

func (c *recordingDerivedResultCommitter) CommitDerivedImageResult(
	_ context.Context, mutation workflow.ClaimMutation, scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration, sourceFacts []facts.Envelope,
) error {
	c.called = true
	c.mutation = mutation
	c.scope = scopeValue
	c.generation = generation
	c.facts = append(c.facts, sourceFacts...)
	return nil
}

func TestServiceDerivedImageCommitCompletesAtomically(t *testing.T) {
	t.Parallel()
	item := testScannerWorkItem()
	item.ScopeID = "image://derived/scan-target"
	item.AcceptanceUnitID = item.ScopeID
	claim := testScannerClaim(item)
	store := &recordingClaimStore{}
	legacy := &recordingClaimCommitter{}
	derived := &recordingDerivedResultCommitter{}
	analyzer := &recordingAnalyzer{result: AnalyzerResult{Output: FactOutput{
		TargetCount: 1, ResultCount: 1,
		Facts: []facts.Envelope{testScannerFact(t, item, claim, facts.ScannerWorkerAnalysisFactKind)},
	}}}
	service := testScannerService(store, legacy, analyzer)
	service.AnalyzerKind = AnalyzerImageUnpacking
	service.DerivedCommitter = derived
	if err := service.processClaimed(context.Background(), item, claim); err != nil {
		t.Fatalf("processClaimed() error = %v", err)
	}
	if !derived.called || len(derived.facts) != 1 || derived.scope.ScopeID != item.ScopeID ||
		derived.generation.GenerationID != item.GenerationID || derived.mutation.WorkItemID != item.WorkItemID ||
		!derived.mutation.DerivedScannerImage {
		t.Fatalf("derived atomic commit = %+v, want one claimed scan result", derived)
	}
	if legacy.called || store.completed {
		t.Fatal("derived scan used the split legacy fact/complete path")
	}
}

func TestServiceDerivedImageRequiresAtomicCommitter(t *testing.T) {
	t.Parallel()
	item := testScannerWorkItem()
	item.ScopeID = "image://derived/scan-target"
	item.AcceptanceUnitID = item.ScopeID
	claim := testScannerClaim(item)
	store := &recordingClaimStore{}
	legacy := &recordingClaimCommitter{}
	analyzer := &recordingAnalyzer{result: AnalyzerResult{Output: FactOutput{
		TargetCount: 1, ResultCount: 1,
		Facts: []facts.Envelope{testScannerFact(t, item, claim, facts.ScannerWorkerAnalysisFactKind)},
	}}}
	service := testScannerService(store, legacy, analyzer)
	service.AnalyzerKind = AnalyzerImageUnpacking
	if err := service.processClaimed(context.Background(), item, claim); err == nil {
		t.Fatal("derived scan succeeded without atomic result committer")
	}
	if legacy.called || store.completed {
		t.Fatal("derived scan used the split legacy fact/complete path")
	}
}

func TestServiceStaticImageKeepsClaimedFactCommit(t *testing.T) {
	t.Parallel()
	item := testScannerWorkItem()
	claim := testScannerClaim(item)
	store := &recordingClaimStore{}
	legacy := &recordingClaimCommitter{}
	derived := &recordingDerivedResultCommitter{}
	analyzer := &recordingAnalyzer{result: AnalyzerResult{Output: FactOutput{
		TargetCount: 1, ResultCount: 1,
		Facts: []facts.Envelope{testScannerFact(t, item, claim, facts.ScannerWorkerAnalysisFactKind)},
	}}}
	service := testScannerService(store, legacy, analyzer)
	service.AnalyzerKind = AnalyzerImageUnpacking
	service.DerivedCommitter = derived
	if err := service.processClaimed(context.Background(), item, claim); err != nil {
		t.Fatalf("processClaimed() error = %v", err)
	}
	if !legacy.called || !store.completed || derived.called || legacy.mutation.DerivedScannerImage {
		t.Fatalf("static claim routing: legacy=%t complete=%t derived=%t marker=%t",
			legacy.called, store.completed, derived.called, legacy.mutation.DerivedScannerImage)
	}
}
