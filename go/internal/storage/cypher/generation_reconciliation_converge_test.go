// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// applyRetractWrite models how the existing retract-then-write projection path
// converges a graph holding a stale edge: the retract removes every edge for the
// affected retract anchor, and the authoritative write re-adds only the rows
// stamped with the authoritative generation. It returns the resulting edge set.
// This mirrors ProcessPartitionOnce's retract(latest+terminal) then
// write(latest) ordering at the granularity the reconciliation classifier
// reasons about. retractAnchors is keyed by the domain-specific retract anchor
// (e.g. repository id), the same key RepairAnchors yields.
func applyRetractWrite(
	existing []GraphDenormalizedEdge,
	retractAnchors map[string]struct{},
	authoritativeWrites []GraphDenormalizedEdge,
) []GraphDenormalizedEdge {
	converged := make([]GraphDenormalizedEdge, 0, len(existing)+len(authoritativeWrites))
	for _, edge := range existing {
		if _, retract := retractAnchors[edge.RetractAnchor]; retract {
			continue
		}
		converged = append(converged, edge)
	}
	converged = append(converged, authoritativeWrites...)
	return converged
}

// TestReconciliationConvergesStaleGenerationAfterPartialFailure proves the
// dual-write partial-failure recovery contract end to end at the classifier
// level: a generation swap left a stale edge in the graph (Postgres advanced to
// gen-2, the gen-1 graph retract failed), the reconciliation pass detects the
// drift and names the retract anchor to repair, and after the existing
// retract-then-write path runs against that anchor the graph converges to
// Postgres truth with zero stranded edges.
func TestReconciliationConvergesStaleGenerationAfterPartialFailure(t *testing.T) {
	t.Parallel()

	identity := AcceptanceIdentity{ScopeID: "scope-1", AcceptanceUnitID: "repo-a", SourceRunID: "run-1"}
	authoritative := []AuthoritativePostgresGeneration{{
		Identity:     identity,
		GenerationID: "gen-2",
		ResolvedIDs:  resolvedIDSet("resolved-2"),
	}}

	// Pre-reconciliation graph: only the stale gen-1 edge survived the swap.
	graph := []GraphDenormalizedEdge{{
		EdgeKey:       "edge-gen1",
		Domain:        reducer.DomainRepoDependency,
		Identity:      identity,
		RetractAnchor: "repo-a",
		GenerationID:  "gen-1",
		ResolvedID:    "resolved-1",
	}}

	report := ClassifyReconciliationDrift(authoritative, graph)
	if report.Converged() {
		t.Fatal("pre-reconciliation graph must report drift, not convergence")
	}

	// The pass names the domain-specific retract anchors that hold stranded edges.
	anchors := report.RepairAnchors()
	repoAnchors := anchors[reducer.DomainRepoDependency]
	if len(repoAnchors) != 1 || repoAnchors[0] != "repo-a" {
		t.Fatalf("reconciliation must target retract anchor repo-a, got %v", repoAnchors)
	}
	retractAnchors := map[string]struct{}{}
	for _, anchor := range repoAnchors {
		retractAnchors[anchor] = struct{}{}
	}

	// Authoritative re-projection of gen-2 truth for the repaired unit.
	authoritativeWrites := []GraphDenormalizedEdge{{
		EdgeKey:       "edge-gen2",
		Domain:        reducer.DomainRepoDependency,
		Identity:      identity,
		RetractAnchor: "repo-a",
		GenerationID:  "gen-2",
		ResolvedID:    "resolved-2",
	}}

	converged := applyRetractWrite(graph, retractAnchors, authoritativeWrites)

	postReport := ClassifyReconciliationDrift(authoritative, converged)
	if !postReport.Converged() {
		t.Fatalf("graph did not converge after retract+write, counts %v", postReport.Counts)
	}
	if got := postReport.Counts[ReconciliationDriftInSync]; got != 1 {
		t.Fatalf("converged in_sync count = %d, want 1", got)
	}
	// No stale gen-1 edge may survive.
	for _, edge := range converged {
		if edge.GenerationID == "gen-1" {
			t.Fatalf("stranded gen-1 edge survived reconciliation: %+v", edge)
		}
	}
}

// TestReconciliationDriftDrivesRepoScopedRetract proves the reconciliation pass
// wires to the existing repo-scoped retract path: feeding the RepairAnchors of
// drifted repo_dependency edges to EdgeWriter.RetractEdges issues a canonical
// retract anchored on those repos, the mechanism that clears stranded
// denormalized edges. This binds the new detector to the real convergence write
// path rather than asserting against a private copy of it.
func TestReconciliationDriftDrivesRepoScopedRetract(t *testing.T) {
	t.Parallel()

	identity := AcceptanceIdentity{ScopeID: "scope-1", AcceptanceUnitID: "repo-a", SourceRunID: "run-1"}
	authoritative := []AuthoritativePostgresGeneration{{
		Identity:     identity,
		GenerationID: "gen-2",
		ResolvedIDs:  resolvedIDSet("resolved-2"),
	}}
	graph := []GraphDenormalizedEdge{{
		EdgeKey:       "edge-gen1",
		Domain:        reducer.DomainRepoDependency,
		Identity:      identity,
		RetractAnchor: "repo-a",
		GenerationID:  "gen-1",
		ResolvedID:    "resolved-1",
	}}

	report := ClassifyReconciliationDrift(authoritative, graph)

	retractRows := make([]reducer.SharedProjectionIntentRow, 0)
	for _, anchor := range report.RepairAnchors()[reducer.DomainRepoDependency] {
		retractRows = append(retractRows, reducer.SharedProjectionIntentRow{
			RepositoryID: anchor,
			Payload:      map[string]any{"repo_id": anchor},
		})
	}
	if len(retractRows) != 1 {
		t.Fatalf("expected 1 retract row, got %d", len(retractRows))
	}

	executor := &sqlSequentialRecordingExecutor{}
	writer := NewEdgeWriter(executor, 0)
	if err := writer.RetractEdges(
		context.Background(),
		reducer.DomainRepoDependency,
		retractRows,
		"reconciliation/dual_write",
	); err != nil {
		t.Fatalf("RetractEdges() error = %v", err)
	}

	if len(executor.calls) == 0 {
		t.Fatal("expected sequential retract statements, got none")
	}
	stmts := executor.calls
	if len(stmts) == 0 {
		t.Fatal("retract issued no statements")
	}
	foundRepoIDAnchor := false
	for _, stmt := range stmts {
		if stmt.Operation != OperationCanonicalRetract {
			t.Fatalf("statement operation = %q, want canonical_retract", stmt.Operation)
		}
		repoIDs := statementRepoAnchorIDs(stmt)
		if len(repoIDs) == 1 && repoIDs[0] == "repo-a" {
			foundRepoIDAnchor = true
		}
	}
	if !foundRepoIDAnchor {
		t.Fatalf("retract must anchor on repo-a; statements = %s", summarizeStatements(stmts))
	}
}

func summarizeStatements(stmts []Statement) string {
	var b strings.Builder
	for i, stmt := range stmts {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(string(stmt.Operation))
	}
	return b.String()
}
