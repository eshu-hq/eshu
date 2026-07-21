// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

var errTerraformStateConfigMatchResolverFixtureFailure = errors.New("fixture: config-match resolver query failed")

// fakeTerraformStateOwnershipResolver counts calls per (backend_kind,
// locator_hash) pair so tests can assert the resolver is memoized within one
// batch, not called once per row.
type fakeTerraformStateOwnershipResolver struct {
	calls   map[[2]string]int
	answers map[[2]string]string // present key => resolved repo id; absent => not ok
}

func newFakeTerraformStateOwnershipResolver() *fakeTerraformStateOwnershipResolver {
	return &fakeTerraformStateOwnershipResolver{
		calls:   map[[2]string]int{},
		answers: map[[2]string]string{},
	}
}

func (f *fakeTerraformStateOwnershipResolver) ResolveOwningRepoID(_ context.Context, backendKind, locatorHash string) (string, bool) {
	key := [2]string{backendKind, locatorHash}
	f.calls[key]++
	repoID, ok := f.answers[key]
	return repoID, ok
}

func TestResolveTerraformStateOwnershipMemoizesPerBackendLocatorPair(t *testing.T) {
	t.Parallel()

	resolver := newFakeTerraformStateOwnershipResolver()
	resolver.answers[[2]string{"s3", "locator-a"}] = "repo-a"

	writer := NewCanonicalNodeWriter(&recordingExecutor{}, 500, nil).WithTerraformStateOwnershipResolver(resolver)
	rows := []projector.TerraformStateResourceRow{
		{UID: "uid-1", Address: "aws_instance.web", BackendKind: "s3", LocatorHash: "locator-a"},
		{UID: "uid-2", Address: "aws_instance.api", BackendKind: "s3", LocatorHash: "locator-a"},
		{UID: "uid-3", Address: "aws_instance.db", BackendKind: "gcs", LocatorHash: "locator-b"},
	}

	out := writer.resolveTerraformStateOwnership(context.Background(), rows)

	if got, want := resolver.calls[[2]string{"s3", "locator-a"}], 1; got != want {
		t.Fatalf("resolver calls for (s3, locator-a) = %d, want %d (memoized across 2 rows)", got, want)
	}
	if got, want := resolver.calls[[2]string{"gcs", "locator-b"}], 1; got != want {
		t.Fatalf("resolver calls for (gcs, locator-b) = %d, want %d", got, want)
	}
	if got, want := out[0].OwningRepoID, "repo-a"; got != want {
		t.Fatalf("out[0].OwningRepoID = %q, want %q", got, want)
	}
	if got, want := out[1].OwningRepoID, "repo-a"; got != want {
		t.Fatalf("out[1].OwningRepoID = %q, want %q", got, want)
	}
	if got, want := out[2].OwningRepoID, ""; got != want {
		t.Fatalf("out[2].OwningRepoID = %q, want %q (unresolved backend stays empty)", got, want)
	}
}

func TestResolveTerraformStateOwnershipNilResolverLeavesRowsUnchanged(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&recordingExecutor{}, 500, nil)
	rows := []projector.TerraformStateResourceRow{
		{UID: "uid-1", Address: "aws_instance.web", BackendKind: "s3", LocatorHash: "locator-a"},
	}

	out := writer.resolveTerraformStateOwnership(context.Background(), rows)
	if got, want := out[0].OwningRepoID, ""; got != want {
		t.Fatalf("OwningRepoID = %q, want %q (no resolver wired)", got, want)
	}
}

func TestResolveTerraformStateOwnershipSkipsBlankBackendIdentity(t *testing.T) {
	t.Parallel()

	resolver := newFakeTerraformStateOwnershipResolver()
	writer := NewCanonicalNodeWriter(&recordingExecutor{}, 500, nil).WithTerraformStateOwnershipResolver(resolver)
	rows := []projector.TerraformStateResourceRow{
		{UID: "uid-1", Address: "aws_instance.web", BackendKind: "", LocatorHash: ""},
	}

	out := writer.resolveTerraformStateOwnership(context.Background(), rows)
	if got, want := out[0].OwningRepoID, ""; got != want {
		t.Fatalf("OwningRepoID = %q, want %q (blank backend identity never calls the resolver)", got, want)
	}
	if len(resolver.calls) != 0 {
		t.Fatalf("resolver was called %d times for a blank backend identity, want 0", len(resolver.calls))
	}
}

func TestTerraformStateMatchesConfigEdgeStatementsOnlyIncludesResolvedRows(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&recordingExecutor{}, 500, nil)
	mat := projector.CanonicalMaterialization{
		ScopeID:      "tf-scope-edge",
		GenerationID: "tf-generation-edge",
		TerraformStateResources: []projector.TerraformStateResourceRow{
			{UID: "uid-matched", Address: "aws_instance.web", OwningRepoID: "repo-a"},
			{UID: "uid-unresolved", Address: "aws_instance.orphan", OwningRepoID: ""},
			{UID: "uid-no-address", Address: "", OwningRepoID: "repo-a"},
		},
	}

	statements := writer.terraformStateMatchesConfigEdgeStatements(mat)
	if len(statements) != 1 {
		t.Fatalf("terraformStateMatchesConfigEdgeStatements() count = %d, want 1", len(statements))
	}

	stmt := statements[0]
	if !strings.Contains(stmt.Cypher, "MATCH (c:TerraformResource {repo_id: row.owning_repo_id, name: row.address})") {
		t.Fatalf("edge Cypher = %q, want a repo_id+name anchored config match", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "MATCH (s:TerraformStateResource {uid: row.uid})") {
		t.Fatalf("edge Cypher = %q, want a uid anchored state match", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "MERGE (c)-[e:MATCHES_STATE]->(s)") {
		t.Fatalf("edge Cypher = %q, want a MATCHES_STATE edge merge", stmt.Cypher)
	}

	rows := stmt.Parameters["rows"].([]map[string]any)
	if len(rows) != 1 {
		t.Fatalf("edge rows = %d, want 1 (only the resolved, addressed row)", len(rows))
	}
	if got, want := rows[0]["uid"], "uid-matched"; got != want {
		t.Fatalf("rows[0][uid] = %#v, want %q", got, want)
	}
	if got, want := rows[0]["owning_repo_id"], "repo-a"; got != want {
		t.Fatalf("rows[0][owning_repo_id] = %#v, want %q", got, want)
	}
}

func TestTerraformStateMatchesConfigEdgeStatementsEmptyWhenNoneResolved(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&recordingExecutor{}, 500, nil)
	mat := projector.CanonicalMaterialization{
		TerraformStateResources: []projector.TerraformStateResourceRow{
			{UID: "uid-1", Address: "aws_instance.web", OwningRepoID: ""},
		},
	}

	if got := writer.terraformStateMatchesConfigEdgeStatements(mat); len(got) != 0 {
		t.Fatalf("terraformStateMatchesConfigEdgeStatements() count = %d, want 0", len(got))
	}
}

func TestTerraformStateOwningRepoIDValueConvertsEmptyToNil(t *testing.T) {
	t.Parallel()

	if got := terraformStateOwningRepoIDValue(""); got != nil {
		t.Fatalf("terraformStateOwningRepoIDValue(\"\") = %#v, want nil", got)
	}
	if got, want := terraformStateOwningRepoIDValue("repo-a"), "repo-a"; got != want {
		t.Fatalf("terraformStateOwningRepoIDValue(%q) = %#v, want %q", "repo-a", got, want)
	}
}

// TestTerraformStateMatchesConfigEdgeStatementsSkipsAmbiguousRows proves the
// P1 review fix: a resolved-and-addressed row flagged ConfigMatchAmbiguous
// (2+ TerraformResource nodes share its (repo_id, name) pair -- no
// uniqueness constraint backs that pair) must be excluded from the
// MATCHES_STATE edge write, not silently fanned out to every candidate. This
// must fail against pre-fix HEAD (ConfigMatchAmbiguous does not exist / is
// not checked, so the ambiguous row would be included).
func TestTerraformStateMatchesConfigEdgeStatementsSkipsAmbiguousRows(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&recordingExecutor{}, 500, nil)
	mat := projector.CanonicalMaterialization{
		ScopeID:      "tf-scope-ambiguous",
		GenerationID: "tf-generation-ambiguous",
		TerraformStateResources: []projector.TerraformStateResourceRow{
			{UID: "uid-unambiguous", Address: "aws_instance.web", OwningRepoID: "repo-a", ConfigMatchAmbiguous: false},
			{UID: "uid-ambiguous", Address: "aws_instance.shared", OwningRepoID: "repo-a", ConfigMatchAmbiguous: true},
		},
	}

	statements := writer.terraformStateMatchesConfigEdgeStatements(mat)
	if len(statements) != 1 {
		t.Fatalf("terraformStateMatchesConfigEdgeStatements() count = %d, want 1", len(statements))
	}
	rows := statements[0].Parameters["rows"].([]map[string]any)
	if len(rows) != 1 {
		t.Fatalf("edge rows = %d, want 1 (only the unambiguous row)", len(rows))
	}
	if got, want := rows[0]["uid"], "uid-unambiguous"; got != want {
		t.Fatalf("rows[0][uid] = %#v, want %q (the ambiguous row must never reach the edge write)", got, want)
	}
}

// TestTerraformStateMatchesConfigEdgeStatementsAllAmbiguousYieldsNoStatement
// covers the total-ambiguity case: every resolved row is ambiguous, so no
// MATCHES_STATE statement should be built at all (matching
// TestTerraformStateMatchesConfigEdgeStatementsEmptyWhenNoneResolved's
// existing precedent for the "nothing to write" case).
func TestTerraformStateMatchesConfigEdgeStatementsAllAmbiguousYieldsNoStatement(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&recordingExecutor{}, 500, nil)
	mat := projector.CanonicalMaterialization{
		TerraformStateResources: []projector.TerraformStateResourceRow{
			{UID: "uid-1", Address: "aws_instance.shared", OwningRepoID: "repo-a", ConfigMatchAmbiguous: true},
		},
	}

	if got := writer.terraformStateMatchesConfigEdgeStatements(mat); len(got) != 0 {
		t.Fatalf("terraformStateMatchesConfigEdgeStatements() count = %d, want 0", len(got))
	}
}

// fakeTerraformStateConfigMatchResolver is an in-memory
// TerraformStateConfigMatchResolver for testing
// resolveTerraformStateConfigMatchAmbiguity without a real graph backend.
type fakeTerraformStateConfigMatchResolver struct {
	calls  int
	counts map[string]int // UID -> candidate count
	err    error
}

func (f *fakeTerraformStateConfigMatchResolver) CountConfigMatchCandidates(
	_ context.Context,
	queries []TerraformStateConfigMatchQuery,
) (map[string]int, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	out := make(map[string]int, len(queries))
	for _, q := range queries {
		if count, ok := f.counts[q.UID]; ok {
			out[q.UID] = count
		}
	}
	return out, nil
}

// TestResolveTerraformStateConfigMatchAmbiguityFlagsMultiCandidateRows proves
// resolveTerraformStateConfigMatchAmbiguity calls the resolver exactly once
// per batch (not once per row) and flags ConfigMatchAmbiguous only for rows
// whose resolved candidate count is not exactly 1.
func TestResolveTerraformStateConfigMatchAmbiguityFlagsMultiCandidateRows(t *testing.T) {
	t.Parallel()

	resolver := &fakeTerraformStateConfigMatchResolver{
		counts: map[string]int{
			"uid-unique":    1,
			"uid-ambiguous": 2,
			"uid-absent":    0,
		},
	}
	writer := NewCanonicalNodeWriter(&recordingExecutor{}, 500, nil).WithTerraformStateConfigMatchResolver(resolver)
	rows := []projector.TerraformStateResourceRow{
		{UID: "uid-unique", Address: "aws_instance.web", OwningRepoID: "repo-a"},
		{UID: "uid-ambiguous", Address: "aws_instance.shared", OwningRepoID: "repo-a"},
		{UID: "uid-absent", Address: "aws_instance.gone", OwningRepoID: "repo-a"},
		{UID: "uid-unresolved-owner", Address: "aws_instance.orphan", OwningRepoID: ""},
	}

	out := writer.resolveTerraformStateConfigMatchAmbiguity(context.Background(), rows)

	if resolver.calls != 1 {
		t.Fatalf("resolver called %d times, want 1 (single batch call)", resolver.calls)
	}
	if got := out[0].ConfigMatchAmbiguous; got {
		t.Fatalf("uid-unique ConfigMatchAmbiguous = %v, want false", got)
	}
	if got := out[1].ConfigMatchAmbiguous; !got {
		t.Fatalf("uid-ambiguous ConfigMatchAmbiguous = %v, want true", got)
	}
	if got := out[2].ConfigMatchAmbiguous; got {
		t.Fatalf("uid-absent ConfigMatchAmbiguous = %v, want false (zero candidates is not ambiguous, just absent)", got)
	}
	if got := out[3].ConfigMatchAmbiguous; got {
		t.Fatalf("uid-unresolved-owner ConfigMatchAmbiguous = %v, want false (never queried: no OwningRepoID)", got)
	}
}

// TestResolveTerraformStateConfigMatchAmbiguityNilResolverLeavesRowsUnchanged
// mirrors TestResolveTerraformStateOwnershipNilResolverLeavesRowsUnchanged:
// with no resolver wired, rows pass through with ConfigMatchAmbiguous left
// at its zero value, matching every unit test in this package that
// constructs rows directly without exercising resolver wiring. Production
// wiring (cmd/projector) always wires a real resolver.
func TestResolveTerraformStateConfigMatchAmbiguityNilResolverLeavesRowsUnchanged(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&recordingExecutor{}, 500, nil)
	rows := []projector.TerraformStateResourceRow{
		{UID: "uid-1", Address: "aws_instance.web", OwningRepoID: "repo-a"},
	}

	out := writer.resolveTerraformStateConfigMatchAmbiguity(context.Background(), rows)
	if got := out[0].ConfigMatchAmbiguous; got {
		t.Fatalf("ConfigMatchAmbiguous = %v, want false (no resolver wired)", got)
	}
}

// TestResolveTerraformStateConfigMatchAmbiguityResolverErrorFailsClosed
// proves the fail-closed contract on resolver failure: a query error must
// flag every queried row ambiguous rather than let a resolver outage risk a
// silent wrong-edge write.
func TestResolveTerraformStateConfigMatchAmbiguityResolverErrorFailsClosed(t *testing.T) {
	t.Parallel()

	resolver := &fakeTerraformStateConfigMatchResolver{err: errTerraformStateConfigMatchResolverFixtureFailure}
	writer := NewCanonicalNodeWriter(&recordingExecutor{}, 500, nil).WithTerraformStateConfigMatchResolver(resolver)
	rows := []projector.TerraformStateResourceRow{
		{UID: "uid-1", Address: "aws_instance.web", OwningRepoID: "repo-a"},
	}

	out := writer.resolveTerraformStateConfigMatchAmbiguity(context.Background(), rows)
	if got := out[0].ConfigMatchAmbiguous; !got {
		t.Fatalf("ConfigMatchAmbiguous = %v, want true (resolver error must fail closed)", got)
	}
}
