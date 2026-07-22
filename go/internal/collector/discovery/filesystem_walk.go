// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package discovery

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// collectSupportedFiles walks scanRoot once, applying every non-git-aware
// discovery rule (ignored dirs/extensions/hidden paths/user globs) and
// harvesting file sizes. Repo-local .gitignore/.eshuignore filtering happens
// afterward, per repository group, in ResolveRepositoryFileSetsWithStats.
func collectSupportedFiles(
	scanRoot string,
	supported SupportedFileMatcher,
	opts Options,
) ([]FileWithSize, DiscoveryStats, error) {
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
	files := make([]FileWithSize, 0)
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
		external, info, ok := classifyPath(scanRoot, path)
		if !ok || external {
			return nil
		}

		fileEntry := FileWithSize{Path: path}
		if info.Mode()&os.ModeSymlink != 0 {
			// Included symlink: follow-Stat for the target size so
			// partition balancing is byte-identical to the old
			// code that called os.Stat (which follows symlinks).
			if targetInfo, targetErr := os.Stat(path); targetErr == nil {
				fileEntry.Size = targetInfo.Size()
			} else {
				// Target could not be followed — mark unavailable so the
				// partition weighter applies its default, matching the old
				// os.Stat-failure path (not the zero-byte floor).
				fileEntry.Size = SizeUnavailable
			}
		} else {
			// Regular file: size harvested from the single Lstat
			// classifyPath already performed (0 for a genuine empty file).
			fileEntry.Size = info.Size()
		}
		files = append(files, fileEntry)
		return nil
	}); err != nil {
		return nil, stats, err
	}

	sortFileWithSizeSlice(files)
	return files, stats, nil
}

func groupFilesByRepository(scanRoot string, files []FileWithSize) map[string][]FileWithSize {
	groups := make(map[string][]FileWithSize)
	repoCache := make(map[string]string)
	for _, file := range files {
		repoRoot := nearestRepositoryRoot(scanRoot, filepath.Dir(file.Path), repoCache)
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

// classifyPath does a single os.Lstat and returns the symlink-external verdict,
// the FileInfo for size harvesting, and an ok flag. For regular files, info.Size()
// is the on-disk size. For included symlinks the caller must follow-Stat for the
// target size (to match the old partition os.Stat behavior that followed symlinks).
func classifyPath(scanRoot, path string) (external bool, info os.FileInfo, ok bool) {
	info, err := os.Lstat(path)
	if err != nil {
		return true, nil, false
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return false, info, true
	}

	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return true, info, true
	}
	absResolved, err := filepath.Abs(resolved)
	if err != nil {
		return true, info, true
	}
	external = !pathWithinRoot(scanRoot, absResolved)
	return external, info, true
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
