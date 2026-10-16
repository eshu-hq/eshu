// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package search

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/graph"
)

// TestIndexStateReadsTheSearchKeyIndexByName proves IndexState reads the
// state of infra_search_key_ft only, reports a missing index as "", and
// returns probe errors.
func TestIndexStateReadsTheSearchKeyIndexByName(t *testing.T) {
	t.Parallel()

	runner := &recordingRunner{rows: func(string) []map[string]any {
		return []map[string]any{{"state": "ONLINE"}}
	}}
	state, err := IndexState(context.Background(), runner)
	if err != nil || state != "ONLINE" {
		t.Fatalf("IndexState() = %q, %v; want ONLINE, nil", state, err)
	}
	if len(runner.calls) != 1 || runner.calls[0].params["name"] != graph.InfraSearchKeyIndexName {
		t.Fatalf("calls = %+v, want one read of %s", runner.calls, graph.InfraSearchKeyIndexName)
	}

	missing, err := IndexState(context.Background(), &recordingRunner{})
	if err != nil || missing != "" {
		t.Fatalf("missing index IndexState() = %q, %v; want empty, nil", missing, err)
	}

	if _, err := IndexState(context.Background(), &recordingRunner{err: errors.New("boom")}); err == nil {
		t.Fatal("IndexState() error = nil, want probe error")
	}
}
