package main

import (
	"strings"
	"testing"
)

func TestP1ExplainUsesShippedProbeAndExactTerms(t *testing.T) {
	query, args := p1ExplainQuery()
	if !strings.HasPrefix(query, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON, SETTINGS) ") {
		t.Fatalf("query is not bounded plan probe: %q", query)
	}
	if !strings.Contains(query, "eshu_require_content_substring_indexes_ready()") ||
		!strings.Contains(query, "LIMIT 250") {
		t.Fatal("query lacks shipped readiness and candidate cap")
	}
	want := []string{"content", "function", "path", "service"}
	if len(args) != len(want) {
		t.Fatalf("term count %d, want %d", len(args), len(want))
	}
	for i, term := range want {
		if args[i] != term {
			t.Errorf("term %d = %v, want %q", i, args[i], term)
		}
	}
}
