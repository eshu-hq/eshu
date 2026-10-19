// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

// stubIndexRetirementReader answers the index-status retirement block from
// fixed counts, proving the handler renders counts only, never ids.
type stubIndexRetirementReader struct {
	summary statuspkg.RetirementSummary
	err     error
	calls   int
}

func (s *stubIndexRetirementReader) ReadRetirementSummary(context.Context) (statuspkg.RetirementSummary, error) {
	s.calls++
	return s.summary, s.err
}

// indexStatusRetirementMux wires an unscoped index-status handler whose
// graph holds two canonically-evidenced repositories and whose retirement
// store holds two pending tombstones.
func indexStatusRetirementMux(t *testing.T, summary statuspkg.RetirementSummary) (http.Handler, *indexStatusRepoGraph, *stubIndexRetirementReader) {
	t.Helper()
	graph := &indexStatusRepoGraph{repos: []string{indexStatusRepoA, indexStatusRepoB}}
	retirements := &stubIndexRetirementReader{summary: summary}
	handler := &StatusHandler{
		StatusReader: fakeStatusReader{snapshot: indexStatusOtherTenantSnapshot(time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC))},
		Neo4j:        graph,
		Retirements:  retirements,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	return mux, graph, retirements
}

// TestServeIndexStatusRetirementBlocks proves the #7766 Deliverable-1 read
// surface: repository_count_by_state splits the graph count into indexed
// versus stub from evidence_source, and the repository_retirements block
// carries per-state counts plus the oldest pending age, with no repository
// id anywhere in either block.
func TestServeIndexStatusRetirementBlocks(t *testing.T) {
	t.Parallel()

	mux, graph, retirements := indexStatusRetirementMux(t, statuspkg.RetirementSummary{
		CountsByState:           map[string]int64{"pending": 2},
		HasPending:              true,
		OldestPendingAgeSeconds: 90,
		HasCompleted:            false,
	})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v0/index-status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if got, want := payload["repository_count"], float64(2); got != want {
		t.Fatalf("repository_count = %#v, want %#v", got, want)
	}
	byState, ok := payload["repository_count_by_state"].(map[string]any)
	if !ok {
		t.Fatalf("repository_count_by_state missing: %s", rec.Body.String())
	}
	if byState["indexed"] != float64(2) || byState["stub"] != float64(0) {
		t.Fatalf("repository_count_by_state = %#v, want indexed 2 stub 0", byState)
	}
	block, ok := payload["repository_retirements"].(map[string]any)
	if !ok {
		t.Fatalf("repository_retirements missing: %s", rec.Body.String())
	}
	counts, ok := block["counts_by_state"].(map[string]any)
	if !ok || counts["pending"] != float64(2) {
		t.Fatalf("counts_by_state = %#v, want pending 2", block["counts_by_state"])
	}
	if block["oldest_pending_age_seconds"] != float64(90) {
		t.Fatalf("oldest_pending_age_seconds = %#v, want 90", block["oldest_pending_age_seconds"])
	}
	if _, ok := block["last_completed_at"]; ok {
		t.Fatalf("last_completed_at present with no completion: %#v", block)
	}
	if retirements.calls != 1 {
		t.Fatalf("retirement summary reads = %d, want exactly 1", retirements.calls)
	}
	if got := graph.callCount(); got != 2 {
		t.Fatalf("graph reads = %d, want 2 (count plus by-state)", got)
	}

	// Counts only, no ids: neither block may name a repository.
	byStateJSON, _ := json.Marshal(byState)
	blockJSON, _ := json.Marshal(block)
	for _, leaked := range []string{indexStatusRepoA, indexStatusRepoB, "rr_"} {
		if strings.Contains(string(byStateJSON), leaked) {
			t.Fatalf("count-by-state surface leaked %q: %s", leaked, byStateJSON)
		}
		if strings.Contains(string(blockJSON), leaked) {
			t.Fatalf("retirements surface leaked %q: %s", leaked, blockJSON)
		}
	}
}
