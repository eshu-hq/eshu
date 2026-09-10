// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package story

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
)

// The story behavior suites live in package codequery and drive the
// leaf through the staying forwarders. This file pins only the leaf's
// own pure contracts: direction expansion, depth bounds, paging, and
// the depth summary's raw-count rule.

func TestDirectionsExpandsBoth(t *testing.T) {
	t.Parallel()
	if got := Directions("both"); !reflect.DeepEqual(got, []string{"incoming", "outgoing"}) {
		t.Fatalf("Directions(both) = %v, want [incoming outgoing]", got)
	}
	if got := Directions("incoming"); !reflect.DeepEqual(got, []string{"incoming"}) {
		t.Fatalf("Directions(incoming) = %v, want [incoming]", got)
	}
}

func TestNormalizeMaxDepthBounds(t *testing.T) {
	t.Parallel()
	for depth, want := range map[int]int{-1: 5, 0: 5, 3: 3, 10: 10, 11: 10} {
		if got := NormalizeMaxDepth(depth); got != want {
			t.Errorf("NormalizeMaxDepth(%d) = %d, want %d", depth, got, want)
		}
	}
}

func TestLimitRowsAnswersEmptyForNonPositiveLimit(t *testing.T) {
	t.Parallel()
	rows := []map[string]any{{"id": "a"}, {"id": "b"}, {"id": "c"}}
	if got := LimitRows(rows, 0); len(got) != 0 {
		t.Fatalf("LimitRows(rows, 0) = %v, want empty", got)
	}
	if got := LimitRows(rows, 2); len(got) != 2 {
		t.Fatalf("LimitRows(rows, 2) has %d rows, want 2", len(got))
	}
	if got := LimitRows(rows, 9); len(got) != 3 {
		t.Fatalf("LimitRows(rows, 9) has %d rows, want 3", len(got))
	}
}

func TestInterleaveDirectionsMergesRoundRobin(t *testing.T) {
	t.Parallel()
	incoming := []map[string]any{{"id": "i1"}, {"id": "i2"}}
	outgoing := []map[string]any{{"id": "o1"}}
	got := InterleaveDirections(incoming, outgoing)
	want := []string{"i1", "o1", "i2"}
	if len(got) != len(want) {
		t.Fatalf("InterleaveDirections = %v, want %d rows", got, len(want))
	}
	for i, id := range want {
		if got[i]["id"] != id {
			t.Fatalf("InterleaveDirections[%d][id] = %v, want %v", i, got[i]["id"], id)
		}
	}
}

func TestDepthSummaryReadsTruncationOffRawCounts(t *testing.T) {
	t.Parallel()
	// A backend page filled to its limit that the grant filter thinned
	// must still report truncated: the flag reads the raw backend
	// counts, never the filtered slices.
	summary := DepthSummary(
		[]map[string]any{{"depth": 4}},
		[]map[string]any{{"depth": 2}},
		6, 6, 5,
	)
	if summary["max_parent_depth"] != 4 || summary["max_child_depth"] != 2 {
		t.Fatalf("DepthSummary depths = %v, want 4 and 2", summary)
	}
	if summary["parent_truncated"] != true || summary["child_truncated"] != true {
		t.Fatalf("DepthSummary truncated = %v, want both true", summary)
	}
}

func TestScopeModeNamesCrossRepo(t *testing.T) {
	t.Parallel()
	if got := ScopeMode(codemodel.RelationshipStoryRequest{CrossRepo: true}); got != "cross_repo" {
		t.Fatalf("ScopeMode(cross_repo) = %q, want cross_repo", got)
	}
	if got := ScopeMode(codemodel.RelationshipStoryRequest{}); got != "repo_scoped" {
		t.Fatalf("ScopeMode(default) = %q, want repo_scoped", got)
	}
}

func TestWhereAnswersEmptyForNoPredicates(t *testing.T) {
	t.Parallel()
	if got := Where(nil); got != "" {
		t.Fatalf("Where(nil) = %q, want empty", got)
	}
	if got := Where([]string{"a = 1", "b = 2"}); got != "WHERE a = 1 AND b = 2" {
		t.Fatalf("Where = %q, want joined clause", got)
	}
}
