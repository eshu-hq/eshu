// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"reflect"
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

func TestSnapshotIDValidation(t *testing.T) {
	if !validSnapshotID("00000004-00000A1B-1") {
		t.Fatal("valid exported snapshot ID rejected")
	}
	if validSnapshotID("a'; DROP TABLE content_entities; --") {
		t.Fatal("unsafe snapshot ID accepted")
	}
}

func TestValidateProofMode(t *testing.T) {
	if err := validateProofMode("fixed_canonical"); err != nil {
		t.Fatalf("fixed mode rejected: %v", err)
	}
	if err := validateProofMode("fixed_diagnostic"); err != nil {
		t.Fatalf("read-only fixed diagnostic rejected: %v", err)
	}
	for _, mode := range []string{"", "parallel", "parallel8", "balanced", "deterministic", "diagnostic_punctuation", "timing_canonical", "explain_p1"} {
		if err := validateProofMode(mode); err == nil {
			t.Errorf("unsupported mode %q accepted", mode)
		}
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
