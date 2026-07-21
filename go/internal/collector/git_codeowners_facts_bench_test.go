// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// codeownersBenchFileCount mirrors deltaSnapshotBenchmarkFileCount
// (git_snapshot_delta_bench_test.go): a mid-size repository's content-stream
// file count, large enough to amortize loop overhead in the no-regression
// benchmark below (issue #5419 Phase 6 perf evidence).
const codeownersBenchFileCount = 400

// codeownersBenchCodeownersPath is the one file, of codeownersBenchFileCount,
// that is a real CODEOWNERS candidate location in the "with CODEOWNERS"
// benchmark variant.
const codeownersBenchCodeownersPath = ".github/CODEOWNERS"

// codeownersBenchBody is the exact content of the real golden-corpus fixture
// at tests/fixtures/ecosystems/go_comprehensive/.github/CODEOWNERS (#5419
// Phase 5), reused here so the benchmark parses representative CODEOWNERS
// content rather than an invented shape.
const codeownersBenchBody = `# Golden-corpus CODEOWNERS fixture (issue #5419 Phase 5). Gives the
# go_comprehensive repo at least one real DECLARES_CODEOWNER edge so the B-7
# golden-corpus gate can assert non-vacuous codeowners ownership truth instead
# of the Phase 4 structural (minimum_results: 0) placeholder.
*.go @eshu-hq/platform
/docs/ @eshu-hq/docs
`

// codeownersBenchRelativePaths returns codeownersBenchFileCount representative
// repo-relative source-file paths. When withCodeowners is true, one of them is
// codeownersBenchCodeownersPath, matching the real CODEOWNERS location
// precedence codeowners.IsCandidatePath honors. Both variants share the same
// file count and non-CODEOWNERS path shapes, so any measured delta between
// them isolates the CODEOWNERS hook's added cost from unrelated content-stream
// work.
func codeownersBenchRelativePaths(withCodeowners bool) []string {
	paths := make([]string, 0, codeownersBenchFileCount)
	for i := 0; i < codeownersBenchFileCount; i++ {
		paths = append(paths, fmt.Sprintf("internal/pkg%03d/file%03d.go", i%20, i))
	}
	if withCodeowners {
		paths[0] = codeownersBenchCodeownersPath
	}
	return paths
}

// BenchmarkNoteCodeownersCandidate measures the O(1) per-content-file
// candidate check (codeowners.IsCandidatePath via noteCodeownersCandidate)
// that streamFacts runs once for every content file the git collector streams,
// CODEOWNERS or not (issue #5419 Phase 6 perf no-regression evidence).
func BenchmarkNoteCodeownersCandidate(b *testing.B) {
	paths := codeownersBenchRelativePaths(true)
	candidates := map[string]string{}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, p := range paths {
			body := ""
			if p == codeownersBenchCodeownersPath {
				body = codeownersBenchBody
			}
			noteCodeownersCandidate(candidates, p, body)
		}
	}
}

// BenchmarkCodeownersContentStreamHook measures the full per-repository
// incremental cost the CODEOWNERS hook adds inside streamFacts: one
// noteCodeownersCandidate call per content file, plus -- only in the
// "WithCodeowners" variant -- the one-time codeowners.ResolveWinner + Parse +
// Emit pass emitCodeownersFactsForCandidates runs once per repository
// generation. Both subtests share codeownersBenchFileCount content files and
// differ only in whether one of them is a CODEOWNERS file, so the delta
// between "NoCodeowners" and "WithCodeowners" is the hook's honest
// no-regression cost on the same input shape (issue #5419 Phase 6).
func BenchmarkCodeownersContentStreamHook(b *testing.B) {
	for _, withCodeowners := range []bool{false, true} {
		name := "NoCodeowners"
		if withCodeowners {
			name = "WithCodeowners"
		}
		b.Run(name, func(b *testing.B) {
			paths := codeownersBenchRelativePaths(withCodeowners)
			// Buffered generously beyond the fixture's rule count (2) so a
			// single emitCodeownersFactsForCandidates call never blocks on
			// send; drained synchronously below, mirroring the real
			// committer that always drains this channel downstream.
			ch := make(chan facts.Envelope, 64)
			var count atomic.Int64
			w := factStreamWriter{ch: ch, count: &count}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				candidates := map[string]string{}
				for _, p := range paths {
					body := ""
					if p == codeownersBenchCodeownersPath {
						body = codeownersBenchBody
					}
					noteCodeownersCandidate(candidates, p, body)
				}
				before := count.Load()
				emitCodeownersFactsForCandidates(w, "repo-1", "scope-1", "gen-1", time.Time{}, candidates)
				sent := int(count.Load() - before)
				for j := 0; j < sent; j++ {
					<-ch
				}
			}
		})
	}
}
