// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package search

import (
	"context"
	"errors"
	"strings"
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

// TestCountUnkeyedSearchableCountsOnlyNodesTheSearchCanMatch proves the
// readiness count adds, per infra label, only unkeyed nodes that hold at
// least one searched field: a node a posture writer created with no searched
// field can never match the search, so it must not hold readiness off.
func TestCountUnkeyedSearchableCountsOnlyNodesTheSearchCanMatch(t *testing.T) {
	t.Parallel()

	runner := &recordingRunner{rows: func(string) []map[string]any {
		return []map[string]any{{"unkeyed": int64(2)}}
	}}
	counts, err := CountUnkeyedSearchable(context.Background(), runner)
	if err != nil {
		t.Fatal(err)
	}
	labels := graph.InfraSearchKeyLabels()
	if len(runner.calls) != len(labels) || len(counts) != len(labels) {
		t.Fatalf("calls = %d, counts = %d, want one per label (%d)", len(runner.calls), len(counts), len(labels))
	}
	for i, label := range labels {
		cypher := runner.calls[i].cypher
		if !strings.Contains(cypher, "MATCH (n:"+label+")") || counts[label] != 2 {
			t.Fatalf("label %s: statement %q count %d", label, cypher, counts[label])
		}
		if !strings.Contains(cypher, "n."+graph.InfraSearchOverflowProperty+" IS NULL") {
			t.Fatalf("label %s: statement does not require a missing flag: %q", label, cypher)
		}
		for _, field := range Fields() {
			if !strings.Contains(cypher, "n."+field+" IS NOT NULL") {
				t.Fatalf("label %s: statement does not test searched field %s: %q", label, field, cypher)
			}
		}
	}

	if _, err := CountUnkeyedSearchable(context.Background(), &recordingRunner{err: errors.New("boom")}); err == nil {
		t.Fatal("CountUnkeyedSearchable() error = nil, want probe error")
	}
}
