package discovery

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
}

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

// RepoFileSet groups one repo root with its discovered supported files.
//
// RepoRoot and Files are absolute paths and Files are sorted for stable output.
type RepoFileSet struct {
	RepoRoot string
	Files    []string
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
		files := append([]string(nil), groups[repoRoot]...)
		sort.Strings(files)
		if opts.HonorGitignore {
			before := len(files)
			files = filterRepoFilesByGitignore(repoRoot, files)
			stats.FilesSkippedGitignore += before - len(files)
		}
		if opts.HonorEshuIgnore {
			before := len(files)
			files = filterRepoFilesByEshuIgnore(repoRoot, files)
			stats.FilesSkippedEshuIgnore += before - len(files)
		}
		result = append(result, RepoFileSet{
			RepoRoot: repoRoot,
			Files:    files,
		})
	}
	return stats, result, nil
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

func collectSupportedFiles(
	scanRoot string,
	supported SupportedFileMatcher,
	opts Options,
) ([]string, DiscoveryStats, error) {
	ignoredDirs := normalizeIgnoredDirs(opts.IgnoredDirs)
	ignoredExts := normalizeExtensions(opts.IgnoredExtensions)
	preservedHidden := normalizePrefixes(opts.PreservedHiddenPrefixes)
	ignoredPathGlobs := normalizePathGlobRules(opts.IgnoredPathGlobs)
	preservedPathGlobs := normalizePathGlobPatterns(opts.PreservedPathGlobs)

	stats := DiscoveryStats{
		DirsSkippedByName:       make(map[string]int),
		FilesSkippedByExtension: make(map[string]int),
		FilesSkippedByContent:   make(map[string]int),
		DirsSkippedByUser:       make(map[string]int),
		FilesSkippedByUser:      make(map[string]int),
	}
	files := make([]string, 0)
	if err := filepath.WalkDir(scanRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, fs.ErrPermission) {
				if entry != nil && entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			return walkErr
		}
		if path == scanRoot {
			return nil
		}

		rel, err := filepath.Rel(scanRoot, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(filepath.Clean(rel))

		if entry.IsDir() {
			if isIgnoredDir(entry.Name(), ignoredDirs) {
				stats.DirsSkippedByName[strings.ToLower(entry.Name())]++
				return filepath.SkipDir
			}
			if opts.IgnoreHidden && isHiddenPath(rel) && !preservesHiddenPath(rel, preservedHidden) {
				stats.DirsSkippedByName[".hidden"]++
				return filepath.SkipDir
			}
			if reason, ok := matchedIgnoredPathGlob(rel, true, ignoredPathGlobs, preservedPathGlobs); ok {
				stats.DirsSkippedByUser[reason]++
				return filepath.SkipDir
			}
			return nil
		}

		if shouldSkipFile(rel, opts.IgnoreHidden, preservedHidden) {
			stats.FilesSkippedHidden++
			return nil
		}
		if ext := matchedIgnoredExtension(entry.Name(), ignoredExts); ext != "" {
			stats.FilesSkippedByExtension[ext]++
			return nil
		}
		if !supported(path) {
			return nil
		}
		if reason, ok := matchedIgnoredPathGlob(rel, false, ignoredPathGlobs, preservedPathGlobs); ok {
			stats.FilesSkippedByUser[reason]++
			return nil
		}
		if isExternalSymlink(scanRoot, path) {
			return nil
		}

		files = append(files, path)
		return nil
	}); err != nil {
		return nil, stats, err
	}

	sort.Strings(files)
	return files, stats, nil
}

func groupFilesByRepository(scanRoot string, files []string) map[string][]string {
	groups := make(map[string][]string)
	repoCache := make(map[string]string)
	for _, file := range files {
		repoRoot := nearestRepositoryRoot(scanRoot, filepath.Dir(file), repoCache)
		if repoRoot == "" {
			repoRoot = scanRoot
		}
		groups[repoRoot] = append(groups[repoRoot], file)
	}
	return groups
}

func nearestRepositoryRoot(scanRoot, dir string, cache map[string]string) string {
	current := filepath.Clean(dir)
	walked := make([]string, 0, 8)
	for {
		if cached, ok := cache[current]; ok {
			for _, path := range walked {
				cache[path] = cached
			}
			return cached
		}

		walked = append(walked, current)
		if hasGitMarker(current) {
			for _, path := range walked {
				cache[path] = current
			}
			return current
		}
		if current == scanRoot {
			break
		}

		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}

	for _, path := range walked {
		cache[path] = ""
	}
	return ""
}

func hasGitMarker(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ".git"))
	if err != nil {
		return false
	}
	return info.IsDir() || info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0
}

func isExternalSymlink(scanRoot, path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return true
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return false
	}

	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return true
	}
	absResolved, err := filepath.Abs(resolved)
	if err != nil {
		return true
	}

	return !pathWithinRoot(scanRoot, absResolved)
}

func pathWithinRoot(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(filepath.Clean(rel))
	return rel == "." || !strings.HasPrefix(rel, "../")
}

func shouldSkipFile(rel string, ignoreHidden bool, preservedHidden []string) bool {
	if !ignoreHidden {
		return false
	}
	return isHiddenPath(rel) && !preservesHiddenPath(rel, preservedHidden)
}

func isIgnoredDir(name string, ignoredDirs map[string]struct{}) bool {
	_, ok := ignoredDirs[strings.ToLower(name)]
	return ok
}

func isHiddenPath(rel string) bool {
	if rel == "." {
		return false
	}
	for _, segment := range strings.Split(rel, "/") {
		if strings.HasPrefix(segment, ".") && segment != "." && segment != ".." {
			return true
		}
	}
	return false
}

func preservesHiddenPath(rel string, preserved []string) bool {
	if len(preserved) == 0 {
		return false
	}
	rel = filepath.ToSlash(filepath.Clean(rel))
	parts := strings.Split(rel, "/")
	for start := 0; start < len(parts); start++ {
		candidate := strings.Join(parts[start:], "/")
		for _, prefix := range preserved {
			if pathPrefixMatches(candidate, prefix) {
				return true
			}
		}
	}
	return false
}

func pathPrefixMatches(path string, prefix string) bool {
	path = filepath.ToSlash(filepath.Clean(path))
	prefix = filepath.ToSlash(filepath.Clean(prefix))
	if path == prefix {
		return true
	}
	if strings.HasPrefix(path, prefix+"/") {
		return true
	}
	return strings.HasPrefix(prefix, path+"/")
}

func normalizeExtensions(exts []string) []string {
	normalized := make([]string, 0, len(exts))
	for _, ext := range exts {
		ext = strings.TrimSpace(ext)
		if ext == "" {
			continue
		}
		normalized = append(normalized, strings.ToLower(ext))
	}
	return normalized
}

// matchedIgnoredExtension returns the specific extension that matched, or ""
// if no extension matched. Used by collectSupportedFiles to record per-extension
// skip counts.
func matchedIgnoredExtension(name string, exts []string) string {
	lower := strings.ToLower(name)
	for _, ext := range exts {
		if strings.HasSuffix(lower, ext) {
			return ext
		}
	}
	return ""
}

func normalizeIgnoredDirs(dirs []string) map[string]struct{} {
	normalized := make(map[string]struct{}, len(dirs))
	for _, dir := range dirs {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		normalized[strings.ToLower(dir)] = struct{}{}
	}
	return normalized
}

func normalizePrefixes(prefixes []string) []string {
	normalized := make([]string, 0, len(prefixes))
	for _, prefix := range prefixes {
		prefix = strings.TrimSpace(prefix)
		if prefix == "" {
			continue
		}
		normalized = append(normalized, filepath.ToSlash(filepath.Clean(prefix)))
	}
	sort.Strings(normalized)
	return normalized
}
