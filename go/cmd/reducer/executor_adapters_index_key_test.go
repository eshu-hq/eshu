// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestReducerCypherExecutorSkipsOversizedIndexKeys covers the one reducer
// write chain with no sourcecypher.InstrumentedExecutor: the workload and
// platform materializers. An oversized Workload.id row must not reach the
// backend, and the rest of the batch must (#7058).
func TestReducerCypherExecutorSkipsOversizedIndexKeys(t *testing.T) {
	t.Parallel()

	session := &runCypherOnlySession{}
	exec := newReducerCypherExecutor(session, nil, nil)
	cypher := "UNWIND $rows AS row\nMERGE (w:Workload {id: row.workload_id})\nSET w.name = row.name"
	big := strings.Repeat("w", 9000)
	rows := []map[string]any{
		{"workload_id": "workload:ok", "name": "ok"},
		{"workload_id": big, "name": "big"},
	}
	if err := exec.ExecuteCypher(context.Background(), cypher, map[string]any{"rows": rows}); err != nil {
		t.Fatalf("ExecuteCypher() error = %v", err)
	}
	if err := exec.ExecuteCypherGroup(context.Background(), []reducer.CypherGroupStatement{
		{Cypher: cypher, Parameters: map[string]any{"rows": rows}},
		{Cypher: "MERGE (w:Workload {id: $id})", Parameters: map[string]any{"id": big}},
	}); err != nil {
		t.Fatalf("ExecuteCypherGroup() error = %v", err)
	}
	if len(session.calls) != 2 {
		t.Fatalf("runner calls = %d, want 2 (the scalar oversized group member is skipped)", len(session.calls))
	}
	for _, call := range session.calls {
		got := call.Parameters["rows"].([]map[string]any)
		if len(got) != 1 || got[0]["workload_id"] != "workload:ok" {
			t.Fatalf("rows reaching the runner = %v, want only workload:ok", got)
		}
	}
}
