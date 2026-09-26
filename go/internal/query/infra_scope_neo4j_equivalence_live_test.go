// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_infra_scope_neo4j

package query

import (
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// shapeAReferenceTimeout bounds one SHAPE-A reference statement run outside
// the handler. SHAPE-A g5 search planned in about 34 s cold in the #7215
// proof, so the reference needs far more than the handler's 10 s.
const shapeAReferenceTimeout = 280 * time.Second

// captureStatements records the statements a backend's handler would send,
// without a backend.
func captureStatements(t *testing.T, backend querycontract.GraphBackend, g dialectGrant, path, body string) []recordedInfraCall {
	t.Helper()
	auth := g.auth()
	graph := &dialectRecordingGraph{}
	serveInfraDialect(t, backend, &auth, graph, path, body)
	return graph.calls
}

// shapeASearchReference returns the SHAPE-A admitted ids. For g1 and g5 it
// runs the real SHAPE-A statement. At the cap the per-branch SHAPE-A search
// does not plan on Neo4j at all (the #7215 proof: g25 failed after about 249
// s), so it runs the SHAPE-A predicate hoisted after the CALL, which keeps
// SHAPE-A's admission semantics exactly.
func shapeASearchReference(t *testing.T, l *liveNeo4jScope, g dialectGrant, body string) []string {
	t.Helper()
	if g.name != "cap" {
		call := captureStatements(t, querycontract.GraphBackendNornicDB, g, infraSearchPath, body)[0]
		return sortedIDs(l.raw(t, shapeAReferenceTimeout, call.Cypher, call.Params), "id")
	}
	call := captureStatements(t, querycontract.GraphBackendNeo4j, g, infraSearchPath, body)[0]
	scalars, _ := g.filter().ScopeGrantInlineScalars()
	cypher := strings.Replace(call.Cypher, infraResourceScopeListPredicate("n"), infraResourceScopePredicate("n", scalars), 1)
	if cypher == call.Cypher {
		t.Fatal("cap reference: list predicate not found in the Neo4j statement")
	}
	params := map[string]any{}
	for k, v := range call.Params {
		if k != infraScopeGrantsParam {
			params[k] = v
		}
	}
	g.filter().GraphParams(params)
	return sortedIDs(l.raw(t, shapeAReferenceTimeout, cypher, params), "id")
}

// TestLiveInfraScopeNeo4jSearchEquivalenceAndNoLeak proves the Neo4j search
// admits exactly SHAPE-A's rows and the oracle's, and never a negative node.
func TestLiveInfraScopeNeo4jSearchEquivalenceAndNoLeak(t *testing.T) {
	l := openLiveNeo4jScope(t)
	f := seedLiveScopeFixture(t, l)
	body := fmt.Sprintf(`{"query":%q,"limit":200}`, f.nonce)
	negatives := []string{"cr-ungranted", "cr-orphan", "tsr-unmatched", "tsr-ungranted", "k8s-ungranted", "tf-other"}

	for _, g := range f.grants() {
		code, raw, elapsed := l.liveHandlerRequest(t, querycontract.GraphBackendNeo4j, g, infraSearchPath, body)
		requireStatus(t, "search "+g.name, code, http.StatusOK, raw)
		data := decodeInfraData(t, raw)
		if data["truncated"] != false {
			t.Fatalf("%s: search truncated; the fixture must fit one page", g.name)
		}
		var got []string
		for _, r := range data["results"].([]any) {
			got = append(got, r.(map[string]any)["id"].(string))
		}
		sort.Strings(got)

		want := f.expectedSearch(g)
		if len(want) == 0 {
			t.Fatalf("%s: empty oracle; the proof would be vacuous", g.name)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: Neo4j search ids differ from oracle\n got  %v\n want %v", g.name, got, want)
		}
		ref := shapeASearchReference(t, l, g, body)
		if !reflect.DeepEqual(got, ref) {
			t.Fatalf("%s: Neo4j search ids differ from SHAPE-A\n neo4j  %v\n shapeA %v", g.name, got, ref)
		}
		for _, neg := range negatives {
			for _, id := range got {
				if id == f.id(neg) {
					t.Fatalf("%s: leaked negative node %s", g.name, id)
				}
			}
		}
		t.Logf("search %s: %d rows == oracle == SHAPE-A; handler %.2fs", g.name, len(got), elapsed.Seconds())
	}
}

// relationshipSignature normalizes one relationships answer (handler JSON or
// a raw SHAPE-A row) to id, labels and sorted edge tuples.
func relationshipSignature(row map[string]any) string {
	var edges []string
	for _, dir := range []struct{ key, idKey string }{{"outgoing", "target_id"}, {"incoming", "source_id"}} {
		list, _ := row[dir.key].([]any)
		for _, item := range list {
			m, ok := item.(map[string]any)
			if !ok || m["type"] == nil {
				continue
			}
			edges = append(edges, fmt.Sprintf("%s|%v|%v", dir.key, m["type"], m[dir.idKey]))
		}
	}
	sort.Strings(edges)
	return fmt.Sprintf("%v %v %v", row["id"], row["labels"], edges)
}

// shapeARelationshipsReference replays the SHAPE-A anchor loop statement by
// statement outside the handler budget and returns the first row, or nil.
func shapeARelationshipsReference(t *testing.T, l *liveNeo4jScope, g dialectGrant, body string) map[string]any {
	t.Helper()
	for _, call := range captureStatements(t, querycontract.GraphBackendNornicDB, g, infraRelationshipsPath, body) {
		if rows := l.raw(t, shapeAReferenceTimeout, call.Cypher, call.Params); len(rows) > 0 {
			return rows[0]
		}
	}
	return nil
}

// TestLiveInfraScopeNeo4jRelationshipsEquivalenceAndNoLeak proves the Neo4j
// relationships route answers exactly like SHAPE-A, returns the nonexistent-id
// 404 for every ungranted anchor (labeled and unlabeled-fallback), and never
// returns an ungranted neighbour in either direction.
func TestLiveInfraScopeNeo4jRelationshipsEquivalenceAndNoLeak(t *testing.T) {
	l := openLiveNeo4jScope(t)
	f := seedLiveScopeFixture(t, l)
	bodyFor := func(id string) string { return fmt.Sprintf(`{"entity_id":%q}`, id) }

	for _, g := range f.grants() {
		missCode, missBody, _ := l.liveHandlerRequest(t, querycontract.GraphBackendNeo4j, g, infraRelationshipsPath, bodyFor(f.id("does-not-exist")))
		requireStatus(t, "nonexistent "+g.name, missCode, http.StatusNotFound, missBody)

		// Labeled (CloudResource / TerraformStateResource are probed anchor
		// labels, so the probe HITS) and unlabeled-fallback (K8sResource,
		// HelmChart are not) ungranted anchors.
		ungranted := []string{"cr-ungranted", "cr-orphan", "tsr-unmatched", "tsr-ungranted", "k8s-ungranted", "tf-other"}
		if g.name == "g1" {
			ungranted = append(ungranted, "cr-repo-01", "hc-repo-01") // granted only from g5
		}
		for _, part := range ungranted {
			code, raw, elapsed := l.liveHandlerRequest(t, querycontract.GraphBackendNeo4j, g, infraRelationshipsPath, bodyFor(f.id(part)))
			if code != missCode || string(raw) != string(missBody) {
				t.Fatalf("%s %s: ungranted anchor answered %d %s, want the nonexistent-id answer %d %s", g.name, part, code, raw, missCode, missBody)
			}
			t.Logf("relationships %s %s: 404 identical to nonexistent; handler %.2fs", g.name, part, elapsed.Seconds())
		}

		for _, part := range []string{"tf-repo-00", "cr-repo-00", "k8s-repo-00", "hc-scope-00"} {
			body := bodyFor(f.id(part))
			code, raw, elapsed := l.liveHandlerRequest(t, querycontract.GraphBackendNeo4j, g, infraRelationshipsPath, body)
			requireStatus(t, "relationships "+g.name+" "+part, code, http.StatusOK, raw)
			data := decodeInfraData(t, raw)
			for _, dir := range []struct{ key, idKey string }{{"outgoing", "target_id"}, {"incoming", "source_id"}} {
				list, _ := data[dir.key].([]any)
				for _, item := range list {
					other := fmt.Sprint(item.(map[string]any)[dir.idKey])
					if !f.admitted(other, g) {
						t.Fatalf("%s %s: returned ungranted %s neighbour %s", g.name, part, dir.key, other)
					}
				}
			}
			if part == "tf-repo-00" && !strings.Contains(string(raw), f.id("cr-repo-00")) {
				t.Fatalf("%s: granted neighbour cr-repo-00 missing; the no-leak check would be vacuous: %s", g.name, raw)
			}
			if g.name != "cap" {
				ref := shapeARelationshipsReference(t, l, g, body)
				if ref == nil || relationshipSignature(data) != relationshipSignature(ref) {
					t.Fatalf("%s %s: Neo4j answer differs from SHAPE-A\n neo4j  %s\n shapeA %v", g.name, part, relationshipSignature(data), ref)
				}
			}
			t.Logf("relationships %s %s: 200, neighbours all granted; handler %.2fs", g.name, part, elapsed.Seconds())
		}
	}
}
