// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package discovery

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// SupportedFileMatcher reports whether the caller wants to index one file path.
//
// Callers can base this on extension, parser key, or any other repository-local
// metadata they already have.
type SupportedFileMatcher func(path string) bool

// Options controls filesystem discovery and repo-local ignore behavior.
type Options struct {
	// IgnoredDirs is compared case-insensitively against directory names.
	IgnoredDirs []string
	// IgnoredExtensions lists file suffixes (e.g. ".log", ".min.js") that are
	// always skipped. Matching is case-insensitive against the full file name.
	IgnoredExtensions []string
	// IgnoreHidden skips dot-prefixed files and directories unless their paths
	// are covered by PreservedHiddenPrefixes.
	IgnoreHidden bool
	// PreservedHiddenPrefixes keeps hidden paths such as ".github/workflows"
	// when hidden-path skipping is enabled. Paths are relative to the scan root.
	PreservedHiddenPrefixes []string
	// HonorGitignore enables repo-local .gitignore filtering.
	HonorGitignore bool
	// HonorEshuIgnore enables repo-local .eshuignore filtering.
	HonorEshuIgnore bool
	// IgnoredPathGlobs lists repo-relative path globs that should be skipped
	// before parser matching. Patterns may end in "/**" to prune a subtree.
	IgnoredPathGlobs []PathGlobRule
	// PreservedPathGlobs lists repo-relative path globs that remain indexable
	// even when a broader ignored path glob covers an ancestor.
	PreservedPathGlobs []string
	// GitTrackedResolver resolves the set of repo-relative, slash-separated
	// paths git tracks in the repository rooted at repoRoot, and reports
	// whether that resolution succeeded (ok=false means "unknown," e.g.
	// repoRoot is not a git checkout or git itself is unavailable — callers
	// MUST NOT treat ok=false as "nothing is tracked").
	//
	// When set and HonorGitignore is true, a file this resolver reports as
	// tracked is never filtered by .gitignore (issue #5591): git itself only
	// applies .gitignore to UNTRACKED paths, so a force-committed
	// (`git add -f`) file that matches a gitignore rule stays tracked and
	// must stay discoverable. This package stays git-free — the collector
	// package is responsible for constructing a resolver that shells out to
	// `git ls-files`. HonorEshuIgnore is unaffected: .eshuignore remains the
	// operator's own opt-out and still skips a tracked file.
	GitTrackedResolver func(repoRoot string) (tracked map[string]struct{}, ok bool)
}

// PathGlobRule describes a repo-relative path glob and the operator-facing
// skip reason to report when discovery prunes a matching path.
type PathGlobRule struct {
	Pattern string
	Reason  string
}

// DiscoveryStats tracks how many files and directories were skipped during
// discovery, broken down by the specific name that triggered the skip.
// These stats support operator telemetry for understanding what the indexer
// excludes across 878+ repos.
type DiscoveryStats struct {
	// DirsSkippedByName maps each ignored directory name (e.g. "node_modules",
	// "vendor") to the number of times it was pruned.
	DirsSkippedByName map[string]int
	// FilesSkippedByExtension maps each ignored extension (e.g. ".min.js",
	// ".pyc") to the number of files skipped.
	FilesSkippedByExtension map[string]int
	// FilesSkippedByContent maps content-based skip reasons (e.g.
	// "generated-webpack") to the number of files skipped.
	FilesSkippedByContent map[string]int
	// DirsSkippedByUser maps user-configured skip reasons to pruned
	// directories.
	DirsSkippedByUser map[string]int
	// FilesSkippedByUser maps user-configured skip reasons to skipped files.
	FilesSkippedByUser map[string]int
	// FilesSkippedHidden counts files skipped because they are hidden (dot-prefixed).
	FilesSkippedHidden int
	// FilesSkippedGitignore counts files filtered by repo-local .gitignore rules.
	FilesSkippedGitignore int
	// FilesSkippedEshuIgnore counts files filtered by repo-local .eshuignore rules.
	FilesSkippedEshuIgnore int
	// TrackedFilesSkippedEshuIgnore lists the repo-relative, slash-separated
	// paths of files git tracks (per GitTrackedResolver) that repo-local
	// .eshuignore rules still skipped (issue #5591 acceptance: unlike
	// .gitignore, which #5591 makes defer to git's own tracked set,
	// .eshuignore remains a deliberate operator opt-out that CAN skip a
	// tracked file — but that skip must stay individually visible to
	// operators rather than disappearing into the aggregate
	// FilesSkippedEshuIgnore count). Capped at
	// trackedFilesSkippedEshuIgnoreCap entries; additional skips beyond the
	// cap are counted in TrackedFilesSkippedEshuIgnoreOverflow instead of
	// growing this slice without bound.
	TrackedFilesSkippedEshuIgnore []string
	// TrackedFilesSkippedEshuIgnoreOverflow counts tracked-file .eshuignore
	// skips beyond the TrackedFilesSkippedEshuIgnore cap.
	TrackedFilesSkippedEshuIgnoreOverflow int
}

// trackedFilesSkippedEshuIgnoreCap bounds DiscoveryStats.TrackedFilesSkippedEshuIgnore
// so a repo with a large number of tracked-but-eshuignored files cannot grow
// one generation's stats (and its downstream structured logs) unbounded.
const trackedFilesSkippedEshuIgnoreCap = 100

// TotalDirsSkipped returns the aggregate count of pruned directories.
func (s DiscoveryStats) TotalDirsSkipped() int {
	total := 0
	for _, n := range s.DirsSkippedByName {
		total += n
	}
	for _, n := range s.DirsSkippedByUser {
		total += n
	}
	return total
}

// TotalFilesSkipped returns the aggregate count of skipped files across all
// skip reasons.
func (s DiscoveryStats) TotalFilesSkipped() int {
	total := s.FilesSkippedHidden + s.FilesSkippedGitignore + s.FilesSkippedEshuIgnore
	for _, n := range s.FilesSkippedByExtension {
		total += n
	}
	for _, n := range s.FilesSkippedByContent {
		total += n
	}
	for _, n := range s.FilesSkippedByUser {
		total += n
	}
	return total
}

// ResolveRepositoryFileSets discovers supported files beneath root, groups them
// by the nearest repo root, and applies repo-local .gitignore/.eshuignore
// filtering.
func ResolveRepositoryFileSets(
	root string,
	supported SupportedFileMatcher,
	opts Options,
) ([]RepoFileSet, error) {
	_, fileSets, err := ResolveRepositoryFileSetsWithStats(root, supported, opts)
	return fileSets, err
}

// ResolveRepositoryFileSetsWithStats is like ResolveRepositoryFileSets but
// also returns discovery statistics for telemetry.
func ResolveRepositoryFileSetsWithStats(
	root string,
	supported SupportedFileMatcher,
	opts Options,
) (DiscoveryStats, []RepoFileSet, error) {
	scanRoot, err := normalizeScanRoot(root)
	if err != nil {
		return DiscoveryStats{}, nil, err
	}
	if supported == nil {
		return DiscoveryStats{}, nil, errors.New("supported file matcher is required")
	}

	candidates, stats, err := collectSupportedFiles(scanRoot, supported, opts)
	if err != nil {
		return stats, nil, err
	}
	if len(candidates) == 0 {
		return stats, []RepoFileSet{{RepoRoot: scanRoot}}, nil
	}

	groups := groupFilesByRepository(scanRoot, candidates)
	repoRoots := make([]string, 0, len(groups))
	for repoRoot := range groups {
		repoRoots = append(repoRoots, repoRoot)
	}
	sort.Strings(repoRoots)

	result := make([]RepoFileSet, 0, len(repoRoots))
	for _, repoRoot := range repoRoots {
		files := append([]FileWithSize(nil), groups[repoRoot]...)
		sortFileWithSizeSlice(files)

		// resolveTracked defers the GitTrackedResolver call (one `git
		// ls-files` subprocess, ~26-29ms measured — see
		// evidence-5591-tracked-ignored-perf.md) until a filter below
		// actually needs a tracked-status decision for THIS repo root: a
		// .gitignore match (filterRepoFilesByGitignore) or a non-empty
		// .eshuignore skip set (recordTrackedEshuIgnoreSkips). A repo with
		// no ignore-matched candidate at all never calls the resolver.
		// sync.OnceValue memoizes across both call sites so a repo that
		// needs it for both filters still pays the subprocess only once.
		// This loop iterates repoRoots on a single goroutine; OnceValue
		// documents "called at most once" regardless, so this stays correct
		// if a future change parallelizes the loop.
		resolveTracked := sync.OnceValue(func() map[string]struct{} {
			if opts.GitTrackedResolver == nil {
				return nil
			}
			resolved, ok := opts.GitTrackedResolver(repoRoot)
			if !ok {
				return nil
			}
			return resolved
		})

		if opts.HonorGitignore {
			before := len(files)
			files = filterRepoFilesByGitignore(repoRoot, files, resolveTracked)
			stats.FilesSkippedGitignore += before - len(files)
		}
		if opts.HonorEshuIgnore {
			before := len(files)
			var skippedPaths []string
			files, skippedPaths = filterRepoFilesByEshuIgnore(repoRoot, files)
			stats.FilesSkippedEshuIgnore += before - len(files)
			recordTrackedEshuIgnoreSkips(&stats, repoRoot, skippedPaths, resolveTracked)
		}
		result = append(result, RepoFileSet{
			RepoRoot: repoRoot,
			Files:    files,
		})
	}
	return stats, result, nil
}

// recordTrackedEshuIgnoreSkips appends the repo-relative path of every
// skippedPath that is also a member of tracked() into
// stats.TrackedFilesSkippedEshuIgnore, capped at
// trackedFilesSkippedEshuIgnoreCap with an overflow counter beyond the cap.
//
// It checks len(skippedPaths) == 0 BEFORE calling tracked(): a repo with no
// .eshuignore skips at all must never spawn the `git ls-files` subprocess
// tracked() lazily resolves. A nil/empty resolved set (resolver absent, or
// reported ok=false) is then a no-op, matching the pre-#5591 behavior of not
// knowing which files git tracks.
func recordTrackedEshuIgnoreSkips(stats *DiscoveryStats, repoRoot string, skippedPaths []string, tracked func() map[string]struct{}) {
	if len(skippedPaths) == 0 {
		return
	}
	trackedSet := tracked()
	if len(trackedSet) == 0 {
		return
	}
	for _, skippedPath := range skippedPaths {
		rel, err := filepath.Rel(repoRoot, skippedPath)
		if err != nil {
			continue
		}
		rel = filepath.ToSlash(filepath.Clean(rel))
		if _, ok := trackedSet[rel]; !ok {
			continue
		}
		if len(stats.TrackedFilesSkippedEshuIgnore) >= trackedFilesSkippedEshuIgnoreCap {
			stats.TrackedFilesSkippedEshuIgnoreOverflow++
			continue
		}
		stats.TrackedFilesSkippedEshuIgnore = append(stats.TrackedFilesSkippedEshuIgnore, rel)
	}
}

func normalizeScanRoot(root string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", errors.New("scan root is required")
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve scan root %q: %w", root, err)
	}
	if resolved, err := filepath.EvalSymlinks(absRoot); err == nil {
		absRoot = resolved
	}

	info, err := os.Stat(absRoot)
	if err != nil {
		return "", fmt.Errorf("stat scan root %q: %w", root, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("scan root %q is not a directory", root)
	}
	return absRoot, nil
}
