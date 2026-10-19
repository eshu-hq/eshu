// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/cypher/canonical/node"
)

// T4 (issue #5385, migration doc section 2): re-onboarding a repository's
// path under a new repo_id must delete the retired identity's owned
// Workload, WorkloadInstance, and Endpoint nodes. The deletes are scoped by
// label and repo_id, never by node id: an edge escaping an id-anchored
// delete stays forever. The three cascade statements run before the
// path-conflict Repository retirement (which makes the retired r.id
// unrecoverable), and each re-derives the retired ids from the same
// path anchor + id guard, so no statement feeds rows to the next.

var retirementCascadeLabels = []string{"Workload", "WorkloadInstance", "Endpoint"}

func TestRetirementCascadeDeletesOwnedLabelsBeforePathCleanup(t *testing.T) {
	t.Parallel()
	stmts := NewCanonicalNodeWriter(nil, 500, nil).buildRepositoryCleanupStatements(retirementMaterialization(false, false))
	if len(stmts) != 4 {
		t.Fatalf("cleanup statements = %d, want 4 (3 cascades + path conflict)", len(stmts))
	}
	for i, label := range retirementCascadeLabels {
		stmt := stmts[i]
		if stmt.Operation != OperationCanonicalRetract {
			t.Fatalf("cascade %d operation = %q, want %q", i, stmt.Operation, OperationCanonicalRetract)
		}
		if !strings.Contains(stmt.Cypher, "MATCH (r:Repository {path: $path})") {
			t.Fatalf("cascade %s missing the retired-repository path anchor:\n%s", label, stmt.Cypher)
		}
		if !strings.Contains(stmt.Cypher, "WHERE r.id <> $repo_id") {
			t.Fatalf("cascade %s missing the live-identity guard:\n%s", label, stmt.Cypher)
		}
		if want := "MATCH (n:" + label + " {repo_id: r.id})"; !strings.Contains(stmt.Cypher, want) {
			t.Fatalf("cascade %s missing the repo-scoped label match %q:\n%s", label, want, stmt.Cypher)
		}
		if !strings.Contains(stmt.Cypher, "DETACH DELETE n") {
			t.Fatalf("cascade %s does not DETACH DELETE the owned nodes:\n%s", label, stmt.Cypher)
		}
		if strings.Contains(stmt.Cypher, "{id:") || strings.Contains(stmt.Cypher, "{id :") {
			t.Fatalf("cascade %s is id-anchored, an escaped edge would stay forever:\n%s", label, stmt.Cypher)
		}
		if got := stmt.Parameters["repo_id"]; got != "repository:r_new" {
			t.Fatalf("cascade %s repo_id = %#v, want repository:r_new", label, got)
		}
		if got := stmt.Parameters["path"]; got != "/repos/service" {
			t.Fatalf("cascade %s path = %#v, want /repos/service", label, got)
		}
		if got := stmt.Parameters[StatementMetadataSummaryKey]; got != "repository_cleanup lookup=path_conflict cascade="+label {
			t.Fatalf("cascade %s summary = %#v, want the per-leg summary", label, got)
		}
	}
	last := stmts[3]
	if !IsRepositoryPathCleanupCypher(last.Cypher) && last.Cypher != node.RepositoryPathCleanupBareCypher {
		t.Fatalf("statement 4 = %q, want the path-conflict retirement last", last.Cypher)
	}
}

// The cascade is retirement-scoped, never regeneration-scoped: a first
// generation, a delta projection, or a missing repository emits nothing, so
// re-projection of the live identity keeps every owned node.
func TestRetirementCascadeSkippedWithoutRetirement(t *testing.T) {
	t.Parallel()
	if stmts := NewCanonicalNodeWriter(nil, 500, nil).buildRepositoryCleanupStatements(retirementMaterialization(true, false)); stmts != nil {
		t.Fatalf("first-generation cleanup = %d statements, want nil", len(stmts))
	}
	if stmts := NewCanonicalNodeWriter(nil, 500, nil).buildRepositoryCleanupStatements(retirementMaterialization(false, true)); stmts != nil {
		t.Fatalf("delta cleanup = %d statements, want nil", len(stmts))
	}
}

// The cascade carries no OPTIONAL MATCH + WITH collect composition, so the
// #8173 NornicDB defect it trips on does not apply: the bare writer emits
// the same three cascade statements, and only the path-conflict tail
// differs by backend.
func TestRetirementCascadeIdenticalAcrossBackends(t *testing.T) {
	t.Parallel()
	mat := bareRetirementMaterialization()
	neo4j := NewCanonicalNodeWriter(nil, 500, nil).buildRepositoryCleanupStatements(mat)
	nornic := NewCanonicalNodeWriter(nil, 500, nil).WithBareRepositoryRetirement().buildRepositoryCleanupStatements(mat)
	if len(neo4j) != 4 || len(nornic) != 4 {
		t.Fatalf("cleanup statements neo4j=%d nornicdb=%d, want 4 each", len(neo4j), len(nornic))
	}
	for i := range retirementCascadeLabels {
		if neo4j[i].Cypher != nornic[i].Cypher {
			t.Fatalf("cascade %d differs by backend:\nneo4j=%q\nnornicdb=%q", i, neo4j[i].Cypher, nornic[i].Cypher)
		}
	}
}
