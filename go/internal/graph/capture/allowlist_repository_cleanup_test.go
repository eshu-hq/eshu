// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package capture

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/backendconformance"
	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// cleanupRecordingExecutor records every statement the production
// CanonicalNodeWriter sends, so the test reads the Repository path-conflict
// cleanup text from the writer itself and never from a copy.
type cleanupRecordingExecutor struct{ calls []sourcecypher.Statement }

func (e *cleanupRecordingExecutor) Execute(_ context.Context, stmt sourcecypher.Statement) error {
	e.calls = append(e.calls, stmt)
	return nil
}

// repositoryCleanupStatement drives the production writer for a repository
// materialization that is neither the first generation nor a delta (the
// corpus gate's staged second generation) and returns the one path-conflict
// cleanup statement it emits. The writer sends it on every such
// materialization; the MATCH deletes only when a different-id Repository
// holds the path. bare selects the NornicDB
// writer configuration (#8173).
func repositoryCleanupStatement(t *testing.T, bare bool) sourcecypher.Statement {
	t.Helper()
	exec := &cleanupRecordingExecutor{}
	writer := sourcecypher.NewCanonicalNodeWriter(exec, 500, nil)
	if bare {
		writer = writer.WithBareRepositoryRetirement()
	}
	mat := canonical.CanonicalMaterialization{
		ScopeID: "scope-1", GenerationID: "gen-2", RepoID: "repository:r_new", RepoPath: "/repos/service",
		Repository: &canonical.RepositoryRow{
			RepoID: "repository:r_new", Name: "service", Path: "/repos/service", LocalPath: "/repos/service",
		},
	}
	if err := writer.Write(context.Background(), mat); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	var found []sourcecypher.Statement
	for _, stmt := range exec.calls {
		if stmt.Parameters[sourcecypher.StatementMetadataSummaryKey] == "repository_cleanup lookup=path_conflict" {
			found = append(found, stmt)
		}
	}
	if len(found) != 1 {
		t.Fatalf("path-conflict cleanup statements = %d, want 1 (bare=%v)", len(found), bare)
	}
	return found[0]
}

// TestAllowlistExcusesRepositoryPathConflictCleanupOnBothBackends pins the
// #8240 entries to the production writer: the attributed shape (Neo4j) and
// the bare shape (NornicDB) are each recorded on one backend only by
// design, so each must be excused as a missing-kind divergence, with the
// staged paths bound to any parameters.
func TestAllowlistExcusesRepositoryPathConflictCleanupOnBothBackends(t *testing.T) {
	t.Parallel()
	allow := loadBackendDivergenceAllowlist(t)
	for _, tc := range []struct {
		name string
		bare bool
	}{
		{"attributed-neo4j", false},
		{"bare-nornicdb", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stmt := repositoryCleanupStatement(t, tc.bare)
			fingerprint, err := backendconformance.FingerprintStatement(stmt.Cypher, stmt.Parameters)
			if err != nil {
				t.Fatalf("FingerprintStatement: %v", err)
			}
			diff := backendconformance.DifferentialDifference{
				Fingerprint: fingerprint,
				Kind:        backendconformance.DivergenceMissing,
				Detail:      "recorded on one backend only",
			}
			if !anyEntryMatches(allow, diff) {
				t.Fatalf("no allowlist entry excuses the %s path-conflict cleanup as a missing divergence; refresh specs/backend-divergence-allowlist.v1.yaml", tc.name)
			}
		})
	}
}

// TestAllowlistDoesNotExcuseRepositoryCleanupBeyondMissing is the seeded
// violation: the cleanup entries excuse only the one-sided recording. A
// results or failures divergence on the same statement must still fail, and
// so must a cleanup text that drifts from the production statement (a
// dropped id guard would delete the live node).
func TestAllowlistDoesNotExcuseRepositoryCleanupBeyondMissing(t *testing.T) {
	t.Parallel()
	allow := loadBackendDivergenceAllowlist(t)
	stmt := repositoryCleanupStatement(t, true)
	fingerprint, err := backendconformance.FingerprintStatement(stmt.Cypher, stmt.Parameters)
	if err != nil {
		t.Fatalf("FingerprintStatement: %v", err)
	}
	for _, kind := range []string{backendconformance.DivergenceResults, backendconformance.DivergenceFailures} {
		if anyEntryMatches(allow, backendconformance.DifferentialDifference{Fingerprint: fingerprint, Kind: kind}) {
			t.Fatalf("%s divergence on the cleanup statement was excused; entries must cover only the missing kind", kind)
		}
	}
	drifted := fingerprint
	drifted.Statement = strings.Replace(drifted.Statement, "r.id <> $repo_id", "r.id = $repo_id", 1)
	if drifted.Statement == fingerprint.Statement {
		t.Fatal("seeded drift did not change the statement")
	}
	if anyEntryMatches(allow, backendconformance.DifferentialDifference{Fingerprint: drifted, Kind: backendconformance.DivergenceMissing}) {
		t.Fatal("a cleanup statement with a changed id guard was excused")
	}
}

// anyEntryMatches reports whether any allowlist entry excuses diff. It is
// entry-level on purpose: Excuse and Validate also fail the whole file's
// stale entries, which a single-read pin cannot satisfy.
func anyEntryMatches(allow *Allowlist, diff backendconformance.DifferentialDifference) bool {
	for _, entry := range allow.entries {
		if entryMatches(entry, diff) {
			return true
		}
	}
	return false
}
