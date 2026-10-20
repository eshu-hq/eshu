// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"strings"
	"testing"
)

// TestBuildRetirementFileRetractStatement pins the G5f shape: a
// repo-anchored, evidence-scoped File DETACH DELETE that also fires when
// the Repository node is already gone (it never traverses from it). The
// statement must stay replay-safe: the runner replays a cursor step whose
// commit never persisted.
func TestBuildRetirementFileRetractStatement(t *testing.T) {
	t.Parallel()

	stmt, err := BuildRetirementFileRetractStatement("repo-retired")
	if err != nil {
		t.Fatalf("BuildRetirementFileRetractStatement: %v", err)
	}
	if stmt.Operation != OperationCanonicalRetract {
		t.Fatalf("Operation = %q, want %q", stmt.Operation, OperationCanonicalRetract)
	}
	for _, want := range []string{
		"MATCH (f:File)",
		"f.repo_id = $repo_id",
		"f.evidence_source = $evidence_source",
		"DETACH DELETE f",
	} {
		if !strings.Contains(stmt.Cypher, want) {
			t.Errorf("G5f Cypher missing %q:\n%s", want, stmt.Cypher)
		}
	}
	if stmt.Parameters["evidence_source"] != "projector/canonical" {
		t.Errorf("evidence_source = %v, want projector/canonical", stmt.Parameters["evidence_source"])
	}
	if strings.Contains(stmt.Cypher, "Repository") {
		t.Errorf("G5f Cypher must not anchor on the Repository node:\n%s", stmt.Cypher)
	}
	if stmt.Parameters["repo_id"] != "repo-retired" {
		t.Errorf("repo_id = %v, want repo-retired", stmt.Parameters["repo_id"])
	}
	if !isIdempotentRetractStatement(stmt) {
		t.Errorf("G5f must stay replay-safe (param-bound MATCH + DELETE only):\n%s", stmt.Cypher)
	}

	if _, err := BuildRetirementFileRetractStatement(""); err == nil {
		t.Error("empty repo_id must fail closed, not build a predicate that matches nothing by luck")
	}
}

// TestBuildRetirementParameterRetractStatement pins the G5p shape: a
// root-prefix Parameter DETACH DELETE that spares nested checkouts. The
// NOT term makes it non-replay-safe by construction (the same documented
// shape as the existing generation-scoped retracts), so the runner owns
// the retry through its cursor, never the group replay.
func TestBuildRetirementParameterRetractStatement(t *testing.T) {
	t.Parallel()

	stmt, err := BuildRetirementParameterRetractStatement("/repo/retired", []string{"/repo/retired/nested/"})
	if err != nil {
		t.Fatalf("BuildRetirementParameterRetractStatement: %v", err)
	}
	if stmt.Operation != OperationCanonicalRetract {
		t.Fatalf("Operation = %q, want %q", stmt.Operation, OperationCanonicalRetract)
	}
	for _, want := range []string{
		"MATCH (p:Parameter)",
		"p.path STARTS WITH $root_prefix",
		"p.evidence_source = 'projector/canonical'",
		"NOT any(",
		"DETACH DELETE p",
	} {
		if !strings.Contains(stmt.Cypher, want) {
			t.Errorf("G5p Cypher missing %q:\n%s", want, stmt.Cypher)
		}
	}
	if stmt.Parameters["root_prefix"] != "/repo/retired/" {
		t.Errorf("root_prefix = %v, want /repo/retired/ (trailing slash)", stmt.Parameters["root_prefix"])
	}
	nested, ok := stmt.Parameters["nested_root_prefixes"].([]string)
	if !ok || len(nested) != 1 || nested[0] != "/repo/retired/nested/" {
		t.Errorf("nested_root_prefixes = %v, want the nested root", stmt.Parameters["nested_root_prefixes"])
	}
	if isIdempotentRetractStatement(stmt) {
		t.Errorf("G5p must NOT classify replay-safe (NOT term): the runner cursor owns its retry:\n%s", stmt.Cypher)
	}

	if _, err := BuildRetirementParameterRetractStatement("", nil); err == nil {
		t.Error("empty root must fail closed: a bare prefix would match every Parameter")
	}
	if _, err := BuildRetirementParameterRetractStatement("  ", nil); err == nil {
		t.Error("blank root must fail closed")
	}
}

// TestRetirementNestedPrefixesNormalized pins the P1 slash rule: an
// un-slashed nested root ("/r/shared") must normalize to "/r/shared/",
// or STARTS WITH would also match the sibling "/r/shared-backup/x.go"
// and spare a retired Parameter invisibly to the census (which shares
// the list). Both the G5p builder and the census arm must agree.
func TestRetirementNestedPrefixesNormalized(t *testing.T) {
	t.Parallel()

	stmt, err := BuildRetirementParameterRetractStatement("/r/retired", []string{"/r/shared", "  ", "/r/kept/"})
	if err != nil {
		t.Fatalf("BuildRetirementParameterRetractStatement: %v", err)
	}
	nested, ok := stmt.Parameters["nested_root_prefixes"].([]string)
	if !ok || len(nested) != 2 || nested[0] != "/r/shared/" || nested[1] != "/r/kept/" {
		t.Fatalf("nested_root_prefixes = %v, want [/r/shared/ /r/kept/]", stmt.Parameters["nested_root_prefixes"])
	}

	checks, err := BuildRetirementResidueCensus("repo-retired", "/r/retired", []string{"/r/shared"}, true)
	if err != nil {
		t.Fatalf("BuildRetirementResidueCensus: %v", err)
	}
	for _, c := range checks {
		if c.Name != "parameter_residue" {
			continue
		}
		got, ok := c.Parameters["nested_root_prefixes"].([]string)
		if !ok || len(got) != 1 || got[0] != "/r/shared/" {
			t.Fatalf("census nested_root_prefixes = %v, want [/r/shared/]", c.Parameters["nested_root_prefixes"])
		}
		return
	}
	t.Fatal("parameter_residue arm missing with includeParameters=true")
}

// TestBuildRetirementRepositoryDemoteStatement pins the G6 shape: the
// Repository node is demoted, never deleted. Clearing path keeps the node
// out of the path-conflict cleanup; clearing scope and generation ids
// detaches it from liveness reads; incoming edges owned by other
// repositories stay until their owners retract them (contract A).
func TestBuildRetirementRepositoryDemoteStatement(t *testing.T) {
	t.Parallel()

	stmt, err := BuildRetirementRepositoryDemoteStatement("repo-retired", "rr_1")
	if err != nil {
		t.Fatalf("BuildRetirementRepositoryDemoteStatement: %v", err)
	}
	if stmt.Operation != OperationCanonicalRetract {
		t.Fatalf("Operation = %q, want %q", stmt.Operation, OperationCanonicalRetract)
	}
	for _, want := range []string{
		"MATCH (r:Repository {id: $repo_id})",
		"r.evidence_source = 'projector/canonical'",
		"r.evidence_source = 'retirement/repository'",
		"r.path = null",
		"r.local_path = null",
		"r.scope_id = null",
		"r.generation_id = $sentinel_generation_id",
	} {
		if !strings.Contains(stmt.Cypher, want) {
			t.Errorf("G6 Cypher missing %q:\n%s", want, stmt.Cypher)
		}
	}
	for _, forbidden := range []string{"DELETE", "REMOVE", "DETACH"} {
		if strings.Contains(stmt.Cypher, forbidden) {
			t.Errorf("G6 Cypher must not delete or remove anything (found %q):\n%s", forbidden, stmt.Cypher)
		}
	}
	if stmt.Parameters["sentinel_generation_id"] != "retired:rr_1" {
		t.Errorf("sentinel_generation_id = %v, want retired:rr_1", stmt.Parameters["sentinel_generation_id"])
	}

	if _, err := BuildRetirementRepositoryDemoteStatement("", "rr_1"); err == nil {
		t.Error("empty repo_id must fail closed")
	}
	if _, err := BuildRetirementRepositoryDemoteStatement("repo-retired", ""); err == nil {
		t.Error("empty retirement id must fail closed")
	}
}

// TestBuildRetirementResidueCensusPinsAnchoredArms pins the 2z runtime
// census: every arm seeks an index (P10'), and the Parameter arm is
// omitted exactly when the root is shared (the live repository's
// Parameters survive the skip).
func TestBuildRetirementResidueCensusPinsAnchoredArms(t *testing.T) {
	t.Parallel()

	checks, err := BuildRetirementResidueCensus("repo-retired", "/repo/retired", nil, true)
	if err != nil {
		t.Fatalf("BuildRetirementResidueCensus: %v", err)
	}
	names := make([]string, 0, len(checks))
	for _, check := range checks {
		names = append(names, check.Name)
		if strings.Contains(check.Cypher, "NodeByLabelScan") {
			t.Errorf("arm %s carries a plan operator, not Cypher", check.Name)
		}
	}
	for _, want := range []string{"file_residue", "directory_residue", "function_residue", "parameter_residue", "repository_outgoing_edges", "workload_residue", "workload_instance_residue", "endpoint_residue"} {
		found := false
		for _, name := range names {
			if name == want {
				found = true
			}
		}
		if !found {
			t.Errorf("census arms %v miss %q", names, want)
		}
	}

	shared, err := BuildRetirementResidueCensus("repo-retired", "/repo/retired", []string{"/repo/live/"}, false)
	if err != nil {
		t.Fatalf("BuildRetirementResidueCensus shared root: %v", err)
	}
	for _, check := range shared {
		if check.Name == "parameter_residue" {
			t.Error("shared root must omit the Parameter arm (live owner's Parameters survive)")
		}
	}

	if _, err := BuildRetirementResidueCensus("", "/repo/retired", nil, true); err == nil {
		t.Error("empty repo_id must fail closed")
	}
	if _, err := BuildRetirementResidueCensus("repo-retired", "", nil, true); err == nil {
		t.Error("empty root must fail closed when Parameters are included")
	}
}

// TestBuildRetirementWorkloadRetractStatements pins the G5w shape: the
// retired repository's Workload, WorkloadInstance, and Endpoint nodes go
// by repo_id anchor (their indexes seek it), with DETACH DELETE so the
// DEFINES, EXPOSES_ENDPOINT, USES, and DEPLOYMENT_SOURCE edges die with
// the nodes. No evidence filter: any node carrying the retired repo_id is
// residue, whichever writer stamped it.
func TestBuildRetirementWorkloadRetractStatements(t *testing.T) {
	t.Parallel()

	stmts, err := BuildRetirementWorkloadRetractStatements("repo-retired")
	if err != nil {
		t.Fatalf("BuildRetirementWorkloadRetractStatements: %v", err)
	}
	if len(stmts) != 3 {
		t.Fatalf("len(statements) = %d, want 3 (Workload, WorkloadInstance, Endpoint)", len(stmts))
	}
	for _, want := range []string{"(w:Workload)", "(i:WorkloadInstance)", "(e:Endpoint)"} {
		found := false
		for _, stmt := range stmts {
			if strings.Contains(stmt.Cypher, want) {
				found = true
				if stmt.Operation != OperationCanonicalRetract {
					t.Errorf("Operation = %q, want %q", stmt.Operation, OperationCanonicalRetract)
				}
				if !strings.Contains(stmt.Cypher, "repo_id = $repo_id") {
					t.Errorf("statement missing repo_id anchor:\n%s", stmt.Cypher)
				}
				if !strings.Contains(stmt.Cypher, "DETACH DELETE") {
					t.Errorf("statement missing DETACH DELETE:\n%s", stmt.Cypher)
				}
				if !isIdempotentRetractStatement(stmt) {
					t.Errorf("G5w statement must stay replay-safe:\n%s", stmt.Cypher)
				}
			}
		}
		if !found {
			t.Errorf("no statement matches %q", want)
		}
	}

	if _, err := BuildRetirementWorkloadRetractStatements(""); err == nil {
		t.Error("empty repo_id must fail closed")
	}
	if _, err := BuildRetirementWorkloadRetractStatements("  "); err == nil {
		t.Error("blank repo_id must fail closed")
	}
}

// TestRetirementSharedRootQueryCoversEqualAndParentPaths pins both arms of
// the shared-root read. Live coverage uses the parent shape only: two
// Repository nodes can never carry the same path under the repository_path
// uniqueness constraint, so the equal-path arm has no live shape and this
// pin is its only guard against a future edit dropping it.
func TestRetirementSharedRootQueryCoversEqualAndParentPaths(t *testing.T) {
	t.Parallel()

	cypher, params := RetirementSharedRootQuery("repo-retired", "/root/child")
	if !strings.Contains(cypher, "$root = o.path") {
		t.Errorf("shared-root query missing the equal-path arm:\n%s", cypher)
	}
	if !strings.Contains(cypher, "$root STARTS WITH o.path + '/'") {
		t.Errorf("shared-root query missing the parent-path arm:\n%s", cypher)
	}
	if params["repo_id"] != "repo-retired" || params["root"] != "/root/child" {
		t.Errorf("shared-root params = %v, want repo_id and root carried through", params)
	}
}
