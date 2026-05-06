package discovery

import (
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

type normalizedPathGlobRule struct {
	pattern string
	reason  string
}

func matchedIgnoredPathGlob(
	rel string,
	isDir bool,
	ignored []normalizedPathGlobRule,
	preserved []string,
) (string, bool) {
	if len(ignored) == 0 {
		return "", false
	}
	rel = normalizeRepoRelativePath(rel)
	if rel == "" || pathMatchesAnyGlob(rel, preserved) {
		return "", false
	}
	for _, rule := range ignored {
		if !pathGlobMatches(rel, rule.pattern) {
			continue
		}
		if isDir && pathMayContainPreservedGlob(rel, preserved) {
			return "", false
		}
		return rule.reason, true
	}
	return "", false
}

func pathMatchesAnyGlob(rel string, patterns []string) bool {
	for _, pattern := range patterns {
		if pathGlobMatches(rel, pattern) {
			return true
		}
	}
	return false
}

func pathMayContainPreservedGlob(dirRel string, preserved []string) bool {
	if len(preserved) == 0 {
		return false
	}
	dirRel = normalizeRepoRelativePath(dirRel)
	for _, pattern := range preserved {
		literal := pathGlobLiteralPrefix(pattern)
		if literal == "" {
			return true
		}
		if literal == dirRel ||
			strings.HasPrefix(literal, dirRel+"/") ||
			strings.HasPrefix(dirRel, literal) {
			return true
		}
	}
	return false
}

func pathGlobMatches(rel string, pattern string) bool {
	rel = normalizeRepoRelativePath(rel)
	pattern = normalizeRepoRelativePath(pattern)
	if rel == "" || pattern == "" {
		return false
	}
	if strings.HasSuffix(pattern, "/**") {
		basePattern := strings.TrimSuffix(pattern, "/**")
		return pathPrefixGlobMatches(rel, basePattern)
	}
	if matched, err := pathpkg.Match(pattern, rel); err == nil && matched {
		return true
	}
	return rel == pattern
}

func pathPrefixGlobMatches(rel string, basePattern string) bool {
	if basePattern == "" {
		return false
	}
	parts := strings.Split(rel, "/")
	for i := 1; i <= len(parts); i++ {
		candidate := strings.Join(parts[:i], "/")
		if matched, err := pathpkg.Match(basePattern, candidate); err == nil && matched {
			return true
		}
	}
	return rel == basePattern || strings.HasPrefix(rel, basePattern+"/")
}

func pathGlobLiteralPrefix(pattern string) string {
	pattern = normalizeRepoRelativePath(pattern)
	wildcard := strings.IndexAny(pattern, "*?[")
	if wildcard >= 0 {
		pattern = pattern[:wildcard]
	}
	pattern = strings.TrimSuffix(pattern, "/")
	if pattern == "" {
		return ""
	}
	return normalizeRepoRelativePath(pattern)
}

func normalizePathGlobRules(rules []PathGlobRule) []normalizedPathGlobRule {
	normalized := make([]normalizedPathGlobRule, 0, len(rules))
	for _, rule := range rules {
		pattern := normalizeRepoRelativePath(rule.Pattern)
		if pattern == "" {
			continue
		}
		normalized = append(normalized, normalizedPathGlobRule{
			pattern: pattern,
			reason:  normalizeSkipReason(rule.Reason),
		})
	}
	sort.Slice(normalized, func(i, j int) bool {
		if normalized[i].pattern == normalized[j].pattern {
			return normalized[i].reason < normalized[j].reason
		}
		return normalized[i].pattern < normalized[j].pattern
	})
	return normalized
}

func normalizePathGlobPatterns(patterns []string) []string {
	normalized := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		pattern = normalizeRepoRelativePath(pattern)
		if pattern == "" {
			continue
		}
		normalized = append(normalized, pattern)
	}
	sort.Strings(normalized)
	return normalized
}

func normalizeRepoRelativePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	path = filepath.ToSlash(path)
	for strings.HasPrefix(path, "./") {
		path = strings.TrimPrefix(path, "./")
	}
	path = strings.TrimPrefix(path, "/")
	path = filepath.ToSlash(filepath.Clean(path))
	if path == "." {
		return ""
	}
	return path
}

func normalizeSkipReason(reason string) string {
	reason = strings.TrimSpace(strings.ToLower(reason))
	if reason == "" {
		return "configured"
	}
	var builder strings.Builder
	lastDash := false
	for _, r := range reason {
		allowed := unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '.' || r == '-'
		if allowed {
			builder.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			builder.WriteByte('-')
			lastDash = true
		}
	}
	normalized := strings.Trim(builder.String(), "-")
	if normalized == "" {
		return "configured"
	}
	return normalized
}
