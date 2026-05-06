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

func filterRepoFilesByGitignore(repoRoot string, files []string) []string {
	return filterRepoFilesByIgnoreFile(repoRoot, files, ".gitignore")
}

func filterRepoFilesByEshuIgnore(repoRoot string, files []string) []string {
	return filterRepoFilesByIgnoreFile(repoRoot, files, ".eshuignore")
}

func filterRepoFilesByIgnoreFile(repoRoot string, files []string, ignoreFileName string) []string {
	cache := make(map[string]*gitignoreSpec)
	kept := make([]string, 0, len(files))
	for _, file := range files {
		if !isIgnoredByRepoIgnoreFile(repoRoot, file, ignoreFileName, cache) {
			kept = append(kept, file)
		}
	}
	return kept
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

	if strings.Contains(p.raw, "/") {
		if ok, _ := path.Match(p.raw, rel); ok {
			return true
		}
		if p.anchored {
			return false
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
