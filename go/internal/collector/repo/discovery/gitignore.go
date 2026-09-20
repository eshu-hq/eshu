// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package discovery

import (
	"os"
	"path"
	"path/filepath"
	"strings"
)

type gitignoreSpec struct {
	patterns []gitignorePattern
}

type gitignorePattern struct {
	raw      string
	negated  bool
	dirOnly  bool
	anchored bool
}

// filterRepoFilesByGitignore drops files matching a repo-local .gitignore
// rule, EXCEPT a file whose repo-relative path is a member of tracked()
// (issue #5591): git only applies .gitignore to UNTRACKED paths, so a
// force-committed file that matches a gitignore rule stays tracked and must
// stay discoverable.
//
// tracked() (a resolveTracked closure, memoized per repo root via
// sync.OnceValue by the caller) is called ONLY when a file actually matches
// the gitignore pattern — never for a file gitignore would keep anyway. A
// repository whose files never match a .gitignore rule therefore never
// invokes the resolver, and never pays its `git ls-files` subprocess cost
// (see evidence-5591-tracked-ignored-perf.md). A tracked() result that is
// nil/empty (no resolver, or the resolver reported ok=false) then filters
// exactly as it did before #5591.
func filterRepoFilesByGitignore(repoRoot string, files []FileWithSize, tracked func() map[string]struct{}) []FileWithSize {
	cache := make(map[string]*gitignoreSpec)
	kept := make([]FileWithSize, 0, len(files))
	for _, file := range files {
		if !isIgnoredByRepoIgnoreFile(repoRoot, file.Path, ".gitignore", cache) {
			kept = append(kept, file)
			continue
		}
		if isTrackedRepoFile(repoRoot, file.Path, tracked()) {
			kept = append(kept, file)
		}
	}
	return kept
}

// filterRepoFilesByEshuIgnore drops files matching a repo-local .eshuignore
// rule. Unlike .gitignore, .eshuignore is the operator's own opt-out and
// applies regardless of git-tracked status (issue #5591 leaves this filter's
// semantics unchanged). It also returns the absolute paths of every file it
// skipped so the caller can classify which skips are tracked-file skips
// worth surfacing individually (see recordTrackedEshuIgnoreSkips).
func filterRepoFilesByEshuIgnore(repoRoot string, files []FileWithSize) (kept []FileWithSize, skipped []string) {
	cache := make(map[string]*gitignoreSpec)
	kept = make([]FileWithSize, 0, len(files))
	for _, file := range files {
		if isIgnoredByRepoIgnoreFile(repoRoot, file.Path, ".eshuignore", cache) {
			skipped = append(skipped, file.Path)
			continue
		}
		kept = append(kept, file)
	}
	return kept, skipped
}

// isTrackedRepoFile reports whether filePath's repo-relative, slash-separated
// path is a member of tracked. A nil/empty tracked set always reports false,
// matching the pre-#5591 behavior of not knowing which files git tracks.
func isTrackedRepoFile(repoRoot string, filePath string, tracked map[string]struct{}) bool {
	if len(tracked) == 0 {
		return false
	}
	rel, err := filepath.Rel(repoRoot, filePath)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(filepath.Clean(rel))
	_, ok := tracked[rel]
	return ok
}

func isIgnoredByRepoIgnoreFile(
	repoRoot string,
	filePath string,
	ignoreFileName string,
	cache map[string]*gitignoreSpec,
) bool {
	if !pathWithinRoot(repoRoot, filePath) {
		return false
	}

	ignored := false
	for _, dir := range ancestorDirs(repoRoot, filePath) {
		spec := loadGitignoreSpec(filepath.Join(dir, ignoreFileName), cache)
		if spec == nil {
			continue
		}

		rel, err := filepath.Rel(dir, filePath)
		if err != nil {
			continue
		}
		rel = filepath.ToSlash(filepath.Clean(rel))

		for _, pattern := range spec.patterns {
			if pattern.matches(rel) {
				ignored = !pattern.negated
			}
		}
	}

	return ignored
}

func ancestorDirs(repoRoot, filePath string) []string {
	current := filepath.Clean(filepath.Dir(filePath))
	root := filepath.Clean(repoRoot)
	dirs := make([]string, 0, 8)
	for {
		dirs = append(dirs, current)
		if current == root {
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}

	for i, j := 0, len(dirs)-1; i < j; i, j = i+1, j-1 {
		dirs[i], dirs[j] = dirs[j], dirs[i]
	}
	return dirs
}

func loadGitignoreSpec(path string, cache map[string]*gitignoreSpec) *gitignoreSpec {
	normalized := filepath.Clean(path)
	if spec, ok := cache[normalized]; ok {
		return spec
	}

	contents, err := os.ReadFile(normalized)
	if err != nil {
		cache[normalized] = nil
		return nil
	}

	spec := parseGitignoreSpec(strings.Split(string(contents), "\n"))
	cache[normalized] = spec
	return spec
}

func parseGitignoreSpec(lines []string) *gitignoreSpec {
	patterns := make([]gitignorePattern, 0, len(lines))
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		pattern := gitignorePattern{}
		if strings.HasPrefix(line, "!") {
			pattern.negated = true
			line = strings.TrimPrefix(line, "!")
		}
		if strings.HasPrefix(line, "/") {
			pattern.anchored = true
			line = strings.TrimPrefix(line, "/")
		}
		if strings.HasSuffix(line, "/") {
			pattern.dirOnly = true
			line = strings.TrimSuffix(line, "/")
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		pattern.raw = filepath.ToSlash(line)
		patterns = append(patterns, pattern)
	}

	if len(patterns) == 0 {
		return nil
	}
	return &gitignoreSpec{patterns: patterns}
}

func (p gitignorePattern) matches(rel string) bool {
	rel = filepath.ToSlash(filepath.Clean(rel))
	if rel == "." {
		return false
	}

	if p.dirOnly {
		return pathPrefixMatches(rel, p.raw)
	}

	if p.anchored {
		ok, _ := path.Match(p.raw, rel)
		return ok
	}

	if strings.Contains(p.raw, "/") {
		if ok, _ := path.Match(p.raw, rel); ok {
			return true
		}
		for _, candidate := range suffixCandidates(rel) {
			if ok, _ := path.Match(p.raw, candidate); ok {
				return true
			}
		}
		return false
	}

	base := filepath.Base(rel)
	if ok, _ := path.Match(p.raw, base); ok {
		return true
	}
	for _, segment := range strings.Split(rel, "/") {
		if ok, _ := path.Match(p.raw, segment); ok {
			return true
		}
	}
	return false
}

func suffixCandidates(rel string) []string {
	parts := strings.Split(rel, "/")
	candidates := make([]string, 0, len(parts))
	for i := range parts {
		candidates = append(candidates, strings.Join(parts[i:], "/"))
	}
	return candidates
}
