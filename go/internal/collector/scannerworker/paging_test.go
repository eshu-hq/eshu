// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scannerworker

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/factenvelope"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
)

func hostilePagingOutput(input ClaimInput, count int) FactOutput {
	output := FactOutput{TargetCount: 1, ResultCount: count}
	for i := 0; i < count; i++ {
		fact := vulnerabilitySourceFact(input, chain.VulnerabilityOSPackageFactKind, strings.Repeat("pkg-", 8)+string(rune('a'+i%26))+strings.Repeat("x", 40))
		fact.Payload["blob"] = strings.Repeat("x", 400)
		output.Facts = append(output.Facts, fact)
	}
	return output
}

func wireBytesOf(t *testing.T, envelopes []facts.Envelope) int64 {
	t.Helper()

	var total int64
	for _, envelope := range envelopes {
		wire, err := factenvelope.SDKFactFromInternal(envelope)
		if err != nil {
			t.Fatalf("SDKFactFromInternal() error = %v, want nil", err)
		}
		raw, err := json.Marshal(wire)
		if err != nil {
			t.Fatalf("json.Marshal() error = %v, want nil", err)
		}
		total += int64(len(raw))
	}
	return total
}

// TestSplitFactOutputPagesFitByteBound proves analyzer-side byte accounting:
// a hostile FactOutput splits into pages that each fit the per-call bound,
// in order, with no fact lost, and every page keeps the claim identity.
func TestSplitFactOutputPagesFitByteBound(t *testing.T) {
	t.Parallel()

	input := testClaimInput(t)
	output := hostilePagingOutput(input, 10)
	single := wireBytesOf(t, output.Facts[:1])
	pages, err := SplitFactOutput(output, single*3)
	if err != nil {
		t.Fatalf("SplitFactOutput() error = %v, want nil", err)
	}
	if len(pages) < 2 {
		t.Fatalf("SplitFactOutput() pages = %d, want split output", len(pages))
	}
	seen := 0
	for index, page := range pages {
		if len(page.Facts) == 0 {
			t.Fatalf("SplitFactOutput() pages[%d] is empty", index)
		}
		if size := wireBytesOf(t, page.Facts); size > single*3 {
			t.Fatalf("SplitFactOutput() pages[%d] bytes = %d, want <= %d", index, size, single*3)
		}
		if page.TargetCount != output.TargetCount {
			t.Fatalf("SplitFactOutput() pages[%d] target_count = %d, want %d", index, page.TargetCount, output.TargetCount)
		}
		if page.ResultCount != len(page.Facts) {
			t.Fatalf("SplitFactOutput() pages[%d] result_count = %d, want %d", index, page.ResultCount, len(page.Facts))
		}
		for offset, fact := range page.Facts {
			if fact.StableFactKey != output.Facts[seen+offset].StableFactKey {
				t.Fatalf("SplitFactOutput() pages[%d] order changed at offset %d", index, offset)
			}
			if fact.GenerationID != input.GenerationID || fact.ScopeID != input.Target.ScopeID {
				t.Fatalf("SplitFactOutput() pages[%d] lost claim identity", index)
			}
		}
		seen += len(page.Facts)
	}
	if seen != len(output.Facts) {
		t.Fatalf("SplitFactOutput() kept %d facts, want %d", seen, len(output.Facts))
	}
}

// TestSplitFactOutputRejectsOversizeFact proves a single fact above the whole
// bound fails closed naming itself: callers fix the producer, the fact is
// never silently dropped to fit.
func TestSplitFactOutputRejectsOversizeFact(t *testing.T) {
	t.Parallel()

	input := testClaimInput(t)
	output := hostilePagingOutput(input, 1)
	output.Facts[0].Payload["blob"] = strings.Repeat("x", 4096)
	if _, err := SplitFactOutput(output, 128); err == nil {
		t.Fatal("SplitFactOutput(oversized) error = nil, want explicit bound error")
	} else if !strings.Contains(err.Error(), output.Facts[0].StableFactKey) {
		t.Fatalf("SplitFactOutput(oversized) error = %v, want stable key named", err)
	}
	if _, err := SplitFactOutput(FactOutput{}, 1024); err == nil {
		t.Fatal("SplitFactOutput(empty) error = nil, want non-nil")
	}
	if _, err := SplitFactOutput(hostilePagingOutput(input, 1), 0); err == nil {
		t.Fatal("SplitFactOutput(non-positive bound) error = nil, want non-nil")
	}
}

// TestMaxFactsContractUnchanged pins the acceptance criterion: paging never
// reduces the analyzer fact ceiling silently. Every scanner profile keeps its
// 50,000-fact contract; the per-call page bound only spreads those facts
// across resumed calls.
func TestMaxFactsContractUnchanged(t *testing.T) {
	t.Parallel()

	for _, analyzer := range []AnalyzerKind{
		AnalyzerSourceAnalysis,
		AnalyzerSBOMGeneration,
		AnalyzerOSPackageExtraction,
		AnalyzerImageUnpacking,
	} {
		limits, err := DefaultResourceLimits(analyzer)
		if err != nil {
			t.Fatalf("DefaultResourceLimits(%q) error = %v, want nil", analyzer, err)
		}
		if limits.MaxFacts != 50000 {
			t.Fatalf("DefaultResourceLimits(%q).MaxFacts = %d, want 50000", analyzer, limits.MaxFacts)
		}
	}
}
