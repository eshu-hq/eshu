// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// #7215: the scoped infra search and relationships reads split by graph
// dialect. NornicDB (and any non-Neo4j value) keeps the SHAPE-A inline-map
// statements byte for byte; Neo4j gets one hoisted list-EXISTS grant predicate
// whose text does not depend on the grant count.

const (
	infraSearchPath        = "/api/v0/infra/resources/search"
	infraRelationshipsPath = "/api/v0/infra/relationships"
	infraSearchBody        = `{"query":"api"}`
	infraRelationshipsBody = `{"entity_id":"does-not-exist"}`
	// grantPredicateMarker occurs once per rendered grant predicate in both
	// dialects (the direct-ownership disjunct), so counting it counts
	// predicate copies.
	grantPredicateMarker = ".repo_id IN $allowed_repository_ids"
)

// nornicDBStatementDigests pins the SHAPE-A statements NornicDB receives
// today (captured at 348861541, before the #7215 change). Keys are
// "<route>/<grant>". A mismatch means the NornicDB path changed; that must
// never happen as a side effect of a Neo4j-only rewrite. Regenerate only with
// a live NornicDB proof of the new statement.
var nornicDBStatementDigests = map[string]string{
	"search/g1":         "47ee31764c58d0168c81eb6543e876e1198f3ce1b8aeb50b2f42cb488d02ec3b",
	"relationships/g1":  "bdda8475e2a279b0c82a34394faecf008d5189c845ea9d67e8af38efb1bb8554",
	"search/g5":         "15c005edfc04b3ff5fd4849bcc638592ccde4d64f4f12d464523f10258c0364b",
	"relationships/g5":  "95a385230443088ba63b6310b7da7b4cdf8b72eccc0421cf069a299a3de65a8b",
	"search/cap":        "119f9e8c91d8b1b24afe541bb6499b870ddbf6db5dcf72238659a3f26d94a77d",
	"relationships/cap": "dfbe0ae08edbdd31b00a677397dcbe7d688e30f461de0e311fcfc2e566c76e91",
}

func TestInfraScopeNornicDBStatementsByteIdentical(t *testing.T) {
	t.Parallel()
	for _, backend := range []querycontract.GraphBackend{querycontract.GraphBackendNornicDB, ""} {
		for _, grant := range dialectGrants() {
			auth := grant.auth()

			search := &dialectRecordingGraph{}
			serveInfraDialect(t, backend, &auth, search, infraSearchPath, infraSearchBody)
			rel := &dialectRecordingGraph{}
			serveInfraDialect(t, backend, &auth, rel, infraRelationshipsPath, infraRelationshipsBody)

			for key, calls := range map[string][]recordedInfraCall{
				"search/" + grant.name:        search.calls,
				"relationships/" + grant.name: rel.calls,
			} {
				got := statementDigest(t, calls)
				if want := nornicDBStatementDigests[key]; got != want {
					t.Errorf("backend %q %s digest = %s, want %s (%d statements)", backend, key, got, want, len(calls))
				}
			}
		}
	}
}

func TestInfraSearchNeo4jScopedHoistsOneGrantPredicate(t *testing.T) {
	t.Parallel()
	var firstCypher string
	for _, grant := range dialectGrants() {
		auth := grant.auth()
		graph := &dialectRecordingGraph{}
		rec := serveInfraDialect(t, querycontract.GraphBackendNeo4j, &auth, graph, infraSearchPath, infraSearchBody)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d; body = %s", grant.name, rec.Code, rec.Body.String())
		}
		if len(graph.calls) != 1 {
			t.Fatalf("%s: graph calls = %d, want 1", grant.name, len(graph.calls))
		}
		call := graph.calls[0]
		if got := strings.Count(call.Cypher, grantPredicateMarker); got != 1 {
			t.Fatalf("%s: grant predicate copies = %d, want exactly 1 (hoisted after the CALL):\n%s", grant.name, got, call.Cypher)
		}
		if strings.Contains(call.Cypher, "$"+querycontract.ScopeGrantInlineParamPrefix) {
			t.Fatalf("%s: Neo4j search must not reference per-grant $scope_grant_<i> params:\n%s", grant.name, call.Cypher)
		}
		if got := countScopeGrantIndexParams(call.Params); got != 0 {
			t.Fatalf("%s: Neo4j search bound %d scope_grant_<i> params, want 0", grant.name, got)
		}
		wantGrants, _ := grant.filter().ScopeGrantInlineScalars()
		if got, ok := call.Params["scope_grants"].([]string); !ok || !reflect.DeepEqual(got, wantGrants) {
			t.Fatalf("%s: scope_grants = %#v, want the capped ScopeGrantInlineScalars slice %#v", grant.name, call.Params["scope_grants"], wantGrants)
		}
		callEnd := strings.LastIndex(call.Cypher, "}\n")
		if pred := strings.Index(call.Cypher, grantPredicateMarker); callEnd < 0 || pred < callEnd {
			t.Fatalf("%s: grant predicate must follow the CALL subquery, not sit inside a branch:\n%s", grant.name, call.Cypher)
		}
		if got, want := strings.Count(call.Cypher, "RETURN n\n"), len(allInfraLabels); got != want {
			t.Fatalf("%s: branches returning n = %d, want %d:\n%s", grant.name, got, want, call.Cypher)
		}
		if firstCypher == "" {
			firstCypher = call.Cypher
		} else if call.Cypher != firstCypher {
			t.Fatalf("%s: Neo4j search text changed with grant count; it must be one plan-cache entry", grant.name)
		}
	}
}

func TestInfraRelationshipsNeo4jScopedUsesListPredicateOnAllAliases(t *testing.T) {
	t.Parallel()
	for _, grant := range dialectGrants() {
		auth := grant.auth()
		graph := &dialectRecordingGraph{single: func(cypher string, _ map[string]any) map[string]any {
			if strings.Contains(cypher, "(n:TerraformResource)") {
				if isAnchorProbe(cypher) {
					return map[string]any{"hit": int64(1)}
				}
				return map[string]any{"id": "tf-1", "name": "tf", "labels": []any{"TerraformResource"}, "outgoing": []any{}, "incoming": []any{}}
			}
			return nil
		}}
		rec := serveInfraDialect(t, querycontract.GraphBackendNeo4j, &auth, graph, infraRelationshipsPath, `{"entity_id":"tf-1"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d; body = %s", grant.name, rec.Code, rec.Body.String())
		}
		scoped := 0
		wantGrants, _ := grant.filter().ScopeGrantInlineScalars()
		for _, call := range graph.calls {
			if isAnchorProbe(call.Cypher) {
				if strings.Contains(call.Cypher, "allowed_") || len(call.Params) != 1 {
					t.Fatalf("%s: probe must bind only entity_id and no grant: %v\n%s", grant.name, call.Params, call.Cypher)
				}
				continue
			}
			scoped++
			if got := strings.Count(call.Cypher, grantPredicateMarker); got != 3 {
				t.Fatalf("%s: scoped statement grant predicates = %d, want 3 (n, target, source):\n%s", grant.name, got, call.Cypher)
			}
			if strings.Contains(call.Cypher, "$"+querycontract.ScopeGrantInlineParamPrefix) || countScopeGrantIndexParams(call.Params) != 0 {
				t.Fatalf("%s: Neo4j relationships must not use per-grant params:\n%s", grant.name, call.Cypher)
			}
			if got, ok := call.Params["scope_grants"].([]string); !ok || !reflect.DeepEqual(got, wantGrants) {
				t.Fatalf("%s: scope_grants = %#v, want %#v", grant.name, call.Params["scope_grants"], wantGrants)
			}
		}
		if scoped != 1 {
			t.Fatalf("%s: scoped statements = %d, want 1 (only the label whose probe hit)", grant.name, scoped)
		}
	}
}

// TestInfraRelationshipsNeo4jProbeHitDoesNotDiscloseUngrantedAnchor proves the
// unscoped probe's hit never reaches the caller: an ungranted anchor, labeled
// or unlabeled, answers with exactly the bytes a nonexistent id gets.
func TestInfraRelationshipsNeo4jProbeHitDoesNotDiscloseUngrantedAnchor(t *testing.T) {
	t.Parallel()
	auth := dialectGrants()[0].auth()

	missing := &dialectRecordingGraph{}
	missRec := serveInfraDialect(t, querycontract.GraphBackendNeo4j, &auth, missing, infraRelationshipsPath, infraRelationshipsBody)
	if missRec.Code != http.StatusNotFound {
		t.Fatalf("nonexistent id status = %d, want 404", missRec.Code)
	}
	if got, want := len(missing.calls), len(impactRelationshipAnchorLabels)+1; got != want {
		t.Fatalf("nonexistent id graph calls = %d, want %d (every probe, then the scoped unlabeled fallback)", got, want)
	}
	if last := missing.calls[len(missing.calls)-1].Cypher; isAnchorProbe(last) || !strings.Contains(last, "MATCH (n) WHERE n.id = $entity_id") {
		t.Fatalf("last read must be the scoped unlabeled fallback:\n%s", last)
	}

	cases := map[string]func(string) bool{
		// The id exists on CloudResource (a probed anchor label), but the
		// grant does not admit it.
		"labeled": func(cypher string) bool {
			return strings.Contains(cypher, "(n:CloudResource)") && isAnchorProbe(cypher)
		},
		// The id exists only on a label outside the probe list.
		"unlabeled": func(string) bool { return false },
	}
	for name, probeHit := range cases {
		graph := &dialectRecordingGraph{single: func(cypher string, _ map[string]any) map[string]any {
			if probeHit(cypher) {
				return map[string]any{"hit": int64(1)}
			}
			return nil // every scoped statement is ungranted
		}}
		rec := serveInfraDialect(t, querycontract.GraphBackendNeo4j, &auth, graph, infraRelationshipsPath, infraRelationshipsBody)
		scopedReads := 0
		for _, call := range graph.calls {
			if !isAnchorProbe(call.Cypher) {
				scopedReads++
			}
		}
		if want := map[string]int{"labeled": 2, "unlabeled": 1}[name]; scopedReads != want {
			t.Fatalf("%s: scoped reads = %d, want %d", name, scopedReads, want)
		}
		if rec.Code != missRec.Code || rec.Body.String() != missRec.Body.String() {
			t.Fatalf("%s ungranted anchor answered %d %q, want the nonexistent-id answer %d %q",
				name, rec.Code, rec.Body.String(), missRec.Code, missRec.Body.String())
		}
	}
}

func TestInfraScopeNeo4jEmptyGrantMakesNoGraphRead(t *testing.T) {
	t.Parallel()
	auth := AuthContext{Mode: AuthModeScoped, TenantID: "tenant-a", WorkspaceID: "workspace-a"}
	for path, body := range map[string]string{infraSearchPath: infraSearchBody, infraRelationshipsPath: infraRelationshipsBody} {
		graph := &dialectRecordingGraph{}
		serveInfraDialect(t, querycontract.GraphBackendNeo4j, &auth, graph, path, body)
		if len(graph.calls) != 0 {
			t.Fatalf("%s: empty grant made %d graph reads, want 0", path, len(graph.calls))
		}
	}
}

// TestInfraScopeListExistsNeverSelectedOffNeo4j guards the authorization
// boundary: the list-EXISTS predicate is an always-true whole-graph leak on
// NornicDB, so only an explicit Neo4j backend may select it.
func TestInfraScopeListExistsNeverSelectedOffNeo4j(t *testing.T) {
	t.Parallel()
	auth := dialectGrants()[1].auth()
	for _, backend := range []querycontract.GraphBackend{querycontract.GraphBackendNornicDB, "", "bogus"} {
		for path, body := range map[string]string{infraSearchPath: infraSearchBody, infraRelationshipsPath: infraRelationshipsBody} {
			graph := &dialectRecordingGraph{}
			serveInfraDialect(t, backend, &auth, graph, path, body)
			for _, call := range graph.calls {
				if strings.Contains(call.Cypher, "$scope_grants") {
					t.Fatalf("backend %q %s selected the list-EXISTS predicate:\n%s", backend, path, call.Cypher)
				}
				if !strings.Contains(call.Cypher, "$scope_grant_0") {
					t.Fatalf("backend %q %s lost the SHAPE-A inline-map predicate:\n%s", backend, path, call.Cypher)
				}
			}
		}
	}
}

// TestInfraScopeNeo4jCapParity proves the Neo4j list admits exactly the grant
// scalars SHAPE-A inlines, including the 128 cap past 129+ scalars.
func TestInfraScopeNeo4jCapParity(t *testing.T) {
	t.Parallel()
	grant := dialectGrants()[2]
	auth := grant.auth()
	neo := &dialectRecordingGraph{}
	serveInfraDialect(t, querycontract.GraphBackendNeo4j, &auth, neo, infraSearchPath, infraSearchBody)
	shapeA := &dialectRecordingGraph{}
	serveInfraDialect(t, querycontract.GraphBackendNornicDB, &auth, shapeA, infraSearchPath, infraSearchBody)

	list, _ := neo.calls[0].Params["scope_grants"].([]string)
	if len(list) != maxScopeGrantInlineTerms || len(grant.repos)+len(grant.scopes) <= maxScopeGrantInlineTerms {
		t.Fatalf("cap fixture: scope_grants len = %d of %d scalars, want the %d cap", len(list), len(grant.repos)+len(grant.scopes), maxScopeGrantInlineTerms)
	}
	inlined := make([]string, 0, maxScopeGrantInlineTerms)
	for i := 0; ; i++ {
		v, ok := shapeA.calls[0].Params[querycontract.ScopeGrantInlineParamPrefix+strconv.Itoa(i)].(string)
		if !ok {
			break
		}
		inlined = append(inlined, v)
	}
	if !reflect.DeepEqual(list, inlined) {
		t.Fatalf("Neo4j scope_grants %v differ from SHAPE-A inlined scalars %v", list, inlined)
	}
}

// TestInfraScopeNeo4jUnscopedStatementsUnchanged proves the dialect split
// leaves the unscoped statements identical across backends.
func TestInfraScopeNeo4jUnscopedStatementsUnchanged(t *testing.T) {
	t.Parallel()
	for path, body := range map[string]string{infraSearchPath: infraSearchBody, infraRelationshipsPath: infraRelationshipsBody} {
		neo := &dialectRecordingGraph{}
		serveInfraDialect(t, querycontract.GraphBackendNeo4j, nil, neo, path, body)
		nornic := &dialectRecordingGraph{}
		serveInfraDialect(t, querycontract.GraphBackendNornicDB, nil, nornic, path, body)
		if statementDigest(t, neo.calls) != statementDigest(t, nornic.calls) {
			t.Fatalf("%s: unscoped statements differ between Neo4j and NornicDB", path)
		}
	}
}

func isAnchorProbe(cypher string) bool {
	return strings.Contains(cypher, "RETURN 1 AS hit")
}
