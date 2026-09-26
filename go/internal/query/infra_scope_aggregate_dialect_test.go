// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// #7231: the scoped infra aggregate reads (count and inventory) split by graph
// dialect the way #7226 split search and relationships. NornicDB and any
// non-Neo4j value keep the SHAPE-A per-branch statements byte for byte; Neo4j
// gets the grant predicate hoisted out of the 27 label branches as one
// list-EXISTS term whose text does not depend on the grant count.

const (
	infraCountPath     = "/api/v0/infra/resources/count"
	infraInventoryPath = "/api/v0/infra/resources/inventory?group_by=provider"
)

// serveInfraAggregateDialect drives one GET aggregate request through
// serveInfraDialect.
func serveInfraAggregateDialect(
	t *testing.T,
	backend querycontract.GraphBackend,
	auth *AuthContext,
	graph GraphQuery,
	path string,
) *httptest.ResponseRecorder {
	t.Helper()
	return serveInfraDialect(t, backend, auth, graph, path, "")
}

// nornicDBAggregateStatementDigests pins the SHAPE-A aggregate statements
// NornicDB receives, captured at f55447c45 before the #7231 change. Keys are
// "<route>/<grant>". Count sends four statements (total, provider,
// environment, label); inventory sends one. Regenerate only with a live
// NornicDB proof of the new statement.
var nornicDBAggregateStatementDigests = map[string]string{
	"count/g1":      "14b0517d5a874568c54c4dc00c440508cd3b6b0dd03ed8e726bbf4bab7341ded",
	"inventory/g1":  "9deb12ff5757603bc90a70e6436b7844352ffac1bfbdaa3029f11b706fc61219",
	"count/g5":      "6d6461e78d78af4ae8ce3e8f26899669c9dcf39fed957a19f21aac4a39065dbb",
	"inventory/g5":  "a57851f804aff61d3deedf8432733bec5018b2fe3ca313bb5baa9a492bcafcb4",
	"count/cap":     "58aa5fb02bcf73f7bd373ec09ff865e85e369123e3903c6bc951ce4de1883a4c",
	"inventory/cap": "dc8e7b7d16b013522b36735adf611093609e9da0a62038388125cf863b2a319d",
}

var infraAggregateRoutes = map[string]string{
	"count":     infraCountPath,
	"inventory": infraInventoryPath,
}

func TestInfraAggregateScopeNornicDBStatementsByteIdentical(t *testing.T) {
	t.Parallel()
	for _, backend := range []querycontract.GraphBackend{querycontract.GraphBackendNornicDB, "", "bogus"} {
		for _, grant := range dialectGrants() {
			auth := grant.auth()
			for route, path := range infraAggregateRoutes {
				graph := &dialectRecordingGraph{}
				rec := serveInfraAggregateDialect(t, backend, &auth, graph, path)
				if rec.Code != http.StatusOK {
					t.Fatalf("backend %q %s: status = %d; body = %s", backend, route, rec.Code, rec.Body.String())
				}
				key := route + "/" + grant.name
				if got, want := statementDigest(t, graph.calls), nornicDBAggregateStatementDigests[key]; got != want {
					t.Errorf("backend %q %s digest = %s, want %s (%d statements)", backend, key, got, want, len(graph.calls))
				}
			}
		}
	}
}

// TestInfraAggregateNeo4jScopedHoistsOneGrantPredicate proves every scoped
// Neo4j aggregate statement carries exactly one grant predicate, after the
// CALL, in list-EXISTS form, with no SHAPE-A per-grant params, and that the
// statement text does not depend on the grant count.
func TestInfraAggregateNeo4jScopedHoistsOneGrantPredicate(t *testing.T) {
	t.Parallel()
	for route, path := range infraAggregateRoutes {
		var first []string
		for _, grant := range dialectGrants() {
			auth := grant.auth()
			graph := &dialectRecordingGraph{}
			rec := serveInfraAggregateDialect(t, querycontract.GraphBackendNeo4j, &auth, graph, path)
			if rec.Code != http.StatusOK {
				t.Fatalf("%s %s: status = %d; body = %s", route, grant.name, rec.Code, rec.Body.String())
			}
			wantCalls := 1
			if route == "count" {
				wantCalls = 4
			}
			if len(graph.calls) != wantCalls {
				t.Fatalf("%s %s: statements = %d, want %d", route, grant.name, len(graph.calls), wantCalls)
			}
			var texts []string
			for i, call := range graph.calls {
				if n := strings.Count(call.Cypher, grantPredicateMarker); n != 1 {
					t.Fatalf("%s %s statement %d: grant predicates = %d, want 1:\n%s", route, grant.name, i, n, call.Cypher)
				}
				if !strings.Contains(call.Cypher, infraResourceScopeListPredicate("n")) {
					t.Fatalf("%s %s statement %d: missing the list-EXISTS predicate:\n%s", route, grant.name, i, call.Cypher)
				}
				callEnd := strings.Index(call.Cypher, "\n}\n")
				if callEnd < 0 || strings.Index(call.Cypher, grantPredicateMarker) < callEnd {
					t.Fatalf("%s %s statement %d: grant predicate is not hoisted after the CALL:\n%s", route, grant.name, i, call.Cypher)
				}
				// UNION ALL keeps the per-branch counting SHAPE-A has: a bare
				// UNION would de-duplicate a node carrying two infra labels and
				// undercount it (and force a distinct over every row).
				if got, want := strings.Count(call.Cypher, "\nUNION ALL\n"), len(allInfraLabels)-1; got != want {
					t.Fatalf("%s %s statement %d: UNION ALL joins = %d, want %d:\n%s", route, grant.name, i, got, want, call.Cypher)
				}
				if strings.Contains(call.Cypher, "\nUNION\n") {
					t.Fatalf("%s %s statement %d: bare UNION de-duplicates dual-labeled nodes:\n%s", route, grant.name, i, call.Cypher)
				}
				if n := countScopeGrantIndexParams(call.Params); n != 0 {
					t.Fatalf("%s %s statement %d: %d scope_grant_<i> params bound, want 0", route, grant.name, i, n)
				}
				if strings.Contains(call.Cypher, "$"+querycontract.ScopeGrantInlineParamPrefix+"0") {
					t.Fatalf("%s %s statement %d: references a SHAPE-A scalar", route, grant.name, i)
				}
				scalars, _ := grant.filter().ScopeGrantInlineScalars()
				if got, _ := call.Params[infraScopeGrantsParam].([]string); !reflect.DeepEqual(got, scalars) {
					t.Fatalf("%s %s statement %d: scope_grants = %v, want %v", route, grant.name, i, got, scalars)
				}
				texts = append(texts, call.Cypher)
			}
			if first == nil {
				first = texts
			} else if !reflect.DeepEqual(first, texts) {
				t.Fatalf("%s %s: statement text depends on the grant count", route, grant.name)
			}
		}
	}
}

// TestInfraAggregateNeo4jKeepsBranchFiltersAndGrouping proves the hoist moves
// only the grant: property filters stay in every branch, and the count route's
// four statements keep their total / provider / environment / label shapes.
func TestInfraAggregateNeo4jKeepsBranchFiltersAndGrouping(t *testing.T) {
	t.Parallel()
	auth := dialectGrants()[1].auth()
	graph := &dialectRecordingGraph{}
	rec := serveInfraAggregateDialect(t, querycontract.GraphBackendNeo4j, &auth, graph, infraCountPath+"?environment=prod")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", rec.Code, rec.Body.String())
	}
	if len(graph.calls) != 4 {
		t.Fatalf("statements = %d, want 4", len(graph.calls))
	}
	wantReturns := []string{
		"RETURN count(n) AS bucket_count",
		"RETURN " + infraResourceProviderGroupExpression(InfraResourceAggregateFilter{}) + " AS bucket, count(n) AS bucket_count",
		"RETURN " + infraResourceEnvironmentGroupExpression + " AS bucket, count(n) AS bucket_count",
		"RETURN head(labels(n)) AS bucket, count(n) AS bucket_count",
	}
	for i, call := range graph.calls {
		if got, want := strings.Count(call.Cypher, "n.environment = $environment"), len(allInfraLabels); got != want {
			t.Fatalf("statement %d: branch filters = %d, want %d (one per label)", i, got, want)
		}
		if !strings.HasSuffix(call.Cypher, wantReturns[i]) {
			t.Fatalf("statement %d: does not end with %q:\n%s", i, wantReturns[i], call.Cypher)
		}
		if call.Params["environment"] != "prod" {
			t.Fatalf("statement %d: environment param = %v", i, call.Params["environment"])
		}
	}
}

// TestInfraAggregateNeo4jMergesRowsLikeShapeA proves the Go-side merge gives
// the same answer from the hoisted statements' rows as from the SHAPE-A
// per-branch rows: a summed total, and buckets summed by value.
func TestInfraAggregateNeo4jMergesRowsLikeShapeA(t *testing.T) {
	t.Parallel()
	auth := dialectGrants()[0].auth()
	// Hoisted Neo4j rows: one total row, one row per bucket.
	neo := &dialectRecordingGraph{runRows: []map[string]any{{"bucket": "aws", "bucket_count": int64(3)}}}
	neoRec := serveInfraAggregateDialect(t, querycontract.GraphBackendNeo4j, &auth, neo, infraCountPath)
	// SHAPE-A rows: the same bucket split across two label branches plus an
	// empty-branch zero row.
	shapeA := &dialectRecordingGraph{runRows: []map[string]any{
		{"bucket": "aws", "bucket_count": int64(1)},
		{"bucket": "aws", "bucket_count": int64(2)},
		{"bucket": nil, "bucket_count": int64(0)},
	}}
	shapeARec := serveInfraAggregateDialect(t, querycontract.GraphBackendNornicDB, &auth, shapeA, infraCountPath)
	if neoRec.Code != http.StatusOK || shapeARec.Code != http.StatusOK {
		t.Fatalf("status neo4j %d nornicdb %d", neoRec.Code, shapeARec.Code)
	}
	if neoRec.Body.String() != shapeARec.Body.String() {
		t.Fatalf("answers differ\n neo4j  %s\n shapeA %s", neoRec.Body.String(), shapeARec.Body.String())
	}
}

// TestInfraAggregateNeo4jCapParity proves the Neo4j $scope_grants list on the
// aggregate routes admits exactly the scalars SHAPE-A inlines past the cap.
func TestInfraAggregateNeo4jCapParity(t *testing.T) {
	t.Parallel()
	grant := dialectGrants()[2]
	auth := grant.auth()
	for route, path := range infraAggregateRoutes {
		neo := &dialectRecordingGraph{}
		serveInfraAggregateDialect(t, querycontract.GraphBackendNeo4j, &auth, neo, path)
		shapeA := &dialectRecordingGraph{}
		serveInfraAggregateDialect(t, querycontract.GraphBackendNornicDB, &auth, shapeA, path)
		if len(neo.calls) == 0 || len(shapeA.calls) == 0 {
			t.Fatalf("%s: no graph reads", route)
		}
		list, _ := neo.calls[0].Params[infraScopeGrantsParam].([]string)
		if len(list) != maxScopeGrantInlineTerms || len(grant.repos)+len(grant.scopes) <= maxScopeGrantInlineTerms {
			t.Fatalf("%s cap fixture: scope_grants len = %d, want the %d cap", route, len(list), maxScopeGrantInlineTerms)
		}
		var inlined []string
		for i := 0; ; i++ {
			v, ok := shapeA.calls[0].Params[querycontract.ScopeGrantInlineParamPrefix+strconv.Itoa(i)].(string)
			if !ok {
				break
			}
			inlined = append(inlined, v)
		}
		if !reflect.DeepEqual(list, inlined) {
			t.Fatalf("%s: Neo4j scope_grants %v differ from SHAPE-A inlined scalars %v", route, list, inlined)
		}
	}
}

func TestInfraAggregateNeo4jEmptyGrantMakesNoGraphRead(t *testing.T) {
	t.Parallel()
	auth := AuthContext{Mode: AuthModeScoped, TenantID: "tenant-a", WorkspaceID: "workspace-a"}
	for route, path := range infraAggregateRoutes {
		graph := &dialectRecordingGraph{}
		rec := serveInfraAggregateDialect(t, querycontract.GraphBackendNeo4j, &auth, graph, path)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d", route, rec.Code)
		}
		if len(graph.calls) != 0 {
			t.Fatalf("%s: empty grant made %d graph reads, want 0", route, len(graph.calls))
		}
	}
}

// TestInfraAggregateNeo4jUnscopedStatementsUnchanged proves unscoped callers
// send the same statements on every backend.
func TestInfraAggregateNeo4jUnscopedStatementsUnchanged(t *testing.T) {
	t.Parallel()
	for route, path := range infraAggregateRoutes {
		neo := &dialectRecordingGraph{}
		serveInfraAggregateDialect(t, querycontract.GraphBackendNeo4j, nil, neo, path)
		nornic := &dialectRecordingGraph{}
		serveInfraAggregateDialect(t, querycontract.GraphBackendNornicDB, nil, nornic, path)
		if len(neo.calls) == 0 || statementDigest(t, neo.calls) != statementDigest(t, nornic.calls) {
			t.Fatalf("%s: unscoped statements differ between Neo4j and NornicDB (%d calls)", route, len(neo.calls))
		}
	}
}

// TestInfraAggregateNeo4jFlagNeverDropsBranchGrant proves the Neo4j flag strips
// the grant only inside infraResourceAggregateStatement's hoisted form: every
// other builder of per-label branches (the read-model graph pass) still renders
// the SHAPE-A grant for a scoped filter, so a future caller cannot fail open.
func TestInfraAggregateNeo4jFlagNeverDropsBranchGrant(t *testing.T) {
	t.Parallel()
	grant := dialectGrants()[0]
	filter := applyInfraResourceAggregateAccess(InfraResourceAggregateFilter{}, grant.filter(), true)
	if !filter.usesNeo4jScopeDialect() {
		t.Fatal("fixture: filter does not select the Neo4j dialect")
	}
	if where := infraResourceAggregateBranchWhere(filter); !strings.Contains(where, "$scope_grant_0") {
		t.Fatalf("branch WHERE dropped the SHAPE-A grant for a Neo4j-flagged filter: %q", where)
	}
	cypher := infraGraphOnlyCountCypher(allInfraLabels[:2], nil, filter)
	if got := strings.Count(cypher, grantPredicateMarker); got != 2 {
		t.Fatalf("read-model graph pass grant predicates = %d, want 2 (one per branch):\n%s", got, cypher)
	}
}
