// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_infra_scope_neo4j

package query

import (
	"net/http"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestLiveInfraScopeNeo4jArgoCDCategoryEquivalenceAndNoLeak proves the
// category=argocd shortcut, on the Neo4j list-EXISTS dialect, admits exactly
// the rows SHAPE-A admits (g1, g5) and the oracle's (g1, g5, cap), including a
// dual-labeled node returned once, and never a negative node.
func TestLiveInfraScopeNeo4jArgoCDCategoryEquivalenceAndNoLeak(t *testing.T) {
	l := openLiveNeo4jScope(t)
	f := seedLiveScopeFixture(t, l)
	other := f.id("other")

	argo := map[string]string{} // id -> label
	seed := func(labels, id, repoID string, admit func(d, s map[string]bool) bool) {
		props := map[string]any{"id": id, "name": id, "live7215": f.nonce}
		if repoID != "" {
			props["repo_id"] = repoID
		}
		l.raw(t, 60*time.Second, "CREATE (n:"+labels+") SET n = $props", map[string]any{"props": props})
		argo[id] = labels
		f.admit[id] = admit
	}
	for _, o := range f.owners {
		o := o
		tag := strings.TrimPrefix(o, f.nonce+"-")
		// Application admitted by direct ownership; ApplicationSet admitted by
		// a USES edge from the owner's WorkloadInstance (a scope-list term).
		seed("ArgoCDApplication", f.id("app-"+tag), o, func(d, _ map[string]bool) bool { return d[o] })
		seed("ArgoCDApplicationSet", f.id("appset-"+tag), other, func(_, s map[string]bool) bool { return s[o] })
		l.raw(t, 60*time.Second,
			"MATCH (a:WorkloadInstance {id: $from}) MATCH (b:ArgoCDApplicationSet {id: $to}) CREATE (a)-[:USES]->(b)",
			map[string]any{"from": f.id("wi-" + tag), "to": f.id("appset-" + tag)})
	}
	// Dual-labeled: must come back once, through the Application read only.
	seed("ArgoCDApplication:ArgoCDApplicationSet", f.id("dual-repo-00"), f.id("repo-00"),
		func(d, _ map[string]bool) bool { return d[f.id("repo-00")] })
	negatives := []string{f.id("app-ungranted"), f.id("appset-orphan")}
	seed("ArgoCDApplication", negatives[0], other, func(_, _ map[string]bool) bool { return false })
	seed("ArgoCDApplicationSet", negatives[1], "", func(_, _ map[string]bool) bool { return false })

	body := `{"category":"argocd","limit":200}`
	for _, g := range f.grants() {
		code, raw, elapsed := l.liveHandlerRequest(t, querycontract.GraphBackendNeo4j, g, infraSearchPath, body)
		requireStatus(t, "argocd "+g.name, code, http.StatusOK, raw)
		data := decodeInfraData(t, raw)
		if data["truncated"] != false {
			t.Fatalf("%s: argocd search truncated; the fixture must fit one page", g.name)
		}
		var got []string
		for _, r := range data["results"].([]any) {
			if id := r.(map[string]any)["id"].(string); strings.HasPrefix(id, f.nonce) {
				got = append(got, id)
			}
		}
		got = sortedStrings(got)

		var want []string
		for id := range argo {
			if f.admitted(id, g) {
				want = append(want, id)
			}
		}
		want = sortedStrings(want)
		if len(want) == 0 {
			t.Fatalf("%s: empty oracle; the proof would be vacuous", g.name)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: Neo4j argocd ids differ from oracle\n got  %v\n want %v", g.name, got, want)
		}
		for _, neg := range negatives {
			for _, id := range got {
				if id == neg {
					t.Fatalf("%s: leaked negative node %s", g.name, id)
				}
			}
		}
		if g.name != "cap" {
			// SHAPE-A reference: the real NornicDB-dialect statements, run
			// outside the handler budget, merged like the handler merges them.
			var ref []string
			seen := map[string]bool{}
			for _, call := range captureStatements(t, querycontract.GraphBackendNornicDB, g, infraSearchPath, body) {
				for _, id := range sortedIDs(l.raw(t, shapeAReferenceTimeout, call.Cypher, call.Params), "id") {
					if strings.HasPrefix(id, f.nonce) && !seen[id] {
						seen[id] = true
						ref = append(ref, id)
					}
				}
			}
			if ref = sortedStrings(ref); !reflect.DeepEqual(got, ref) {
				t.Fatalf("%s: Neo4j argocd ids differ from SHAPE-A\n neo4j  %v\n shapeA %v", g.name, got, ref)
			}
		}
		t.Logf("argocd %s: %d rows == oracle (== SHAPE-A off cap); handler %.2fs", g.name, len(got), elapsed.Seconds())
	}
}

func sortedStrings(in []string) []string {
	out := append([]string{}, in...)
	sort.Strings(out)
	return out
}
