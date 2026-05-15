package collector

import (
	"os"
	"path"
	"path/filepath"
	"strings"
)

type collectorGitignoreSpec struct {
	patterns []collectorGitignorePattern
}

type collectorGitignorePattern struct {
	raw      string
	negated  bool
	dirOnly  bool
	anchored bool
	hasGlob  bool
}

func isCollectorGitignoredInRepo(
	repoRoot string,
	filePath string,
	cache map[string]*collectorGitignoreSpec,
) bool {
	return isCollectorIgnoredInRepo(repoRoot, filePath, ".gitignore", cache)
}

func isCollectorEshuignoredInRepo(
	repoRoot string,
	filePath string,
	cache map[string]*collectorGitignoreSpec,
) bool {
	return isCollectorIgnoredInRepo(repoRoot, filePath, ".eshuignore", cache)
}

func isCollectorIgnoredInRepo(
	repoRoot string,
	filePath string,
	ignoreFileName string,
	cache map[string]*collectorGitignoreSpec,
) bool {
	if !collectorPathWithinRoot(repoRoot, filePath) {
		return false
	}
	ignored := false
	for _, dir := range collectorAncestorDirs(repoRoot, filePath) {
		spec := loadCollectorGitignoreSpec(filepath.Join(dir, ignoreFileName), cache)
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

func collectorPathWithinRoot(root string, target string) bool {
	root = filepath.Clean(root)
	target = filepath.Clean(target)
	if root == target {
		return true
	}
	return strings.HasPrefix(target, root+string(os.PathSeparator))
}

func collectorAncestorDirs(repoRoot string, filePath string) []string {
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

func loadCollectorGitignoreSpec(path string, cache map[string]*collectorGitignoreSpec) *collectorGitignoreSpec {
	normalized := filepath.Clean(path)
	if spec, ok := cache[normalized]; ok {
		return spec
	}
	contents, err := os.ReadFile(normalized)
	if err != nil {
		cache[normalized] = nil
		return nil
	}
	spec := parseCollectorGitignoreSpec(strings.Split(string(contents), "\n"))
	cache[normalized] = spec
	return spec
}

func parseCollectorGitignoreSpec(lines []string) *collectorGitignoreSpec {
	patterns := make([]collectorGitignorePattern, 0, len(lines))
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		pattern := collectorGitignorePattern{}
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
		pattern.hasGlob = strings.ContainsAny(pattern.raw, "*?[")
		patterns = append(patterns, pattern)
	}
	if len(patterns) == 0 {
		return nil
	}
	return &collectorGitignoreSpec{patterns: patterns}
}

func (p collectorGitignorePattern) matches(rel string) bool {
	rel = filepath.ToSlash(filepath.Clean(rel))
	if rel == "." {
		return false
	}
	if !p.hasGlob {
		return p.matchesLiteral(rel)
	}
	return p.matchesGlob(rel)
}

func (p collectorGitignorePattern) matchesLiteral(rel string) bool {
	if p.dirOnly {
		return rel == p.raw || strings.HasPrefix(rel, p.raw+"/")
	}
	if strings.Contains(p.raw, "/") {
		if rel == p.raw {
			return true
		}
		return !p.anchored && strings.HasSuffix(rel, "/"+p.raw)
	}
	return path.Base(rel) == p.raw || collectorPathHasSegment(rel, p.raw)
}

func (p collectorGitignorePattern) matchesGlob(rel string) bool {
	if p.dirOnly {
		return rel == p.raw || strings.HasPrefix(rel, p.raw+"/")
	}
	if strings.Contains(p.raw, "/") {
		if matched, _ := path.Match(p.raw, rel); matched {
			return true
		}
		if p.anchored {
			return false
		}
		for _, candidate := range collectorSuffixCandidates(rel) {
			if matched, _ := path.Match(p.raw, candidate); matched {
				return true
			}
		}
		return false
	}
	base := path.Base(rel)
	if matched, _ := path.Match(p.raw, base); matched {
		return true
	}
	for _, segment := range strings.Split(rel, "/") {
		if matched, _ := path.Match(p.raw, segment); matched {
			return true
		}
	}
	return false
}

func collectorPathHasSegment(rel string, segment string) bool {
	for {
		before, after, ok := strings.Cut(rel, "/")
		if !ok {
			return rel == segment
		}
		if before == segment {
			return true
		}
		rel = after
	}
}

func collectorSuffixCandidates(rel string) []string {
	parts := strings.Split(rel, "/")
	candidates := make([]string, 0, len(parts))
	for i := range parts {
		candidates = append(candidates, strings.Join(parts[i:], "/"))
	}
	return candidates
}
