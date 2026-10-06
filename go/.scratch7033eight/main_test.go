package main

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestPartitionTerms(t *testing.T) {
	terms := []string{"a", "b", "c", "d", "e"}
	want := [][]string{{"a", "d"}, {"b", "e"}, {"c"}}
	if got := partitionTerms(terms, 3); !reflect.DeepEqual(got, want) {
		t.Fatalf("partitionTerms() = %v, want %v", got, want)
	}
}

func TestHashStringsPreservesOrder(t *testing.T) {
	if hashStrings([]string{"a", "b"}) == hashStrings([]string{"b", "a"}) {
		t.Fatal("assembled page hash must preserve row order")
	}
}

func TestBalancedGroupsCoverTermsOnce(t *testing.T) {
	groups := balancedGroups()
	if len(groups) != 4 {
		t.Fatalf("got %d groups, want 4", len(groups))
	}
	var got []string
	for _, group := range groups {
		if len(group) != 4 {
			t.Fatalf("group has %d terms, want 4", len(group))
		}
		got = append(got, group...)
	}
	slices.Sort(got)
	if !reflect.DeepEqual(got, terms) {
		t.Fatalf("groups do not cover canonical terms exactly once: %v", got)
	}
}

func TestSnapshotIDValidation(t *testing.T) {
	if !validSnapshotID("00000004-00000A1B-1") {
		t.Fatal("valid exported snapshot ID rejected")
	}
	if validSnapshotID("a'; DROP TABLE content_entities; --") {
		t.Fatal("unsafe snapshot ID accepted")
	}
}

func TestDynamicWorkloadsPreselected(t *testing.T) {
	cases := dynamicWorkloads("sample-repo")
	want := []string{"canonical", "common_rare", "punctuation", "explicit_repo", "grant", "language", "empty"}
	if len(cases) != len(want) {
		t.Fatalf("got %d workloads, want %d", len(cases), len(want))
	}
	for i, workload := range cases {
		if workload.name != want[i] || len(workload.terms) != 16 {
			t.Fatalf("workload %d: name=%q terms=%d", i, workload.name, len(workload.terms))
		}
	}
}

func TestDeterministicProbeBindsTerm(t *testing.T) {
	query := deterministicProbeSQL(false)
	if !strings.Contains(query, "ORDER BY e.entity_id LIMIT 250") ||
		!strings.Contains(query, "ORDER BY f.repo_id, f.relative_path LIMIT 250") ||
		!strings.Contains(query, "$1") {
		t.Fatalf("deterministic probe is missing a bound term or total candidate order")
	}
}
