// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadcode_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/codequery/deadcode"
	"github.com/eshu-hq/eshu/go/internal/query/queryauth"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// The scoped incoming-edge probe answers two questions about one candidate at
// once: is anything the caller may read still calling it, and is anything they
// may not read still calling it. This file pins both answers and the shape that
// produces them.

// deadCodeIncomingProbeSource is one seeded incoming edge: the resolution
// method it carries and whether its source repository is inside the grant. A
// source the graph cannot attribute to any repository is inGrant=false, the
// same answer the backend gives for it.
type deadCodeIncomingProbeSource struct {
	method  string
	inGrant bool
}

// deadCodeIncomingProbeGraph answers the incoming-edge probe the way NornicDB
// v1.2.3 answered it on the seeded fixture, for whichever statement shape it is
// handed. It is not a re-implementation of the handler: it models the backend,
// and the difference between the two shapes is exactly what the live proof
// measured.
//
//   - the merged probe (an in_grant column) groups the seeded sources by
//     (entity, method, in_grant), so a source outside the grant stays its own
//     row even when a granted source carries the same method.
//   - the shipped pair collapses that case. Both probes RETURN DISTINCT the
//     (entity, method) pair, so the ungranted source's row is byte for byte the
//     granted one's and the diff between them is empty.
type deadCodeIncomingProbeGraph struct {
	sourcesByEntity map[string][]deadCodeIncomingProbeSource
	statements      []string
}

func (g *deadCodeIncomingProbeGraph) Run(
	_ context.Context,
	cypher string,
	params map[string]any,
) ([]map[string]any, error) {
	g.statements = append(g.statements, cypher)
	entityIDs, _ := params["entity_ids"].([]string)
	merged := strings.Contains(cypher, "in_grant")
	grantBound := !merged && strings.Contains(cypher, "source_repo:Repository")

	rows := make([]map[string]any, 0)
	for _, entityID := range entityIDs {
		seen := make(map[string]struct{})
		for _, source := range g.sourcesByEntity[entityID] {
			if grantBound && !source.inGrant {
				continue
			}
			key := source.method
			if merged {
				key = source.method + "\x00" + boolKey(source.inGrant)
			}
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			row := map[string]any{
				"incoming_entity_id": entityID,
				"resolution_method":  source.method,
			}
			if merged {
				row["in_grant"] = source.inGrant
				row["edge_count"] = 1
			}
			rows = append(rows, row)
		}
	}
	return rows, nil
}

func (g *deadCodeIncomingProbeGraph) RunSingle(
	context.Context,
	string,
	map[string]any,
) (map[string]any, error) {
	return nil, nil
}

func boolKey(value bool) string {
	if value {
		return "t"
	}
	return "f"
}

const deadCodeIncomingProbeEntity = "repo://tenant-a/granted-service#unusedHelper"

func runDeadCodeIncomingProbe(
	t *testing.T,
	sources []deadCodeIncomingProbeSource,
) (map[string]deadcode.DeadCodeIncomingEdge, *deadCodeIncomingProbeGraph) {
	t.Helper()

	graph := &deadCodeIncomingProbeGraph{
		sourcesByEntity: map[string][]deadCodeIncomingProbeSource{deadCodeIncomingProbeEntity: sources},
	}
	ctx := queryauth.ContextWithAuthContext(
		context.Background(),
		querytestutil.CodeGrantScopedAuthContext([]string{codeGrantGrantedRepo}),
	)
	incoming, err := deadCodeTestIncomingEdges(graph)(ctx, []map[string]any{{
		"entity_id": deadCodeIncomingProbeEntity,
		"repo_id":   codeGrantGrantedRepo,
		"language":  "go",
		"labels":    []any{"Function"},
	}}, "Function")
	if err != nil {
		t.Fatalf("deadCodeResultsWithGraphIncomingEdges() error = %v, want nil", err)
	}
	return incoming, graph
}

// TestDeadCodeGraphProbeKeepsASameMethodUngrantedSource is the case the shipped
// pair of probes could not see.
//
// Two sources call the candidate: one the caller may read and one it may not,
// and both edges carry the same resolution method. Each probe RETURN DISTINCTs
// the (entity, method) pair, so both return the identical single row and the
// difference between them is empty -- the candidate reads as plainly reachable
// and the caller is never told a consumer was hidden from them. The SQL half
// has always answered permission_hidden_consumer here, because it decides the
// grant per row.
func TestDeadCodeGraphProbeKeepsASameMethodUngrantedSource(t *testing.T) {
	t.Parallel()

	incoming, _ := runDeadCodeIncomingProbe(t, []deadCodeIncomingProbeSource{
		{method: codeprovenance.MethodImportBinding, inGrant: true},
		{method: codeprovenance.MethodImportBinding, inGrant: false},
	})
	edge := incoming[deadCodeIncomingProbeEntity]
	if !edge.HiddenConsumer {
		t.Fatalf("edge = %#v, want the ungranted source reported as hidden even though a granted source carries the same resolution method", edge)
	}
	if got, want := edge.Method, codeprovenance.MethodImportBinding; got != want {
		t.Fatalf("Method = %q, want %q: the granted edge is still evidence", got, want)
	}
}

// TestDeadCodeGraphProbeRunsOneTraversalPerPage is the performance half of the
// same change. The scoped path expanded every incoming edge of every candidate
// twice, once per probe, and a high-fan-in symbol pays that in full before
// either statement's DISTINCT reduces anything.
func TestDeadCodeGraphProbeRunsOneTraversalPerPage(t *testing.T) {
	t.Parallel()

	_, graph := runDeadCodeIncomingProbe(t, []deadCodeIncomingProbeSource{
		{method: codeprovenance.MethodImportBinding, inGrant: true},
	})
	if got, want := len(graph.statements), 1; got != want {
		t.Fatalf("statement count = %d, want %d: one expansion per candidate page, not one per probe", got, want)
	}
}

// TestDeadCodeGraphProbeReadsEachSourceClass covers the four answers one
// candidate's incoming edges can produce.
func TestDeadCodeGraphProbeReadsEachSourceClass(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name           string
		sources        []deadCodeIncomingProbeSource
		wantMethod     string
		wantHidden     bool
		wantConfidence float64
	}{
		{
			name:           "granted source alone is evidence",
			sources:        []deadCodeIncomingProbeSource{{method: codeprovenance.MethodImportBinding, inGrant: true}},
			wantMethod:     codeprovenance.MethodImportBinding,
			wantConfidence: codeprovenance.Confidence(codeprovenance.MethodImportBinding),
		},
		{
			name:       "ungranted source alone is only a marker",
			sources:    []deadCodeIncomingProbeSource{{method: codeprovenance.MethodImportBinding, inGrant: false}},
			wantHidden: true,
		},
		{
			name:       "unattributed source is hidden, not evidence",
			sources:    []deadCodeIncomingProbeSource{{method: codeprovenance.MethodUnspecified, inGrant: false}},
			wantHidden: true,
		},
		{
			name: "a weak granted source beside an ungranted one keeps both answers",
			sources: []deadCodeIncomingProbeSource{
				{method: codeprovenance.MethodRepoUniqueName, inGrant: true},
				{method: codeprovenance.MethodImportBinding, inGrant: false},
			},
			wantMethod:     codeprovenance.MethodRepoUniqueName,
			wantHidden:     true,
			wantConfidence: codeprovenance.Confidence(codeprovenance.MethodRepoUniqueName),
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			incoming, _ := runDeadCodeIncomingProbe(t, testCase.sources)
			edge := incoming[deadCodeIncomingProbeEntity]
			if edge.HiddenConsumer != testCase.wantHidden {
				t.Fatalf("HiddenConsumer = %v, want %v (edge = %#v)", edge.HiddenConsumer, testCase.wantHidden, edge)
			}
			if edge.Method != testCase.wantMethod {
				t.Fatalf("Method = %q, want %q", edge.Method, testCase.wantMethod)
			}
			if edge.MaxConfidence != testCase.wantConfidence {
				t.Fatalf("MaxConfidence = %v, want %v: an edge the caller cannot see is not evidence", edge.MaxConfidence, testCase.wantConfidence)
			}
		})
	}
}

// deadCodeIncomingProbeMaxKeys and deadCodeIncomingProbeMaxResults are the
// bounds go/internal/queryplan/testdata/query-source-coverage.yaml hand-declares
// for this symbol's keyed_support disposition.
const (
	deadCodeIncomingProbeMaxKeys    = 250
	deadCodeIncomingProbeMaxResults = 5000
)

// TestDeadCodeIncomingProbeMaxResultsMatchesTheManifest ties the ledger's
// declared row bound to the grouping key the shipped statement actually uses.
//
// The probe carries no LIMIT of its own, so that ledger row is the only place
// the bound is stated, and the queryplan validator only checks max_results > 0
// (queryplan.NonHotDisposition). Nothing else catches a bound that stopped
// matching the statement.
//
// The derivation, worst case for one invocation:
//
//	max_keys (250 candidate entity ids per page)
//	  x 10 distinct resolution_method values a row can project
//	  x 2  in_grant states
//	  = 5000
//
// The 10 is the closed codeprovenance vocabulary -- eight classified methods
// plus MethodUnspecified -- and one more for the null a pre-ADR-#2222 edge
// projects when it carries no resolution_method property at all. Go folds that
// null and the "unspecified" string onto the same LegacyConfidence, but the
// backend groups them separately, and this bound is a row count.
//
// The x2 is what round 8 changed. The withdrawn pair grouped on
// (entity, method); the shipped probe groups on (entity, method, in_grant), so
// every key can produce twice the rows. The ledger's previous 2500 was
// 250 x 10 and was carried forward rather than re-derived.
//
// If codeprovenance gains a resolver branch, its own closed-vocabulary rule
// already forces the Method const, the confidence table and the accuracy
// goldens to move together; this test is what adds the ledger row to that list.
// Update deadCodeIncomingProbeMaxResults and the manifest entry in the same
// change.
func TestDeadCodeIncomingProbeMaxResultsMatchesTheManifest(t *testing.T) {
	t.Parallel()

	vocabulary := []codeprovenance.Method{
		codeprovenance.MethodSCIP,
		codeprovenance.MethodDeclared,
		codeprovenance.MethodSameFile,
		codeprovenance.MethodImportBinding,
		codeprovenance.MethodTypeInferred,
		codeprovenance.MethodScopeUniqueName,
		codeprovenance.MethodCrossRepoExportPackage,
		codeprovenance.MethodRepoUniqueName,
		codeprovenance.MethodUnspecified,
	}
	for _, method := range vocabulary {
		if !codeprovenance.Valid(method) {
			t.Fatalf("codeprovenance.Valid(%q) = false; the vocabulary this bound is derived from moved", method)
		}
	}
	// One more for the null a pre-ADR-#2222 edge projects.
	methodValues := len(vocabulary) + 1
	if got, want := deadCodeIncomingProbeMaxKeys*methodValues*2, deadCodeIncomingProbeMaxResults; got != want {
		t.Fatalf("derived row bound = %d, want %d; update this const and query-source-coverage.yaml's max_results for deadCodeResultsWithGraphIncomingEdges in the same change", got, want)
	}
}

// TestDeadCodeIncomingProbeHasNoLimitOfItsOwn is why the bound above has to be
// structural. A LIMIT would clamp the row count at runtime and the ledger row
// would be a restatement of it; without one, the grouping key is the bound.
func TestDeadCodeIncomingProbeHasNoLimitOfItsOwn(t *testing.T) {
	t.Parallel()

	access := deadcode.RepositoryAccessFilter{AllowedRepositoryIDs: []string{codeGrantGrantedRepo}}
	for _, cypher := range []string{
		deadcode.BuildDeadCodeScopedIncomingBatchProbeCypher("Function", access),
		deadcode.BuildDeadCodeIncomingBatchProbeCypher("Function"),
	} {
		if strings.Contains(cypher, "LIMIT") {
			t.Fatalf("the probe gained a LIMIT; re-derive the manifest bound from it rather than from the grouping key:\n%s", cypher)
		}
	}
}

// TestDeadCodeGraphProbeTreatsAnUngrantedSourceAsUnknown and
// TestDeadCodeWeakGrantedEdgeBesideAnUngrantedOneReadsHiddenOnBothBackends
// moved here from auth_scoped_code_dead_code_hidden_consumer_test.go (#6060
// lane A code L3 PR2): both call deadCodeResultsWithGraphIncomingEdges
// directly to pin the graph probe's shape, so they stay white-box tests in
// package query beside this file's other pins of the same method instead of
// exporting it.

// TestDeadCodeGraphProbeTreatsAnUngrantedSourceAsUnknown runs the probe against
// a graph that answers it as the backend would: the candidate's one incoming
// edge comes from a repository outside the grant, so its row projects
// in_grant=false and becomes the hidden-consumer marker rather than evidence.
func TestDeadCodeGraphProbeTreatsAnUngrantedSourceAsUnknown(t *testing.T) {
	t.Parallel()

	var statements []string
	// The probe traverses incoming edges, which the shared fake routes to
	// runIncoming; run is set to the same answer so a probe that stopped
	// traversing incoming edges would still be seen here.
	probe := func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
		statements = append(statements, cypher)
		return []map[string]any{{
			"incoming_entity_id": deadCodeHiddenConsumerEntityID,
			"resolution_method":  codeprovenance.MethodImportBinding,
			"in_grant":           false,
			"edge_count":         1,
		}}, nil
	}
	graph := querytestutil.FakeGraphReader{RunFn: probe, RunIncomingFn: probe}
	results := []map[string]any{{
		"entity_id": deadCodeHiddenConsumerEntityID,
		"repo_id":   codeGrantGrantedRepo,
		"language":  "go",
		"name":      "unusedHelper",
		"labels":    []any{"Function"},
	}}
	auth := querytestutil.CodeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
	ctx := queryauth.ContextWithAuthContext(context.Background(), auth)
	incoming, err := deadCodeTestIncomingEdges(graph)(ctx, results, "Function")
	if err != nil {
		t.Fatalf("deadCodeResultsWithGraphIncomingEdges() error = %v, want nil", err)
	}
	if len(statements) != 1 {
		t.Fatalf("statement count = %d, want 1 (one expansion with the grant projected per row)", len(statements))
	}
	edge := incoming[deadCodeHiddenConsumerEntityID]
	if !edge.HiddenConsumer {
		t.Fatalf("edge = %#v, want the ungranted source reported as hidden", edge)
	}
	if edge.MaxConfidence != 0 {
		t.Fatalf("MaxConfidence = %v, want 0 for an edge the caller cannot see", edge.MaxConfidence)
	}
}

// TestDeadCodeWeakGrantedEdgeBesideAnUngrantedOneReadsHiddenOnBothBackends
// covers the candidate the caller can see one weak edge into and cannot see
// another. The SQL half reports permission_hidden_consumer for it, because the
// grant is decided per row. The graph half diffs two probes, so it has to diff
// them per edge as well: diffing whole entities lets the granted edge hide the
// ungranted one, and the same candidate then reads as a weak-evidence review
// item on one backend and a permission question on the other.
func TestDeadCodeWeakGrantedEdgeBesideAnUngrantedOneReadsHiddenOnBothBackends(t *testing.T) {
	t.Parallel()

	for _, backend := range []struct {
		name     string
		incoming func(*testing.T) (content, graph map[string]deadcode.DeadCodeIncomingEdge)
	}{
		{name: "sql", incoming: deadCodeWeakGrantedPlusUngrantedFromSQL},
		{name: "graph", incoming: deadCodeWeakGrantedPlusUngrantedFromGraph},
	} {
		t.Run(backend.name, func(t *testing.T) {
			t.Parallel()

			content, graph := backend.incoming(t)
			results := []map[string]any{{
				"entity_id": deadCodeHiddenConsumerEntityID,
				"repo_id":   codeGrantGrantedRepo,
				"language":  "go",
				"name":      "unusedHelper",
			}}
			kept := deadcode.ApplyDeadCodeIncomingEdges(results, content, graph)
			if len(kept) != 1 {
				t.Fatalf("kept = %#v, want the candidate kept: a weak edge is not proof it is reachable", kept)
			}
			if got, want := kept[0]["classification"], codemodel.DeadCodeClassificationAmbiguous; got != want {
				t.Fatalf("classification = %v, want %q", got, want)
			}
			reasons := deadcode.DeadCodeInvestigationAmbiguityReasons(kept[0])
			if !slices.Contains(reasons, codemodel.DeadCodeHiddenConsumerReason) {
				t.Fatalf("ambiguity_reasons = %#v, want %q: an edge the caller cannot see decides the answer even when a weak one beside it can be seen", reasons, codemodel.DeadCodeHiddenConsumerReason)
			}
		})
	}
}

// deadCodeWeakGrantedPlusUngrantedFromSQL runs the shipped reachability read
// over two materialized rows for one entity: a weak consumer inside the grant
// and a stronger one outside it.
// deadCodeWeakGrantedPlusUngrantedFromSQL hand-builds the SQL backend's half
// of the parity shape: one weak granted edge plus one ungranted edge for the
// same entity.
//
// It used to drive the real ContentReader over a recording SQL fake, which
// cannot move to codequery without recreating ContentReader there (#6060).
// The SQL-to-maps half of the contract now lives in package query
// (TestCodeReachabilityIncomingEntityIDsBindsTheConsumerGrant/weak_granted),
// which pins the same literal below; keep the two in sync.
func deadCodeWeakGrantedPlusUngrantedFromSQL(t *testing.T) (map[string]deadcode.DeadCodeIncomingEdge, map[string]deadcode.DeadCodeIncomingEdge) {
	t.Helper()

	incoming := map[string]deadcode.DeadCodeIncomingEdge{
		deadCodeHiddenConsumerEntityID: {
			MaxConfidence:  codeprovenance.Confidence(codeprovenance.MethodRepoUniqueName),
			Method:         string(codeprovenance.MethodRepoUniqueName),
			HiddenConsumer: true,
		},
	}
	return incoming, nil
}

// deadCodeWeakGrantedPlusUngrantedFromGraph runs the shipped graph probe over
// the same shape: one weak edge from inside the grant and one stronger edge
// from outside it, each its own row with its own in_grant answer.
func deadCodeWeakGrantedPlusUngrantedFromGraph(t *testing.T) (map[string]deadcode.DeadCodeIncomingEdge, map[string]deadcode.DeadCodeIncomingEdge) {
	t.Helper()

	probe := func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
		return []map[string]any{{
			"incoming_entity_id": deadCodeHiddenConsumerEntityID,
			"resolution_method":  codeprovenance.MethodRepoUniqueName,
			"in_grant":           true,
			"edge_count":         1,
		}, {
			"incoming_entity_id": deadCodeHiddenConsumerEntityID,
			"resolution_method":  codeprovenance.MethodImportBinding,
			"in_grant":           false,
			"edge_count":         1,
		}}, nil
	}
	probeGraph := querytestutil.FakeGraphReader{RunFn: probe, RunIncomingFn: probe}
	auth := querytestutil.CodeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
	ctx := queryauth.ContextWithAuthContext(context.Background(), auth)
	graph, err := deadCodeTestIncomingEdges(probeGraph)(ctx, []map[string]any{{
		"entity_id": deadCodeHiddenConsumerEntityID,
		"repo_id":   codeGrantGrantedRepo,
		"language":  "go",
		"labels":    []any{"Function"},
	}}, "Function")
	if err != nil {
		t.Fatalf("deadCodeResultsWithGraphIncomingEdges() error = %v, want nil", err)
	}
	return nil, graph
}
