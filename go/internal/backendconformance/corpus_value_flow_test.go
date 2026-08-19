// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backendconformance

import (
	"strings"
	"testing"
)

// The value-flow pair reproduces defects that are open upstream, so it fails
// against NornicDB by design. Gating it behind its own opt-in keeps it runnable
// on demand — which is how anyone checks whether upstream has landed a fix —
// without a known-broken backend blocking every unrelated change in the repo.
//
// The pair is not disabled and nothing about it is weakened: with the variable
// set it runs exactly as before, and its failure still names the case.
func TestValueFlowPairIsOptIn(t *testing.T) {
	t.Setenv(valueFlowCasesEnv, "")
	if valueFlowCasesEnabled() {
		t.Fatal("value-flow cases must be off when the variable is unset")
	}
	for _, off := range []string{"", "0", "false", "no", " "} {
		t.Setenv(valueFlowCasesEnv, off)
		if valueFlowCasesEnabled() {
			t.Fatalf("value-flow cases must stay off for %q", off)
		}
	}
	for _, on := range []string{"1", "true", "TRUE", "yes", " 1 "} {
		t.Setenv(valueFlowCasesEnv, on)
		if !valueFlowCasesEnabled() {
			t.Fatalf("value-flow cases must be on for %q", on)
		}
	}
}

// Off by default, the corpora must not carry the pair at all — otherwise the
// live run would still execute it and still go red.
func TestValueFlowPairAbsentWhenOptOut(t *testing.T) {
	t.Setenv(valueFlowCasesEnv, "")
	for _, c := range DefaultReadCorpus() {
		if strings.Contains(c.Name, "value-flow") {
			t.Fatalf("read corpus still carries %q with the opt-in unset", c.Name)
		}
	}
	for _, c := range DefaultWriteCorpus() {
		if strings.Contains(c.Name, "value-flow") {
			t.Fatalf("write corpus still carries %q with the opt-in unset", c.Name)
		}
	}
}

func TestValueFlowPairIsWiredIntoDefaults(t *testing.T) {
	t.Setenv(valueFlowCasesEnv, "1")
	var readFound bool
	for _, c := range DefaultReadCorpus() {
		if c.Name == "value-flow cloud sink aggregation and subscript projection" {
			readFound = true
			if c.MinRows < 1 {
				t.Fatalf("value-flow read case MinRows = %d, want >= 1 so an empty result fails", c.MinRows)
			}
			// Pin the three shapes this case exists to exercise. Without this
			// the query could be reduced to something trivial that keeps the
			// name and MinRows, and both this guard and every default CI run
			// would stay green while detecting nothing.
			for _, fragment := range []string{
				"collect(DISTINCT workload)",
				"workloads[0] AS workload",
				"IN sinkRel.actions",
			} {
				if !strings.Contains(c.Cypher, fragment) {
					t.Errorf("value-flow read case no longer contains %q; it must keep exercising all three divergent shapes", fragment)
				}
			}
		}
	}
	if !readFound {
		t.Fatal("value-flow read case is not in DefaultReadCorpus")
	}
	var writeFound bool
	for _, c := range DefaultWriteCorpus() {
		if c.Name == "value-flow cloud sink seed" {
			writeFound = true
			if !c.RequireAtomicGroup {
				t.Fatal("value-flow seed must commit atomically")
			}
		}
	}
	if !writeFound {
		t.Fatal("value-flow seed case is not in DefaultWriteCorpus")
	}
}
