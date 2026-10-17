// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

// cleanupStatements runs the production writer for a re-keyed repository and
// returns the path-conflict cleanup statements it sent.
func cleanupStatements(t *testing.T, bare bool) []Statement {
	t.Helper()
	exec := &mockExecutor{}
	writer := NewCanonicalNodeWriter(exec, 500, nil)
	if bare {
		writer = writer.WithBareRepositoryRetirement()
	}
	if err := writer.Write(context.Background(), retirementMaterialization(false, false)); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	var out []Statement
	for _, stmt := range exec.calls {
		if stmt.Cypher == canonicalNodeRepositoryPathCleanupCypher ||
			stmt.Cypher == canonicalNodeRepositoryPathCleanupBareCypher {
			out = append(out, stmt)
		}
	}
	return out
}

// TestRepositoryPathCleanupShapesRetireTheSameSet is the #8240 benign-
// divergence proof the backend-divergence allowlist cites. The attributed
// (Neo4j) and bare (NornicDB) statements must select and delete the same
// Repository rows: same path anchor, same id guard, DETACH DELETE of the
// matched node. The attributed shape may add only the incident-edge
// collection (OPTIONAL MATCH ... WITH r, collect(...)) between the guard and
// the delete, and the RETURN record after it. Any extra filter or a changed
// guard would make the two backends retire different nodes, so the
// allowlist could no longer call the one-sided recording benign.
func TestRepositoryPathCleanupShapesRetireTheSameSet(t *testing.T) {
	t.Parallel()
	attributed := strings.Join(strings.Fields(canonicalNodeRepositoryPathCleanupCypher), " ")
	bare := strings.Join(strings.Fields(canonicalNodeRepositoryPathCleanupBareCypher), " ")

	const matchAndGuard = "MATCH (r:Repository {path: $path}) WHERE r.id <> $repo_id"
	const deleteClause = "DETACH DELETE r"
	if bare != matchAndGuard+" "+deleteClause {
		t.Fatalf("bare shape = %q, want exactly %q", bare, matchAndGuard+" "+deleteClause)
	}
	if !strings.HasPrefix(attributed, matchAndGuard+" ") {
		t.Fatalf("attributed shape = %q, want the same anchor and guard prefix %q", attributed, matchAndGuard)
	}
	rest := strings.TrimPrefix(attributed, matchAndGuard+" ")
	before, after, ok := strings.Cut(rest, " "+deleteClause)
	if !ok {
		t.Fatalf("attributed shape has no %q: %q", deleteClause, attributed)
	}
	if !strings.HasPrefix(before, "OPTIONAL MATCH (r)-[rel]-(other) WITH r, collect(") {
		t.Fatalf("attributed shape adds %q before the delete, want only the incident-edge collection", before)
	}
	if strings.Contains(before, "WHERE") || strings.Count(before, "MATCH") != 1 {
		t.Fatalf("attributed collection %q adds a filter or a second match; it must not change the retired set", before)
	}
	if after != " RETURN dropped" {
		t.Fatalf("attributed tail after the delete = %q, want %q", after, " RETURN dropped")
	}
}

// TestRepositoryPathCleanupShapeFollowsWriterBackend pins which backend gets
// which shape and that the two carry identical parameters and summary key,
// so the differential records the same statement on both backends apart from
// the shape itself.
func TestRepositoryPathCleanupShapeFollowsWriterBackend(t *testing.T) {
	t.Parallel()
	neo4j := cleanupStatements(t, false)
	nornic := cleanupStatements(t, true)
	if len(neo4j) != 1 || len(nornic) != 1 {
		t.Fatalf("cleanup statements neo4j=%d nornicdb=%d, want 1 each", len(neo4j), len(nornic))
	}
	if neo4j[0].Cypher != canonicalNodeRepositoryPathCleanupCypher {
		t.Fatal("default writer must emit the attributed shape")
	}
	if nornic[0].Cypher != canonicalNodeRepositoryPathCleanupBareCypher {
		t.Fatal("bare-retirement writer must emit the bare shape")
	}
	if !reflect.DeepEqual(neo4j[0].Parameters, nornic[0].Parameters) {
		t.Fatalf("parameters differ: neo4j=%v nornicdb=%v", neo4j[0].Parameters, nornic[0].Parameters)
	}
	if neo4j[0].Operation != nornic[0].Operation {
		t.Fatalf("operation differs: neo4j=%v nornicdb=%v", neo4j[0].Operation, nornic[0].Operation)
	}
}
