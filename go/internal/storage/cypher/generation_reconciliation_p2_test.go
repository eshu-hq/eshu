// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestRepairAnchorsYieldDomainRetractAnchorsNotEdgeKeys is the failing-first
// proof for P2 #1: ReconciliationReport must expose the DOMAIN-SPECIFIC retract
// anchor that EdgeWriter.RetractEdges actually keys on (repo_id / scope_id /
// file_path), not the opaque EdgeKey. A repo_dependency finding for repo-a must
// yield retract anchor "repo-a", and feeding that anchor to RetractEdges must
// issue a canonical retract anchored on repo-a so the stranded edge is actually
// deleted. The opaque edge key "edge-gen1" must NEVER reach repo_ids.
func TestRepairAnchorsYieldDomainRetractAnchorsNotEdgeKeys(t *testing.T) {
	t.Parallel()

	authoritative := []AuthoritativePostgresGeneration{{
		Identity:     AcceptanceIdentity{ScopeID: "scope-1", AcceptanceUnitID: "repo-a", SourceRunID: "run-1"},
		GenerationID: "gen-2",
		ResolvedIDs:  resolvedIDSet("resolved-2"),
	}}
	graph := []GraphDenormalizedEdge{{
		EdgeKey:       "edge-gen1",
		Domain:        reducer.DomainRepoDependency,
		Identity:      AcceptanceIdentity{ScopeID: "scope-1", AcceptanceUnitID: "repo-a", SourceRunID: "run-1"},
		RetractAnchor: "repo-a",
		GenerationID:  "gen-1",
		ResolvedID:    "resolved-1",
	}}

	report := ClassifyReconciliationDrift(authoritative, graph)

	anchors := report.RepairAnchors()
	got := anchors[reducer.DomainRepoDependency]
	if len(got) != 1 || got[0] != "repo-a" {
		t.Fatalf("repair anchors for repo_dependency = %v, want [repo-a]", got)
	}
	for _, anchor := range got {
		if anchor == "edge-gen1" {
			t.Fatal("opaque edge key leaked into retract anchors; retract would build repo_ids=[edge-gen1] and never delete the stranded edge")
		}
	}

	// End-to-end: the anchors drive the real repo-scoped retract path.
	retractRows := make([]reducer.SharedProjectionIntentRow, 0, len(got))
	for _, anchor := range got {
		retractRows = append(retractRows, reducer.SharedProjectionIntentRow{
			RepositoryID: anchor,
			Payload:      map[string]any{"repo_id": anchor},
		})
	}
	executor := &recordingGroupExecutor{}
	writer := NewEdgeWriter(executor, 0)
	if err := writer.RetractEdges(
		context.Background(),
		reducer.DomainRepoDependency,
		retractRows,
		"reconciliation/dual_write",
	); err != nil {
		t.Fatalf("RetractEdges() error = %v", err)
	}
	if len(executor.groupCalls) != 1 {
		t.Fatalf("expected 1 grouped retract call, got %d", len(executor.groupCalls))
	}
	foundRepoA := false
	for _, stmt := range executor.groupCalls[0] {
		for _, id := range statementRepoAnchorIDs(stmt) {
			if id == "edge-gen1" {
				t.Fatalf("retract keyed on opaque edge key edge-gen1: %#v", stmt.Parameters)
			}
			if id == "repo-a" {
				foundRepoA = true
			}
		}
	}
	if !foundRepoA {
		t.Fatal("retract did not anchor on repo-a; stranded edge would survive")
	}
}

// TestRepairAnchorsScopeAnchoredDomainUsesScopeID proves the repair anchor is
// domain-aware: documentation edges retract on scope_id, so a documentation
// finding must yield the scope id as its retract anchor, not the repo id or the
// edge key.
func TestRepairAnchorsScopeAnchoredDomainUsesScopeID(t *testing.T) {
	t.Parallel()

	authoritative := []AuthoritativePostgresGeneration{{
		Identity:     AcceptanceIdentity{ScopeID: "scope-doc", AcceptanceUnitID: "unit-doc", SourceRunID: "run-1"},
		GenerationID: "gen-2",
	}}
	graph := []GraphDenormalizedEdge{{
		EdgeKey:       "edge-doc",
		Domain:        reducer.DomainDocumentationEdges,
		Identity:      AcceptanceIdentity{ScopeID: "scope-doc", AcceptanceUnitID: "unit-doc", SourceRunID: "run-1"},
		RetractAnchor: "section-scope-xyz",
		GenerationID:  "gen-1",
	}}

	report := ClassifyReconciliationDrift(authoritative, graph)

	got := report.RepairAnchors()[reducer.DomainDocumentationEdges]
	if len(got) != 1 || got[0] != "section-scope-xyz" {
		t.Fatalf("documentation repair anchor = %v, want [section-scope-xyz]", got)
	}
}

// TestClassifyReconciliationDriftExactAcceptanceIdentityNoCollapse is the
// failing-first proof for P2 #2: two authoritative rows that share an
// acceptance_unit_id but differ in scope_id/source_run_id/generation must NOT
// collapse. Each edge must be classified against the generation of its OWN exact
// (scope_id, acceptance_unit_id, source_run_id) key — no false retract, no
// missed drift.
func TestClassifyReconciliationDriftExactAcceptanceIdentityNoCollapse(t *testing.T) {
	t.Parallel()

	authoritative := []AuthoritativePostgresGeneration{
		{
			Identity:     AcceptanceIdentity{ScopeID: "scope-A", AcceptanceUnitID: "repo-shared", SourceRunID: "run-A"},
			GenerationID: "gen-A2",
			ResolvedIDs:  resolvedIDSet("resolved-A"),
		},
		{
			Identity:     AcceptanceIdentity{ScopeID: "scope-B", AcceptanceUnitID: "repo-shared", SourceRunID: "run-B"},
			GenerationID: "gen-B5",
			ResolvedIDs:  resolvedIDSet("resolved-B"),
		},
	}

	edges := []GraphDenormalizedEdge{
		{
			// Belongs to scope-A/run-A; its generation matches gen-A2 → in sync.
			EdgeKey:       "edge-A",
			Domain:        reducer.DomainRepoDependency,
			Identity:      AcceptanceIdentity{ScopeID: "scope-A", AcceptanceUnitID: "repo-shared", SourceRunID: "run-A"},
			RetractAnchor: "repo-shared",
			GenerationID:  "gen-A2",
			ResolvedID:    "resolved-A",
		},
		{
			// Belongs to scope-B/run-B; its generation matches gen-B5 → in sync.
			EdgeKey:       "edge-B",
			Domain:        reducer.DomainRepoDependency,
			Identity:      AcceptanceIdentity{ScopeID: "scope-B", AcceptanceUnitID: "repo-shared", SourceRunID: "run-B"},
			RetractAnchor: "repo-shared",
			GenerationID:  "gen-B5",
			ResolvedID:    "resolved-B",
		},
	}

	report := ClassifyReconciliationDrift(authoritative, edges)

	// If the views collapsed by acceptance_unit_id, one of these edges would be
	// classified against the wrong generation and falsely retracted.
	if !report.Converged() {
		t.Fatalf("exact-key edges must both be in sync; collapse produced drift, counts=%v", report.Counts)
	}
	if got := report.Counts[ReconciliationDriftInSync]; got != 2 {
		t.Fatalf("in_sync count = %d, want 2 (no collapse)", got)
	}
}

// TestClassifyReconciliationDriftExactKeyDetectsScopeSpecificDrift proves the
// inverse: when one exact-key slice legitimately drifted, keying by full
// identity detects exactly that edge and leaves the sibling slice untouched (no
// missed drift, no over-retract).
func TestClassifyReconciliationDriftExactKeyDetectsScopeSpecificDrift(t *testing.T) {
	t.Parallel()

	authoritative := []AuthoritativePostgresGeneration{
		{
			Identity:     AcceptanceIdentity{ScopeID: "scope-A", AcceptanceUnitID: "repo-shared", SourceRunID: "run-A"},
			GenerationID: "gen-A2",
			ResolvedIDs:  resolvedIDSet("resolved-A"),
		},
		{
			Identity:     AcceptanceIdentity{ScopeID: "scope-B", AcceptanceUnitID: "repo-shared", SourceRunID: "run-B"},
			GenerationID: "gen-B5",
			ResolvedIDs:  resolvedIDSet("resolved-B"),
		},
	}

	edges := []GraphDenormalizedEdge{
		{
			EdgeKey:       "edge-A-ok",
			Domain:        reducer.DomainRepoDependency,
			Identity:      AcceptanceIdentity{ScopeID: "scope-A", AcceptanceUnitID: "repo-shared", SourceRunID: "run-A"},
			RetractAnchor: "repo-shared",
			GenerationID:  "gen-A2",
			ResolvedID:    "resolved-A",
		},
		{
			// scope-B slice stale: graph holds gen-B4, authoritative is gen-B5.
			EdgeKey:       "edge-B-stale",
			Domain:        reducer.DomainRepoDependency,
			Identity:      AcceptanceIdentity{ScopeID: "scope-B", AcceptanceUnitID: "repo-shared", SourceRunID: "run-B"},
			RetractAnchor: "repo-shared",
			GenerationID:  "gen-B4",
			ResolvedID:    "resolved-B",
		},
	}

	report := ClassifyReconciliationDrift(authoritative, edges)

	if got := report.Counts[ReconciliationDriftInSync]; got != 1 {
		t.Fatalf("in_sync count = %d, want 1", got)
	}
	if got := report.Counts[ReconciliationDriftStaleGeneration]; got != 1 {
		t.Fatalf("stale_generation count = %d, want 1", got)
	}
	if report.Findings[0].Class != ReconciliationDriftInSync {
		t.Fatalf("scope-A edge class = %q, want in_sync", report.Findings[0].Class)
	}
	if report.Findings[1].Class != ReconciliationDriftStaleGeneration {
		t.Fatalf("scope-B edge class = %q, want stale_generation", report.Findings[1].Class)
	}
}
