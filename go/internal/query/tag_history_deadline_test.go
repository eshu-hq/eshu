// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// tagHistoryDeadlineGraph is a GraphQuery whose every Run blocks until ctx is
// done and then returns the raw ctx.Err(), exactly as Neo4jReader.runRead does
// when its PARENT context is already expired (graphReadResult's
// parentCtx.Err() branch) rather than the graph-read policy's own bounded
// budget firing. Any GraphQuery implementation that bypasses Neo4jReader --
// this fake, or a future backend -- can surface that same raw
// context.DeadlineExceeded, so the handler must translate it itself rather
// than assuming every caller already wraps it as
// querycontract.ErrGraphReadDeadline.
type tagHistoryDeadlineGraph struct{}

func (tagHistoryDeadlineGraph) Run(ctx context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (tagHistoryDeadlineGraph) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, nil
}

// TestTagHistoryScopedReadTranslatesASpentBudgetToDeadlineResponse pins the
// #6705 handler-side half of the shared-deadline fix: writeTagHistoryReadError
// must map a raw context.DeadlineExceeded from RefillScopedPage's graph read
// to the existing 504 graph-read-deadline shape (WriteGraphReadError), never a
// generic 500 and never a silent success. Without the translation, a
// GraphQuery implementation that returns a raw context.DeadlineExceeded (any
// backend that does not already wrap it, or a fake standing in for one) falls
// through WriteGraphReadError's errors.Is(err, querycontract.ErrGraphReadDeadline)
// check to the handler's generic 500 branch.
func TestTagHistoryScopedReadTranslatesASpentBudgetToDeadlineResponse(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	handler := &TagHistoryHandler{
		Neo4j:   tagHistoryDeadlineGraph{},
		Profile: ProfileLocalAuthoritative,
		Cursors: tagHistoryTestCursorKeyring,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := newTagHistoryRequest(tagHistoryGrantTarget)
	req = req.WithContext(ContextWithAuthContext(ctx, *scopedTagHistoryAuth("repo-granted")))
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusGatewayTimeout; got != want {
		t.Fatalf("status = %d, want %d (504 deadline shape); a spent shared budget must never resolve to a "+
			"generic 500 or a silent success; body = %s", got, want, w.Body.String())
	}
}
