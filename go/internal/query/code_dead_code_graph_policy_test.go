// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"strings"
	"testing"
)

func TestBuildDeadCodeGraphCypherKeepsCandidateReadSimple(t *testing.T) {
	t.Parallel()

	cypher := buildDeadCodeGraphCypher(true, GraphBackendNornicDB)
	for _, want := range []string{
		"MATCH (r:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File)-[:CONTAINS]->(e:Function)",
		"ORDER BY f.relative_path, e.name, coalesce(e.uid, e.id)",
		"SKIP $skip",
		"LIMIT $limit",
	} {
		if !strings.Contains(cypher, want) {
			t.Fatalf("dead-code cypher missing %q:\n%s", want, cypher)
		}
	}
	for _, notWant := range []string{
		"NOT EXISTS { MATCH (e)<-[:CALLS|IMPORTS|REFERENCES|INHERITS]-() }",
		"NOT ()-[:CALLS|IMPORTS|REFERENCES|INHERITS]->(e)",
		"toLower(f.relative_path)",
		"coalesce(e.enclosing_function, '')",
	} {
		if strings.Contains(cypher, notWant) {
			t.Fatalf("dead-code cypher contains app-layer policy or reachability predicate %q:\n%s", notWant, cypher)
		}
	}
}

// TestBuildDeadCodeGraphCypherKeepsTheScopedVariantSimple runs the same
// NornicDB-safety policy over the shape a scoped caller actually gets.
// The test-only buildDeadCodeGraphCypher helper hard-codes an unscoped filter,
// so the test above never sees the grant predicate; this one adds it and checks
// that it lands in the MATCH-attached WHERE with no extra clause between the
// anchor and the RETURN, which is the shape the pinned build evaluates
// faithfully.
func TestBuildDeadCodeGraphCypherKeepsTheScopedVariantSimple(t *testing.T) {
	t.Parallel()

	access := repositoryAccessFilter{AllowedRepositoryIDs: []string{"repo://tenant-a/granted-service"}}
	for _, hasRepoID := range []bool{true, false} {
		cypher := buildDeadCodeGraphCypherForLabel(hasRepoID, "Function", "go", access)
		grant := "(r.id IN $allowed_repository_ids OR r.id IN $allowed_scope_ids)"
		if !strings.Contains(cypher, grant) {
			t.Fatalf("scoped dead-code cypher (has_repo_id=%t) is missing %q:\n%s", hasRepoID, grant, cypher)
		}
		where := strings.Index(cypher, "WHERE ")
		ret := strings.Index(cypher, "RETURN ")
		if where < 0 || ret < 0 || where > ret {
			t.Fatalf("scoped dead-code cypher (has_repo_id=%t) has no WHERE before its RETURN:\n%s", hasRepoID, cypher)
		}
		if strings.Index(cypher, grant) > ret {
			t.Fatalf("the grant predicate sits after the RETURN (has_repo_id=%t):\n%s", hasRepoID, cypher)
		}
		for _, notWant := range []string{"OPTIONAL MATCH", "WITH "} {
			if strings.Contains(cypher[:ret], notWant) {
				t.Fatalf("scoped dead-code cypher (has_repo_id=%t) puts %q between the anchor and the RETURN:\n%s", hasRepoID, notWant, cypher)
			}
		}
	}
}

func TestBuildDeadCodeIncomingProbeCypherUsesBatchedExactEntityLookup(t *testing.T) {
	t.Parallel()

	cypher := buildDeadCodeIncomingBatchProbeCypher("Function")
	for _, want := range []string{
		"UNWIND $entity_ids AS entity_id",
		"MATCH (e:Function {uid: entity_id})<-[rel:CALLS|IMPORTS|REFERENCES|INHERITS|EXECUTES]-(source)",
		"RETURN DISTINCT coalesce(e.uid, e.id) as incoming_entity_id",
		"rel.resolution_method as resolution_method",
	} {
		if !strings.Contains(cypher, want) {
			t.Fatalf("incoming-edge cypher missing %q:\n%s", want, cypher)
		}
	}
	if strings.Contains(cypher, "{uid: $entity_id}") {
		t.Fatalf("incoming-edge probe should use batched entity ids in the lookup:\n%s", cypher)
	}
	if strings.Contains(cypher, "Repository") {
		t.Fatalf("incoming-edge probe should not fan out through repository scope:\n%s", cypher)
	}
}

func TestDeadCodeCandidateScanLimitUsesFullWindowForSmallDisplayLimits(t *testing.T) {
	t.Parallel()

	if got, want := deadCodeCandidateScanLimit(50), 2500; got != want {
		t.Fatalf("deadCodeCandidateScanLimit(50) = %d, want %d", got, want)
	}
}
