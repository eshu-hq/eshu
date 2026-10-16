// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fulltext_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/query/infra/fulltext"
)

// probeGraph answers the readiness probes: the SHOW INDEXES state and the
// per-label unkeyed counts.
type probeGraph struct {
	mu        sync.Mutex
	state     string // "" means the index is missing
	unkeyed   int64  // reported for the first infra label only
	stateErr  error
	countErr  error
	calls     int
	countSeen int
}

func (g *probeGraph) Run(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls++
	if strings.HasPrefix(cypher, "SHOW INDEXES") {
		if g.stateErr != nil {
			return nil, g.stateErr
		}
		if g.state == "" {
			return nil, nil
		}
		return []map[string]any{{"state": g.state}}, nil
	}
	if strings.Contains(cypher, "AS unkeyed") {
		if g.countErr != nil {
			return nil, g.countErr
		}
		g.countSeen++
		if strings.Contains(cypher, "(n:"+graph.InfraSearchKeyLabels()[0]+")") {
			return []map[string]any{{"unkeyed": g.unkeyed}}, nil
		}
		return []map[string]any{{"unkeyed": int64(0)}}, nil
	}
	return nil, errors.New("unexpected statement: " + cypher)
}

func (g *probeGraph) set(state string, unkeyed int64, stateErr error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.state, g.unkeyed, g.stateErr, g.countSeen = state, unkeyed, stateErr, 0
}

func TestReadinessRefresh(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		state       string
		unkeyed     int64
		stateErr    error
		countErr    error
		wantReady   bool
		wantIndex   fulltext.IndexState
		wantReason  fulltext.Reason
		wantUnkeyed int64
	}{
		{"online and keyed", "ONLINE", 0, nil, nil, true, fulltext.IndexOnline, fulltext.ReasonReady, 0},
		{"online with unkeyed", "ONLINE", 7, nil, nil, false, fulltext.IndexOnline, fulltext.ReasonUnkeyed, 7},
		{"populating", "POPULATING", 0, nil, nil, false, fulltext.IndexPopulating, fulltext.ReasonIndexNotOnline, 0},
		{"failed", "FAILED", 0, nil, nil, false, fulltext.IndexFailed, fulltext.ReasonIndexNotOnline, 0},
		{"missing", "", 0, nil, nil, false, fulltext.IndexMissing, fulltext.ReasonIndexNotOnline, 0},
		{"odd state", "SOMETHING", 0, nil, nil, false, fulltext.IndexUnknown, fulltext.ReasonIndexNotOnline, 0},
		{"state probe error", "ONLINE", 0, errors.New("boom"), nil, false, fulltext.IndexUnknown, fulltext.ReasonNotReady, 0},
		{"count probe error", "ONLINE", 0, nil, errors.New("boom"), false, fulltext.IndexOnline, fulltext.ReasonNotReady, 0},
	}
	for _, c := range cases {
		graph := &probeGraph{state: c.state, unkeyed: c.unkeyed, stateErr: c.stateErr, countErr: c.countErr}
		r := fulltext.NewReadiness(graph, nil, nil)
		got := r.Refresh(context.Background())
		if got.Ready != c.wantReady || got.Index != c.wantIndex || got.Reason != c.wantReason || got.Unkeyed != c.wantUnkeyed {
			t.Errorf("%s: Refresh() = %+v, want ready=%v index=%s reason=%s unkeyed=%d",
				c.name, got, c.wantReady, c.wantIndex, c.wantReason, c.wantUnkeyed)
		}
		if cur := r.Current(); cur.Ready != got.Ready || cur.Reason != got.Reason {
			t.Errorf("%s: Current() = %+v, want %+v", c.name, cur, got)
		}
	}
}

func TestReadinessNilAndUnprobedAreNotReady(t *testing.T) {
	t.Parallel()
	var nilReadiness *fulltext.Readiness
	if st := nilReadiness.Current(); st.Ready || st.Reason != fulltext.ReasonNotReady {
		t.Fatalf("nil Current() = %+v, want not ready", st)
	}
	r := fulltext.NewReadiness(&probeGraph{state: "ONLINE"}, nil, nil)
	if st := r.Current(); st.Ready || st.Reason != fulltext.ReasonNotReady {
		t.Fatalf("unprobed Current() = %+v, want not ready", st)
	}
}

// A ready state must flip to not ready when the next probe fails: a stale
// "ready" must never outlive a failed probe.
func TestReadinessProbeErrorDropsReady(t *testing.T) {
	t.Parallel()
	graph := &probeGraph{state: "ONLINE"}
	r := fulltext.NewReadiness(graph, nil, nil)
	if !r.Refresh(context.Background()).Ready {
		t.Fatal("first refresh not ready")
	}
	graph.set("ONLINE", 0, errors.New("connection reset"))
	if st := r.Refresh(context.Background()); st.Ready || r.Current().Ready {
		t.Fatalf("after probe error state = %+v, want not ready", st)
	}
}

func TestReadinessLogsOnlyTransitions(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	graph := &probeGraph{state: "ONLINE"}
	r := fulltext.NewReadiness(graph, logger, nil)
	r.Refresh(context.Background())
	r.Refresh(context.Background())
	graph.set("ONLINE", 4, nil)
	r.Refresh(context.Background())
	r.Refresh(context.Background())
	lines := strings.Count(buf.String(), "query.infra_search_key.readiness_changed")
	if lines != 2 {
		t.Fatalf("transition log lines = %d, want 2 (first state and ready->unkeyed):\n%s", lines, buf.String())
	}
}

func TestReadinessWatchRefreshesUntilCanceled(t *testing.T) {
	t.Parallel()
	graph := &probeGraph{state: "ONLINE"}
	r := fulltext.NewReadiness(graph, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		r.Watch(ctx, 5*time.Millisecond)
		close(done)
	}()
	deadline := time.After(5 * time.Second)
	for {
		graph.mu.Lock()
		calls := graph.calls
		graph.mu.Unlock()
		if calls >= 3*28 && r.Current().Ready {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("Watch did not refresh repeatedly: calls=%d", calls)
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Watch did not return after cancel")
	}
}
