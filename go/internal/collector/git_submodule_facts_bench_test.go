// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// submoduleBenchFileCount mirrors codeownersBenchFileCount
// (git_codeowners_facts_bench_test.go) and deltaSnapshotBenchmarkFileCount
// (git_snapshot_delta_bench_test.go): a mid-size repository's content-stream
// file count, large enough to amortize loop overhead in the no-regression
// benchmark below (issue #5420 Phase 6 perf evidence).
const submoduleBenchFileCount = 400

// submoduleBenchGitmodulesPath is the one file, of submoduleBenchFileCount,
// that is the recognized ".gitmodules" location in the "with submodule"
// benchmark variant.
const submoduleBenchGitmodulesPath = ".gitmodules"

// submoduleBenchGitmodulesBody mirrors the real golden-corpus ".gitmodules"
// fixture scripts/verify-golden-corpus-gate.sh declares for the
// deployable-config fixture (issue #5420 Phase 5), reused here so the
// benchmark parses representative ".gitmodules" content rather than an
// invented shape.
const submoduleBenchGitmodulesBody = `[submodule "vendor/deployable-source"]
	path = vendor/deployable-source
	url = https://github.com/acme/deployable-source.git
`

// submoduleBenchSubmodulePath is the declared submodule path in
// submoduleBenchGitmodulesBody; benchGitSubmoduleFixture records a real
// gitlink tree entry at this exact path.
const submoduleBenchSubmodulePath = "vendor/deployable-source"

// submoduleBenchGitlinkSHA is a syntactically valid (40 lowercase hex
// characters) fake commit SHA, used the same way fakeGitlinkSHA is in
// git_submodule_pinned_sha_test.go: `git ls-tree` never validates that a
// gitlink SHA resolves to a real committed object, so a fake SHA exercises
// the real subprocess and mode-160000 parsing without a network clone.
const submoduleBenchGitlinkSHA = "5420542054205420542054205420542054205420"

// submoduleBenchRelativePaths returns submoduleBenchFileCount representative
// repo-relative source-file paths, mirroring codeownersBenchRelativePaths.
// When withSubmodule is true, one of them is submoduleBenchGitmodulesPath.
// Both variants share the same file count and non-".gitmodules" path shapes,
// so any measured delta between them isolates the ".gitmodules" hook's added
// cost from unrelated content-stream work.
func submoduleBenchRelativePaths(withSubmodule bool) []string {
	paths := make([]string, 0, submoduleBenchFileCount)
	for i := 0; i < submoduleBenchFileCount; i++ {
		paths = append(paths, fmt.Sprintf("internal/pkg%03d/file%03d.go", i%20, i))
	}
	if withSubmodule {
		paths[0] = submoduleBenchGitmodulesPath
	}
	return paths
}

// benchRunGit runs one git command against repoPath, failing the benchmark
// on error. It mirrors runGit (git_snapshot_source_commit_sha_test.go) but
// takes *testing.B instead of *testing.T since benchmark helpers cannot
// share *testing.T-typed test helpers.
func benchRunGit(b *testing.B, repoPath string, args ...string) {
	b.Helper()
	cmdArgs := append([]string{"-C", repoPath}, args...)
	cmd := exec.Command("git", cmdArgs...) // #nosec G204 -- benchmark helper with controlled args
	if output, err := cmd.CombinedOutput(); err != nil {
		b.Fatalf("git %s: %v\noutput: %s", strings.Join(args, " "), err, output)
	}
}

// benchGitSubmoduleFixture creates a small real git repository, once, outside
// any benchmark timing loop, with one committed gitlink at
// submoduleBenchSubmodulePath (mode 160000, via `git update-index
// --cacheinfo`, the same fixture technique
// TestEmitSubmoduleFactsForCandidatesResolvesPinnedSHA and
// scripts/verify-golden-corpus-gate.sh use). This lets
// BenchmarkSubmoduleContentStreamHook's "WithSubmodule" variant and
// BenchmarkGitSubmoduleGitlinkSHA exercise the real `git ls-tree HEAD`
// subprocess read (issue #5420 Phase 2b) instead of a synthetic stand-in.
func benchGitSubmoduleFixture(b *testing.B) string {
	b.Helper()
	repoPath := b.TempDir()
	benchRunGit(b, repoPath, "init")
	benchRunGit(b, repoPath, "config", "user.email", "bench@example.com")
	benchRunGit(b, repoPath, "config", "user.name", "Bench")
	if err := os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("# root\n"), 0o600); err != nil {
		b.Fatalf("write README.md: %v", err)
	}
	benchRunGit(b, repoPath, "add", "README.md")
	benchRunGit(b, repoPath, "update-index", "--add", "--cacheinfo",
		"160000,"+submoduleBenchGitlinkSHA+","+submoduleBenchSubmodulePath)
	benchRunGit(b, repoPath, "commit", "-m", "add gitlink")
	return repoPath
}

// BenchmarkNoteSubmoduleCandidate measures the O(1) per-content-file
// candidate check (submodule.IsGitmodulesPath via noteSubmoduleCandidate)
// that streamFacts runs once for every content file the git collector
// streams, ".gitmodules" or not (issue #5420 Phase 6 perf no-regression
// evidence). Mirrors BenchmarkNoteCodeownersCandidate.
func BenchmarkNoteSubmoduleCandidate(b *testing.B) {
	paths := submoduleBenchRelativePaths(true)
	candidates := map[string]string{}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, p := range paths {
			body := ""
			if p == submoduleBenchGitmodulesPath {
				body = submoduleBenchGitmodulesBody
			}
			noteSubmoduleCandidate(candidates, p, body)
		}
	}
}

// BenchmarkSubmoduleContentStreamHook measures the full per-repository
// incremental cost the ".gitmodules" hook adds inside streamFacts: one
// noteSubmoduleCandidate call per content file, plus -- only in the
// "WithSubmodule" variant -- the one-time submodule.Emit pass
// (emitSubmoduleFactsForCandidates) that runs once per repository
// generation, including its one `git ls-tree HEAD` subprocess call for the
// single declared submodule. Both subtests share submoduleBenchFileCount
// content files and differ only in whether one of them is a ".gitmodules"
// file, so the delta between "NoSubmodule" and "WithSubmodule" is the
// hook's honest no-regression cost on the same input shape (issue #5420
// Phase 6).
func BenchmarkSubmoduleContentStreamHook(b *testing.B) {
	for _, withSubmodule := range []bool{false, true} {
		name := "NoSubmodule"
		if withSubmodule {
			name = "WithSubmodule"
		}
		b.Run(name, func(b *testing.B) {
			paths := submoduleBenchRelativePaths(withSubmodule)
			// repoPath is only read by the "WithSubmodule" variant:
			// emitSubmoduleFactsForCandidates returns before ever touching
			// repoPath when candidates carries no ".gitmodules" key (see its
			// doc comment), so "NoSubmodule" never spawns a subprocess.
			repoPath := ""
			if withSubmodule {
				repoPath = benchGitSubmoduleFixture(b)
			}
			ch := make(chan facts.Envelope, 64)
			var count atomic.Int64
			w := factStreamWriter{ch: ch, count: &count}
			goCtx := context.Background()

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				candidates := map[string]string{}
				for _, p := range paths {
					body := ""
					if p == submoduleBenchGitmodulesPath {
						body = submoduleBenchGitmodulesBody
					}
					noteSubmoduleCandidate(candidates, p, body)
				}
				before := count.Load()
				emitSubmoduleFactsForCandidates(goCtx, w, "repo-1", repoPath, "scope-1", "gen-1", time.Time{}, candidates)
				sent := int(count.Load() - before)
				for j := 0; j < sent; j++ {
					<-ch
				}
			}
		})
	}
}

// BenchmarkGitSubmoduleGitlinkSHA isolates the `git ls-tree HEAD` subprocess
// cost gitSubmoduleGitlinkSHA pays once per declared submodule (issue #5420
// Phase 2b), separate from BenchmarkSubmoduleContentStreamHook's
// parse/emit overhead above. This is the per-submodule cost that scales
// with submodule count: N declared submodules cost approximately N times
// this benchmark's ns/op, since gitSubmoduleGitlinkSHA runs once per entry
// (see submodule.Emit's doc comment).
func BenchmarkGitSubmoduleGitlinkSHA(b *testing.B) {
	repoPath := benchGitSubmoduleFixture(b)
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gitSubmoduleGitlinkSHA(ctx, repoPath, submoduleBenchSubmodulePath)
	}
}
