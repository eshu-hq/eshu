// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// TestGetEntityContextAnchorLoopSharesOneDeadlineAcrossLabels pins the
// #7006 review's P1 finding: the EntityContextAnchorLabels loop must derive
// ONE deadline before its first iteration and reuse it for every
// subsequent one -- never let each RunSingle call start its own fresh
// Neo4jReader.runRead window. Without a shared budget, production traffic
// (whose raw request context, per the review, carries no deadline of its
// own -- cmd/api's http.Server sets no read/handler timeout for this route)
// would let a genuinely-absent entity, or one whose label sits late in the
// try order, pay up to (len(EntityContextAnchorLabels)+1) x the single-read
// budget (~160s for 16 anchors at 10s each) instead of the one bounded-read
// budget the pre-fix single-statement handler had.
//
// The request context here carries NO deadline (matching the review's
// documented production reality), so any deadline the fake observes must
// come from the handler itself. The test asserts every call sees a deadline
// (a bound was applied at all) and that it is the SAME instant on every
// call (one shared budget, not a fresh one per label).
func TestGetEntityContextAnchorLoopSharesOneDeadlineAcrossLabels(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var deadlines []time.Time
	reader := graph.FakeGraphReader{
		RunSingleFn: func(ctx context.Context, _ string, _ map[string]any) (map[string]any, error) {
			deadline, ok := ctx.Deadline()
			if !ok {
				t.Error("RunSingle ctx has no deadline; the anchor loop must derive one shared bounded-read deadline before it starts")
			}
			mu.Lock()
			deadlines = append(deadlines, deadline)
			mu.Unlock()
			return nil, nil // a genuine per-label miss
		},
	}
	handler := &Handler{Neo4j: reader, Profile: querycontract.ProfileLocalAuthoritative}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/entities/entity-a/context", nil)
	req.SetPathValue("entity_id", "entity-a")
	rec := httptest.NewRecorder()

	handler.GetEntityContext(rec, req)

	if len(deadlines) < 2 {
		t.Fatalf("captured %d RunSingle deadlines, want at least 2 (the fake misses every label, so every candidate must be tried)", len(deadlines))
	}
	first := deadlines[0]
	for i, d := range deadlines[1:] {
		if !d.Equal(first) {
			t.Fatalf("call %d deadline = %s, want the same shared deadline as call 0 (%s) -- each call is getting its own fresh window instead of sharing one budget", i+1, d, first)
		}
	}
}

// TestGetEntityContextTranslatesASpentSharedBudgetToDeadlineResponse pins
// the review's other required behavior: when the shared budget is spent
// mid-loop, the handler must answer with the existing bounded-read deadline
// shape (504), never a silent not-found and never an unrelated 500. A
// request-scoped short deadline (simulating the shared budget already being
// exhausted, without waiting out the real 10s production window) makes the
// fake's ctx cancel quickly; the fake returns the raw context.DeadlineExceeded
// exactly as Neo4jReader.runRead does when its parent context is already
// expired (graphReadResult's parentCtx.Err() branch).
func TestGetEntityContextTranslatesASpentSharedBudgetToDeadlineResponse(t *testing.T) {
	t.Parallel()

	reader := graph.FakeGraphReader{
		RunSingleFn: func(ctx context.Context, _ string, _ map[string]any) (map[string]any, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	handler := &Handler{Neo4j: reader, Profile: querycontract.ProfileLocalAuthoritative}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/entities/entity-a/context", nil)
	req.SetPathValue("entity_id", "entity-a")
	ctx, cancel := context.WithTimeout(req.Context(), 5*time.Millisecond)
	defer cancel()
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handler.GetEntityContext(rec, req)

	if got, want := rec.Code, http.StatusGatewayTimeout; got != want {
		t.Fatalf("status = %d, want %d (504 deadline shape); a spent shared budget must never resolve to a silent not-found or an unrelated error", got, want)
	}
}
