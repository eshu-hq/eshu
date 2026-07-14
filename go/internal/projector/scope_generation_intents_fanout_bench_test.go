// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// fanOutBenchmarkFixture builds a large, realistic-shaped inputFacts slice
// for BenchmarkAppendScopeGenerationReducerIntentsFanOut (issue #4875's
// Prove-Theory-First shim). It reuses fanOutParityFixture's ~40 trigger facts
// (so the benchmark exercises the exact same 38 domains the accuracy test
// pins) and pads the generation with decoyCount source-code-domain decoy
// facts of many distinct kinds, none of which any of the 38
// build*ReducerIntent probes match.
//
// This mirrors the dominant real-world shape the issue describes: a
// source-heavy repository generation carries thousands of code/content facts
// and at most a handful of cloud/k8s/supply-chain facts, so most of the 38
// probes scan the ENTIRE generation and find nothing. The pre-#4875
// full-scan implementation pays O(38*N) on such a generation; the shared
// reducerIntentFactIndex should reduce that to O(N) index construction plus
// O(1)-to-O(matches) per probe.
func fanOutBenchmarkFixture(scopeValue scope.IngestionScope, generation scope.ScopeGeneration, decoyCount int) []facts.Envelope {
	triggers := fanOutParityFixture(scopeValue, generation)
	inputFacts := make([]facts.Envelope, 0, len(triggers)+decoyCount)

	// Interleave decoys around the trigger facts (instead of appending them
	// all after) so every probe's scan — old full-scan or new index lookup —
	// sees the same realistic mix a live generation would: trigger facts
	// scattered among a much larger pool of irrelevant source facts, not
	// conveniently clustered at either end.
	decoyKinds := []string{
		"code_symbol_reference", "code_call_edge", "file", "content_entity",
		"git_blame_line", "code_import", "code_symbol_definition",
	}
	triggerStep := decoyCount / (len(triggers) + 1)
	if triggerStep < 1 {
		triggerStep = 1
	}
	triggerIdx := 0
	for i := 0; i < decoyCount; i++ {
		if triggerIdx < len(triggers) && i%triggerStep == 0 {
			inputFacts = append(inputFacts, triggers[triggerIdx])
			triggerIdx++
		}
		inputFacts = append(inputFacts, facts.Envelope{
			FactID:   fmt.Sprintf("decoy-%d", i),
			FactKind: decoyKinds[i%len(decoyKinds)],
			Payload:  map[string]any{"path": fmt.Sprintf("src/pkg%d/file%d.go", i%50, i)},
		})
	}
	for ; triggerIdx < len(triggers); triggerIdx++ {
		inputFacts = append(inputFacts, triggers[triggerIdx])
	}
	return inputFacts
}

// BenchmarkAppendScopeGenerationReducerIntentsFanOut is the #4875
// Prove-Theory-First shim: it measures appendScopeGenerationReducerIntents's
// CPU cost on a representative multi-domain, large-N repo generation. Run
// before and after the shared reducerIntentFactIndex refactor with:
//
//	go test ./internal/projector/... -run '^$' \
//	  -bench '^BenchmarkAppendScopeGenerationReducerIntentsFanOut$' \
//	  -benchmem -count=6
//
// and compare with benchstat. The accuracy proof
// (TestAppendScopeGenerationReducerIntentsFanOutParity) must pass unchanged
// on both sides — this benchmark proves only the CPU/allocation claim.
func BenchmarkAppendScopeGenerationReducerIntentsFanOut(b *testing.B) {
	scopeValue, generation := fanOutParityScopeAndGeneration()
	inputFacts := fanOutBenchmarkFixture(scopeValue, generation, 5000)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		appendScopeGenerationReducerIntents(nil, scopeValue, generation, inputFacts)
	}
}
