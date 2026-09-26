// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build live_infra_scope_neo4j

package query

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestLiveInfraScopeNeo4jColdWarmBudget measures the #7215 before/after on
// the proof's graph and grants: g5 (5 repos + 5 git-repository-scope ids) and
// the cap (64 + 64 = 128 scalars), search {"query":"api"} and relationships on
// a 6th-label hit and a full miss.
//
// After: the real handler on the real reader (10 s budget) with the Neo4j
// dialect; cold = CALL db.clearQueryCaches() first, warm = the same request
// again. Every run must answer (200 or 404) inside the budget.
//
// Before (ESHU_INFRA_SCOPE_NEO4J_TIMING_BEFORE=1): the SHAPE-A statements the
// handler sends today, run outside the handler with a unique /* nonce */
// prefix, bounded by ESHU_INFRA_SCOPE_NEO4J_BEFORE_BOUND_S (default 120 s)
// per statement; a statement that does not finish is reported DNF.
//
// Seed the graph with the proof's seed.py first; the grants name its repos.
func TestLiveInfraScopeNeo4jColdWarmBudget(t *testing.T) {
	if os.Getenv("ESHU_INFRA_SCOPE_NEO4J_TIMING") == "" {
		t.Skip("set ESHU_INFRA_SCOPE_NEO4J_TIMING=1 to run the #7215 cold/warm measurement")
	}
	l := openLiveNeo4jScope(t)
	reps := 3
	bound := 120 * time.Second
	if v, err := strconv.Atoi(os.Getenv("ESHU_INFRA_SCOPE_NEO4J_BEFORE_BOUND_S")); err == nil && v > 0 {
		bound = time.Duration(v) * time.Second
	}
	nodes := l.raw(t, 60*time.Second, "MATCH (n) RETURN count(n) AS c", nil)
	t.Logf("graph nodes = %v", nodes[0]["c"])

	proofGrant := func(name string, n int) dialectGrant {
		g := dialectGrant{name: name}
		for i := 0; i < n; i++ {
			g.repos = append(g.repos, fmt.Sprintf("repo-%02d", i))
			g.scopes = append(g.scopes, fmt.Sprintf("git-repository-scope:%02d", i))
		}
		return g
	}
	type timingCase struct{ route, label, path, body string }
	cases := []timingCase{
		{"search", "api", infraSearchPath, `{"query":"api"}`},
		{"relationships", "hit-6th-label", infraRelationshipsPath, `{"entity_id":"TerraformStateResource:0:0"}`},
		{"relationships", "miss", infraRelationshipsPath, `{"entity_id":"does-not-exist"}`},
	}
	clear := func() { l.raw(t, 60*time.Second, "CALL db.clearQueryCaches()", nil) }

	for _, g := range []dialectGrant{proofGrant("g5", 5), proofGrant("cap", 64)} {
		for _, c := range cases {
			var cold, warm []string
			for i := 0; i < reps; i++ {
				clear()
				for _, sample := range []*[]string{&cold, &warm} {
					code, raw, elapsed := l.liveHandlerRequest(t, querycontract.GraphBackendNeo4j, g, c.path, c.body)
					if code != http.StatusOK && code != http.StatusNotFound {
						t.Fatalf("after %s %s %s: status %d (a 504 is the 10 s budget): %.400s", g.name, c.route, c.label, code, raw)
					}
					if elapsed >= 10*time.Second {
						t.Fatalf("after %s %s %s: %.2fs, over the 10 s budget", g.name, c.route, c.label, elapsed.Seconds())
					}
					*sample = append(*sample, fmt.Sprintf("%.2f", elapsed.Seconds()))
				}
			}
			t.Logf("AFTER  %-4s %-13s %-13s cold_s=%v warm_s=%v", g.name, c.route, c.label, cold, warm)
		}
	}

	if os.Getenv("ESHU_INFRA_SCOPE_NEO4J_TIMING_BEFORE") == "" {
		return
	}
	// Before runs last: a SHAPE-A plan that blows the bound keeps the server
	// planning until the transaction timeout, which would skew any later run.
	for _, g := range []dialectGrant{proofGrant("g5", 5), proofGrant("cap", 64)} {
		for _, c := range cases {
			statements := captureStatements(t, querycontract.GraphBackendNornicDB, g, c.path, c.body)
			nonce := fmt.Sprintf("before7215-%d", time.Now().UnixNano())
			clear()
			cold := runShapeALoop(l, statements, nonce, bound)
			warm := runShapeALoop(l, statements, nonce, bound)
			t.Logf("BEFORE %-4s %-13s %-13s cold=%s warm=%s (%d statements available)", g.name, c.route, c.label, cold, warm, len(statements))
		}
	}
}

// runShapeALoop runs the statements in order, stopping at the first that
// returns a row (the handler's first-non-null loop), and returns the total
// wall time or DNF.
func runShapeALoop(l *liveNeo4jScope, statements []recordedInfraCall, nonce string, bound time.Duration) string {
	start := time.Now()
	for i, call := range statements {
		rows, err := l.rawErr(bound, "/* "+nonce+" */ "+call.Cypher, call.Params)
		if err != nil {
			return fmt.Sprintf("DNF after %.1fs at statement %d (%s)", time.Since(start).Seconds(), i+1, firstLine(err.Error()))
		}
		if len(rows) > 0 {
			break
		}
	}
	return fmt.Sprintf("%.2fs", time.Since(start).Seconds())
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 120 {
		s = s[:120]
	}
	return s
}
