// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestFactRecordSchemaIncludesCodeFlowRepoIndex proves the schema registers the
// partial index that makes the cumulative-active code-flow read (#5280)
// seekable by target repository instead of a residual heap filter over every
// scope's code-flow facts. The partial predicate must cover all three code-flow
// fact kinds the read queries and must NOT exclude tombstones (the read ranks
// retractions to pick the newest generation before dropping rn=1 tombstones).
func TestFactRecordSchemaIncludesCodeFlowRepoIndex(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"fact_records_code_flow_repo_idx",
		"((payload->>'repo_id'), scope_id, generation_id, fact_id)",
	} {
		if !strings.Contains(factRecordSchemaSQL, want) {
			t.Fatalf("factRecordSchemaSQL missing %q", want)
		}
	}

	start := strings.Index(factRecordSchemaSQL, "fact_records_code_flow_repo_idx")
	if start < 0 {
		t.Fatal("code-flow repo index statement not found in schema")
	}
	idx := factRecordSchemaSQL[start:]
	if end := strings.Index(idx, ";"); end >= 0 {
		idx = idx[:end+1]
	}
	if strings.Contains(idx, "is_tombstone") {
		t.Fatalf("code-flow repo index must not filter is_tombstone (the read ranks tombstones): %s", idx)
	}

	// The partial predicate must cover EXACTLY the canonical code-flow read set.
	// Extracting and set-comparing (rather than a substring check) fails when a
	// kind is missing OR when an extra kind drifts in, so the index predicate,
	// the query's literal conjunct, and facts.CodeFlowReadFactKinds cannot drift
	// independently across the query/postgres package boundary (#5284). Without
	// this, adding a 4th kind to the read + query literal while forgetting the
	// index WHERE would silently over-fetch that kind through the old all-scope
	// heap filter while the write path still paid the index maintenance cost.
	got := extractIndexFactKindInList(t, idx)
	want := facts.CodeFlowReadFactKinds()
	sort.Strings(want)
	if !codeFlowIndexEqualKinds(got, want) {
		t.Fatalf("code-flow repo index fact_kind set = %v, want the canonical facts.CodeFlowReadFactKinds() = %v", got, want)
	}
}

// extractIndexFactKindInList pulls the parenthesised kind list from the partial
// index's `fact_kind IN ( ... )` predicate and returns the unquoted kinds
// sorted. Fails the test when the predicate is absent or malformed rather than
// silently returning an empty set that would false-green a broken predicate.
func extractIndexFactKindInList(t *testing.T, ddl string) []string {
	t.Helper()
	marker := "fact_kind IN ("
	start := strings.Index(ddl, marker)
	if start < 0 {
		t.Fatalf("code-flow repo index missing `fact_kind IN (` predicate: %s", ddl)
	}
	open := start + len(marker)
	end := strings.Index(ddl[open:], ")")
	if end < 0 {
		t.Fatalf("code-flow repo index has unterminated `fact_kind IN (` predicate: %s", ddl)
	}
	kinds := []string{}
	for _, part := range strings.Split(ddl[open:open+end], ",") {
		kind := strings.Trim(strings.TrimSpace(part), "'")
		if kind != "" {
			kinds = append(kinds, kind)
		}
	}
	sort.Strings(kinds)
	return kinds
}

func codeFlowIndexEqualKinds(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
