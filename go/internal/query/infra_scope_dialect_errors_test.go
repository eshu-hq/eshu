// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// errScriptGraph is a GraphQuery whose RunSingle answer, and error, are
// scripted per statement. It records every call in order together with the
// ctx deadline so the #7215 Neo4j anchor loop's failure and budget behaviour
// can be asserted.
type errScriptGraph struct {
	mu        sync.Mutex
	statement []string
	deadlines []time.Time
	hasDL     []bool
	single    func(cypher string) (map[string]any, error)
}

func (g *errScriptGraph) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	return nil, nil
}

func (g *errScriptGraph) RunSingle(ctx context.Context, cypher string, _ map[string]any) (map[string]any, error) {
	deadline, ok := ctx.Deadline()
	g.mu.Lock()
	g.statement = append(g.statement, cypher)
	g.deadlines = append(g.deadlines, deadline)
	g.hasDL = append(g.hasDL, ok)
	g.mu.Unlock()
	return g.single(cypher)
}

// TestInfraRelationshipsNeo4jScopedReadErrorsFailClosed proves a probe error,
// a scoped-read error and a spent shared deadline each end the request with a
// graph-read error and no further graph read. A swallowed probe error would
// turn a granted entity into a false 404.
func TestInfraRelationshipsNeo4jScopedReadErrorsFailClosed(t *testing.T) {
	t.Parallel()
	auth := dialectGrants()[0].auth()
	// The third label is the failing one: two clean probe misses come first.
	failLabel := impactRelationshipAnchorLabels[2]

	cases := []struct {
		name       string
		err        error
		wantStatus int
		// scoped selects whether the error is raised by the scoped statement
		// (after a probe hit) instead of the probe.
		scoped bool
		// wantCalls is the exact number of graph reads before the request
		// must stop.
		wantCalls int
	}{
		{name: "probe_unavailable", err: querycontract.ErrGraphUnavailable, wantStatus: http.StatusServiceUnavailable, wantCalls: 3},
		{name: "probe_deadline", err: fmt.Errorf("probe: %w", querycontract.ErrGraphReadDeadline), wantStatus: http.StatusGatewayTimeout, wantCalls: 3},
		{name: "probe_raw_deadline", err: context.DeadlineExceeded, wantStatus: http.StatusGatewayTimeout, wantCalls: 3},
		{name: "scoped_unavailable", err: querycontract.ErrGraphUnavailable, wantStatus: http.StatusServiceUnavailable, scoped: true, wantCalls: 4},
		{name: "scoped_deadline", err: fmt.Errorf("scoped: %w", querycontract.ErrGraphReadDeadline), wantStatus: http.StatusGatewayTimeout, scoped: true, wantCalls: 4},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			graph := &errScriptGraph{single: func(cypher string) (map[string]any, error) {
				onFailLabel := strings.Contains(cypher, "(n:"+failLabel+")")
				switch {
				case isAnchorProbe(cypher) && onFailLabel && !tc.scoped:
					return nil, tc.err
				case isAnchorProbe(cypher) && onFailLabel && tc.scoped:
					return map[string]any{"hit": int64(1)}, nil
				case !isAnchorProbe(cypher) && onFailLabel && tc.scoped:
					return nil, tc.err
				}
				return nil, nil
			}}
			rec := serveInfraDialect(t, querycontract.GraphBackendNeo4j, &auth, graph, infraRelationshipsPath, `{"entity_id":"granted-1"}`)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (a graph-read error, never 200/404); body = %s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if got := len(graph.statement); got != tc.wantCalls {
				t.Fatalf("graph reads = %d, want exactly %d (no read after the error, no unscoped fallback):\n%s",
					got, tc.wantCalls, strings.Join(graph.statement, "\n---\n"))
			}
			if last := graph.statement[len(graph.statement)-1]; !strings.Contains(last, "(n:"+failLabel+")") {
				t.Fatalf("the failing statement must be the last read, got:\n%s", last)
			}
		})
	}
}

// TestInfraRelationshipsNeo4jScopedLoopSharesOneBoundedDeadline proves every
// RunSingle in the probe/scoped/fallback sequence carries a deadline and that
// all of them carry the same one, so the loop cannot spend more than one
// bounded graph-read budget. The request context has no deadline of its own.
func TestInfraRelationshipsNeo4jScopedLoopSharesOneBoundedDeadline(t *testing.T) {
	t.Parallel()
	auth := dialectGrants()[0].auth()
	hitLabel := impactRelationshipAnchorLabels[1]
	graph := &errScriptGraph{single: func(cypher string) (map[string]any, error) {
		if isAnchorProbe(cypher) && strings.Contains(cypher, "(n:"+hitLabel+")") {
			return map[string]any{"hit": int64(1)}, nil // hit, then the scoped read misses
		}
		return nil, nil
	}}
	rec := serveInfraDialect(t, querycontract.GraphBackendNeo4j, &auth, graph, infraRelationshipsPath, `{"entity_id":"ungranted-1"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", rec.Code, rec.Body.String())
	}
	// every probe + one scoped labeled read + the scoped unlabeled fallback
	if want := len(impactRelationshipAnchorLabels) + 2; len(graph.statement) != want {
		t.Fatalf("graph reads = %d, want %d", len(graph.statement), want)
	}
	for i, ok := range graph.hasDL {
		if !ok {
			t.Fatalf("read %d had no deadline on its ctx; every read must carry the bounded graph-read deadline", i)
		}
	}
	for i, d := range graph.deadlines[1:] {
		if !d.Equal(graph.deadlines[0]) {
			t.Fatalf("read %d deadline %s != read 0 deadline %s; the loop must share one budget", i+1, d, graph.deadlines[0])
		}
	}
	if until := time.Until(graph.deadlines[0]); until > 5*time.Minute {
		t.Fatalf("shared deadline is %s away; it must be the bounded graph-read budget", until)
	}
}
