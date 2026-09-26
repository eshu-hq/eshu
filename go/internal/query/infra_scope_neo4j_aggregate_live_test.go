// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_infra_scope_neo4j

package query

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// Live Neo4j proof for the #7231 scoped aggregate dialect (count and
// inventory), on the #7215 fixture (seedLiveScopeFixture). Run it the same way
// as infra_scope_neo4j_live_test.go.

// ungrantedBucket tags every negative fixture node's provider and environment,
// so a leaked node would surface as its own bucket.
const ungrantedBucket = "ungranted-leak"

// aggregateFixture adds provider / environment signals to the #7215 fixture
// and returns the per-node buckets the oracle uses. The rule is fixed in Go
// (sorted id position), so the oracle never reads the graph.
type aggregateFixture struct {
	*liveScopeFixture
	provider map[string]string
	env      map[string]string
	// dual is a granted node that carries two infra labels (its fixture label
	// plus CloudResource). UNION ALL counts it once per matching label branch,
	// exactly as SHAPE-A does, so the oracle counts it twice. A bare UNION in
	// the Neo4j statement would de-duplicate it and undercount by one.
	dual string
}

func seedAggregateFixture(t *testing.T, l *liveNeo4jScope) *aggregateFixture {
	t.Helper()
	f := &aggregateFixture{liveScopeFixture: seedLiveScopeFixture(t, l), provider: map[string]string{}, env: map[string]string{}}
	negative := map[string]bool{}
	for _, part := range []string{"cr-ungranted", "cr-orphan", "tsr-unmatched", "tsr-ungranted", "k8s-ungranted", "tf-other"} {
		negative[f.id(part)] = true
	}
	ids := make([]string, 0, len(f.labels))
	for id := range f.labels {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	providers := []string{"aws", "gcp", ""}
	envs := []string{"prod", "dev", "", "stage"}
	for i, id := range ids {
		p, e := providers[i%len(providers)], envs[i%len(envs)]
		if negative[id] {
			p, e = ungrantedBucket, ungrantedBucket
		}
		// The all-categories provider bucket reads provider, and for a
		// CloudResource source_system; see infraResourceProviderGroupExpression.
		key := "provider"
		if f.labels[id] == "CloudResource" {
			key = "source_system"
		}
		props := map[string]any{}
		if p != "" {
			props[key] = p
		}
		if e != "" {
			props["environment"] = e
		}
		if len(props) > 0 {
			l.raw(t, 60*time.Second, "MATCH (n:"+f.labels[id]+" {id: $id}) SET n += $props", map[string]any{"id": id, "props": props})
		}
		if p == "" {
			p = "unknown"
		}
		if e == "" {
			e = "unknown"
		}
		f.provider[id], f.env[id] = p, e
	}
	// tf-repo-00 is direct-owned by a repo every grant includes, so it is
	// admitted at g1, g5 and the cap. Its second label is applied after the
	// property seeding above so provider and environment stay keyed off its
	// fixture label (TerraformResource).
	f.dual = f.id("tf-repo-00")
	l.raw(t, 60*time.Second, "MATCH (n:TerraformResource {id: $id}) SET n:CloudResource", map[string]any{"id": f.dual})
	return f
}

// aggregateAnswer is one count answer normalized for comparison.
type aggregateAnswer struct {
	Total       int
	ByProvider  map[string]int
	ByEnv       map[string]int
	ByLabel     map[string]int
	Inventories map[string]map[string]int // group_by -> bucket -> count
}

func (f *aggregateFixture) oracle(g dialectGrant) aggregateAnswer {
	a := aggregateAnswer{ByProvider: map[string]int{}, ByEnv: map[string]int{}, ByLabel: map[string]int{}}
	for _, id := range f.expectedSearch(g) {
		// One count per matching label branch: a dual-labeled node is two.
		branches := 1
		if id == f.dual {
			branches = 2
		}
		a.Total += branches
		a.ByProvider[f.provider[id]] += branches
		a.ByEnv[f.env[id]] += branches
		// head(labels(n)) is the fixture label, in creation order.
		a.ByLabel[f.labels[id]] += branches
	}
	return a
}

func intMap(t *testing.T, v any) map[string]int {
	t.Helper()
	out := map[string]int{}
	m, _ := v.(map[string]any)
	for k, c := range m {
		out[k] = int(c.(float64))
	}
	return out
}

// handlerAggregate serves count plus the three inventory groupings through
// the real handler on the real reader with the Neo4j dialect.
func (l *liveNeo4jScope) handlerAggregate(t *testing.T, g dialectGrant) aggregateAnswer {
	t.Helper()
	code, raw, elapsed := l.liveHandlerRequest(t, querycontract.GraphBackendNeo4j, g, infraCountPath, "")
	requireStatus(t, "count "+g.name, code, http.StatusOK, raw)
	data := decodeInfraData(t, raw)
	a := aggregateAnswer{
		Total:       int(data["total_resources"].(float64)),
		ByProvider:  intMap(t, data["by_provider"]),
		ByEnv:       intMap(t, data["by_environment"]),
		ByLabel:     intMap(t, data["by_label"]),
		Inventories: map[string]map[string]int{},
	}
	t.Logf("count %s: handler %.2fs", g.name, elapsed.Seconds())
	for _, dim := range []string{"provider", "environment", "label"} {
		path := "/api/v0/infra/resources/inventory?limit=500&group_by=" + dim
		code, raw, elapsed := l.liveHandlerRequest(t, querycontract.GraphBackendNeo4j, g, path, "")
		requireStatus(t, "inventory "+dim+" "+g.name, code, http.StatusOK, raw)
		data := decodeInfraData(t, raw)
		if data["truncated"] != false {
			t.Fatalf("%s inventory %s truncated", g.name, dim)
		}
		buckets := map[string]int{}
		for _, b := range data["buckets"].([]any) {
			m := b.(map[string]any)
			buckets[m["value"].(string)] = int(m["count"].(float64))
		}
		a.Inventories[dim] = buckets
		t.Logf("inventory %s %s: handler %.2fs", dim, g.name, elapsed.Seconds())
	}
	return a
}

// shapeAAggregateStatements returns the SHAPE-A count statements for g. For
// g1 and g5 they are the exact NornicDB statements. At the cap the per-branch
// SHAPE-A form does not plan on Neo4j in reasonable time (#7215), so the
// SHAPE-A predicate is hoisted into the Neo4j statement, which keeps SHAPE-A
// admission exactly.
func shapeAAggregateStatements(t *testing.T, g dialectGrant) []recordedInfraCall {
	t.Helper()
	if g.name != "cap" {
		return captureStatements(t, querycontract.GraphBackendNornicDB, g, infraCountPath, "")
	}
	scalars, _ := g.filter().ScopeGrantInlineScalars()
	var out []recordedInfraCall
	for _, call := range captureStatements(t, querycontract.GraphBackendNeo4j, g, infraCountPath, "") {
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
		out = append(out, recordedInfraCall{Cypher: cypher, Params: params})
	}
	return out
}

// shapeAAggregate runs the SHAPE-A count statements outside the handler budget
// and reduces them with the production merge.
func shapeAAggregate(t *testing.T, l *liveNeo4jScope, g dialectGrant) aggregateAnswer {
	t.Helper()
	calls := shapeAAggregateStatements(t, g)
	if len(calls) != 4 {
		t.Fatalf("SHAPE-A count statements = %d, want 4", len(calls))
	}
	a := aggregateAnswer{}
	for _, row := range l.raw(t, shapeAReferenceTimeout, calls[0].Cypher, calls[0].Params) {
		a.Total += IntVal(row, "bucket_count")
	}
	a.ByProvider = mergeInfraResourceAggregateBuckets(l.raw(t, shapeAReferenceTimeout, calls[1].Cypher, calls[1].Params))
	a.ByEnv = mergeInfraResourceAggregateBuckets(l.raw(t, shapeAReferenceTimeout, calls[2].Cypher, calls[2].Params))
	a.ByLabel = mergeInfraResourceAggregateBuckets(l.raw(t, shapeAReferenceTimeout, calls[3].Cypher, calls[3].Params))
	return a
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// TestLiveInfraScopeNeo4jAggregateEquivalenceAndNoLeak proves the Neo4j scoped
// count (total and the provider, environment and label rollups) and the
// inventory buckets equal SHAPE-A and an oracle at g1, g5 and the cap, and
// that no negative node (a CloudResource used only by an ungranted instance,
// an orphan CloudResource, an unmatched or ungranted-matched state resource,
// an ungranted-owned node) is ever counted.
func TestLiveInfraScopeNeo4jAggregateEquivalenceAndNoLeak(t *testing.T) {
	l := openLiveNeo4jScope(t)
	f := seedAggregateFixture(t, l)
	for _, g := range f.grants() {
		want := f.oracle(g)
		if want.Total == 0 {
			t.Fatalf("%s: empty oracle; the proof would be vacuous", g.name)
		}
		if !f.admitted(f.dual, g) {
			t.Fatalf("%s: the dual-labeled node is not admitted; the UNION ALL proof would be vacuous", g.name)
		}
		got := l.handlerAggregate(t, g)
		for name, pair := range map[string][2]any{
			"total":          {got.Total, want.Total},
			"by_provider":    {got.ByProvider, want.ByProvider},
			"by_environment": {got.ByEnv, want.ByEnv},
			"by_label":       {got.ByLabel, want.ByLabel},
			"inv/provider":   {got.Inventories["provider"], want.ByProvider},
			"inv/env":        {got.Inventories["environment"], want.ByEnv},
			"inv/label":      {got.Inventories["label"], want.ByLabel},
		} {
			if !reflect.DeepEqual(pair[0], pair[1]) {
				t.Fatalf("%s %s: Neo4j %v != oracle %v", g.name, name, mustJSON(pair[0]), mustJSON(pair[1]))
			}
		}
		ref := shapeAAggregate(t, l, g)
		if ref.Total != got.Total || !reflect.DeepEqual(ref.ByProvider, got.ByProvider) ||
			!reflect.DeepEqual(ref.ByEnv, got.ByEnv) || !reflect.DeepEqual(ref.ByLabel, got.ByLabel) {
			t.Fatalf("%s: Neo4j differs from SHAPE-A\n neo4j  %s\n shapeA %s", g.name, mustJSON(got), mustJSON(ref))
		}
		for _, m := range []map[string]int{got.ByProvider, got.ByEnv, got.Inventories["provider"], got.Inventories["environment"]} {
			if _, leaked := m[ungrantedBucket]; leaked {
				t.Fatalf("%s: a negative node was counted: %s", g.name, mustJSON(got))
			}
		}
		t.Logf("aggregate %s: total=%d == oracle == SHAPE-A; provider=%s env=%s label=%s",
			g.name, got.Total, mustJSON(got.ByProvider), mustJSON(got.ByEnv), mustJSON(got.ByLabel))
	}
}

// TestLiveInfraScopeNeo4jAggregateColdBudget proves the scoped count and
// inventory answer inside the 10 s bounded read with a cleared plan cache,
// at g5 and at the cap, and logs cold and warm wall times. With
// ESHU_INFRA_SCOPE_NEO4J_TIMING_BEFORE=1 it also times the SHAPE-A count
// statements the handler sent before #7231, outside the handler, bounded by
// ESHU_INFRA_SCOPE_NEO4J_BEFORE_BOUND_S (default 120 s) per statement.
func TestLiveInfraScopeNeo4jAggregateColdBudget(t *testing.T) {
	l := openLiveNeo4jScope(t)
	f := seedAggregateFixture(t, l)
	clear := func() { l.raw(t, 60*time.Second, "CALL db.clearQueryCaches()", nil) }
	routes := []struct{ name, path string }{
		{"count", infraCountPath},
		{"inventory", "/api/v0/infra/resources/inventory?group_by=provider"},
	}
	grants := []dialectGrant{f.grant("g5", 5), f.grant("cap", 70)}
	for _, g := range grants {
		for _, r := range routes {
			var cold, warm []string
			for i := 0; i < 3; i++ {
				clear()
				for _, sample := range []*[]string{&cold, &warm} {
					code, raw, elapsed := l.liveHandlerRequest(t, querycontract.GraphBackendNeo4j, g, r.path, "")
					requireStatus(t, "after "+g.name+" "+r.name, code, http.StatusOK, raw)
					if elapsed >= 10*time.Second {
						t.Fatalf("after %s %s: %.2fs, over the 10 s budget", g.name, r.name, elapsed.Seconds())
					}
					*sample = append(*sample, fmt.Sprintf("%.2f", elapsed.Seconds()))
				}
			}
			t.Logf("AFTER  %-4s %-9s cold_s=%v warm_s=%v", g.name, r.name, cold, warm)
		}
	}
	if os.Getenv("ESHU_INFRA_SCOPE_NEO4J_TIMING_BEFORE") == "" {
		return
	}
	bound := 120 * time.Second
	if v := os.Getenv("ESHU_INFRA_SCOPE_NEO4J_BEFORE_BOUND_S"); v != "" {
		if d, err := time.ParseDuration(v + "s"); err == nil && d > 0 {
			bound = d
		}
	}
	// Before runs last so a plan that blows the bound cannot skew the after.
	for _, g := range grants {
		for _, r := range routes {
			statements := captureStatements(t, querycontract.GraphBackendNornicDB, g, r.path, "")
			var cold []string
			for i := 0; i < 3; i++ {
				clear()
				cold = append(cold, runAllStatements(l, statements, fmt.Sprintf("before7231-%d", time.Now().UnixNano()), bound))
			}
			t.Logf("BEFORE %-4s %-9s cold=%v (%d statements, SHAPE-A outside the handler)", g.name, r.name, cold, len(statements))
		}
	}
}

// runAllStatements runs every statement in order (the aggregate routes send
// all of them) and returns the total wall time, or DNF.
func runAllStatements(l *liveNeo4jScope, statements []recordedInfraCall, nonce string, bound time.Duration) string {
	start := time.Now()
	for i, call := range statements {
		if _, err := l.rawErr(bound, "/* "+nonce+" */ "+call.Cypher, call.Params); err != nil {
			return fmt.Sprintf("DNF after %.1fs at statement %d (%s)", time.Since(start).Seconds(), i+1, firstLine(err.Error()))
		}
	}
	return fmt.Sprintf("%.2fs", time.Since(start).Seconds())
}
