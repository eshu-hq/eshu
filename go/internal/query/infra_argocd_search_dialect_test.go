// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// #7215: the category=argocd search shortcut (searchArgoCDCategoryRows) is
// scoped by the same dialect split as the main search. NornicDB and every
// non-Neo4j value keep the SHAPE-A statements byte for byte; Neo4j applies the
// list-EXISTS predicate per label, so the statement text is independent of the
// grant count.

const infraArgoCDSearchBody = `{"category":"argocd"}`

// nornicDBArgoCDStatementDigests pins the SHAPE-A category=argocd statements
// (both label reads) NornicDB receives today, captured before the ArgoCD path
// joined the dialect split. Keys are the grant names from dialectGrants.
var nornicDBArgoCDStatementDigests = map[string]string{
	"g1":  "a89864df9e2c56050757226e40b36520d350dcf0a77687b0478345f501123940",
	"g5":  "7ee01a9ad47457f05e43ccec901c635643aab8cbddd96500a85d34e7824e7734",
	"cap": "e93d69e7e75c31288a7dc726b2a8a56c30272c5c6de21c1b2c65e2ada3adb618",
}

func TestInfraArgoCDSearchNornicDBStatementsByteIdentical(t *testing.T) {
	t.Parallel()
	for _, backend := range []querycontract.GraphBackend{querycontract.GraphBackendNornicDB, "", "bogus"} {
		for _, grant := range dialectGrants() {
			auth := grant.auth()
			graph := &dialectRecordingGraph{}
			serveInfraDialect(t, backend, &auth, graph, infraSearchPath, infraArgoCDSearchBody)
			if len(graph.calls) != 2 {
				t.Fatalf("backend %q %s: argocd reads = %d, want 2 (one per label)", backend, grant.name, len(graph.calls))
			}
			if got, want := statementDigest(t, graph.calls), nornicDBArgoCDStatementDigests[grant.name]; got != want {
				t.Errorf("backend %q argocd/%s digest = %s, want %s", backend, grant.name, got, want)
			}
		}
	}
}

func TestInfraArgoCDSearchNeo4jScopedUsesListPredicatePerLabel(t *testing.T) {
	t.Parallel()
	var firstStatements []string
	for _, grant := range dialectGrants() {
		auth := grant.auth()
		graph := &dialectRecordingGraph{}
		rec := serveInfraDialect(t, querycontract.GraphBackendNeo4j, &auth, graph, infraSearchPath, infraArgoCDSearchBody)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d; body = %s", grant.name, rec.Code, rec.Body.String())
		}
		if len(graph.calls) != 2 {
			t.Fatalf("%s: argocd reads = %d, want 2 (one per label)", grant.name, len(graph.calls))
		}
		wantGrants, _ := grant.filter().ScopeGrantInlineScalars()
		statements := make([]string, 0, len(graph.calls))
		for i, call := range graph.calls {
			statements = append(statements, call.Cypher)
			if got := strings.Count(call.Cypher, grantPredicateMarker); got != 1 {
				t.Fatalf("%s read %d: grant predicate copies = %d, want 1:\n%s", grant.name, i, got, call.Cypher)
			}
			if !strings.Contains(call.Cypher, "IN $scope_grants") {
				t.Fatalf("%s read %d: missing list-EXISTS grant term:\n%s", grant.name, i, call.Cypher)
			}
			if strings.Contains(call.Cypher, "$"+querycontract.ScopeGrantInlineParamPrefix) {
				t.Fatalf("%s read %d: Neo4j argocd must not reference $scope_grant_<i>:\n%s", grant.name, i, call.Cypher)
			}
			if got := countScopeGrantIndexParams(call.Params); got != 0 {
				t.Fatalf("%s read %d: bound %d scope_grant_<i> params, want 0", grant.name, i, got)
			}
			if got, ok := call.Params["scope_grants"].([]string); !ok || !reflect.DeepEqual(got, wantGrants) {
				t.Fatalf("%s read %d: scope_grants = %#v, want %#v", grant.name, i, call.Params["scope_grants"], wantGrants)
			}
			if _, ok := call.Params["limit"]; !ok {
				t.Fatalf("%s read %d: limit param dropped", grant.name, i)
			}
		}
		if !strings.Contains(statements[0], "(n:ArgoCDApplication)") ||
			!strings.Contains(statements[1], "(n:ArgoCDApplicationSet)") ||
			!strings.Contains(statements[1], "AND NOT n:ArgoCDApplication") {
			t.Fatalf("%s: label reads or dual-label exclusion changed:\n%v", grant.name, statements)
		}
		if firstStatements == nil {
			firstStatements = statements
		} else if !reflect.DeepEqual(statements, firstStatements) {
			t.Fatalf("%s: Neo4j argocd text changed with grant count; it must be one plan-cache entry", grant.name)
		}
	}
}

// TestInfraArgoCDSearchNeo4jUnscopedStatementsUnchanged proves an unscoped
// category=argocd read is identical across backends.
func TestInfraArgoCDSearchNeo4jUnscopedStatementsUnchanged(t *testing.T) {
	t.Parallel()
	neo := &dialectRecordingGraph{}
	serveInfraDialect(t, querycontract.GraphBackendNeo4j, nil, neo, infraSearchPath, infraArgoCDSearchBody)
	nornic := &dialectRecordingGraph{}
	serveInfraDialect(t, querycontract.GraphBackendNornicDB, nil, nornic, infraSearchPath, infraArgoCDSearchBody)
	if len(neo.calls) != 2 || statementDigest(t, neo.calls) != statementDigest(t, nornic.calls) {
		t.Fatalf("unscoped argocd statements differ between Neo4j and NornicDB")
	}
}
