// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"context"
	"regexp"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// TestRepoInfrastructureGraphLabelFilterMatchesEntityTypes pins the graph
// infrastructure read to the label-filter shape NornicDB v1.3.3 evaluates (a
// positive label test in a WHERE attached to WITH, #6786 X11) and keeps its
// label list equal to InfrastructureEntityTypes, the set the Go response gate
// admits. A label only in the Go set would be dropped
// server-side; a label only in the Cypher would be fetched and then discarded.
func TestRepoInfrastructureGraphLabelFilterMatchesEntityTypes(t *testing.T) {
	t.Parallel()

	var captured string
	reader := graph.FakeGraphReader{RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
		captured = cypher
		return nil, nil
	}}
	if _, _, err := QueryRepoInfrastructureFromGraph(context.Background(), reader, map[string]any{"repo_id": "repository:r"}); err != nil {
		t.Fatalf("QueryRepoInfrastructureFromGraph() error = %v", err)
	}

	graph.AssertCypherHasNoIgnoredLabelPredicate(t, captured)
	graph.AssertCypherHasNoBrokenAndOr(t, captured)

	withAt := regexp.MustCompile(`\bWITH f, infra\s+WHERE `).FindStringIndex(captured)
	if withAt == nil {
		t.Fatalf("infrastructure read no longer filters labels in a WHERE attached to WITH f, infra:\n%s", captured)
	}
	if regexp.MustCompile(`infra:\w+`).MatchString(captured[:withAt[0]]) {
		t.Fatalf("infrastructure read tests a label before the WITH, where NornicDB v1.3.3 ignores it:\n%s", captured)
	}
	var got []string
	for _, match := range regexp.MustCompile(`infra:(\w+)`).FindAllStringSubmatch(captured, -1) {
		got = append(got, match[1])
	}
	want := append([]string(nil), InfrastructureEntityTypes...)
	sort.Strings(got)
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("cypher filters %d labels %v, want the %d InfrastructureEntityTypes %v", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("cypher label filter %v, want InfrastructureEntityTypes %v", got, want)
		}
	}
}
