package javascript

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// TSConfigImportResolver resolves TypeScript baseUrl and paths aliases for one
// source file. The zero value is disabled and returns no resolved sources.
type TSConfigImportResolver struct {
	repoRoot string
	baseDir  string
	paths    map[string][]string
	ok       bool
}

type tsConfigOptions struct {
	BaseURL string              `json:"baseUrl"`
	Paths   map[string][]string `json:"paths"`
}

// NewTSConfigImportResolver builds a resolver from the nearest tsconfig.json
// owned by path. It accepts JSONC syntax and rejects absolute or out-of-repo
// baseUrl values so imports cannot resolve outside the indexed repository.
func NewTSConfigImportResolver(repoRoot string, path string) TSConfigImportResolver {
	repoRoot = cleanPath(repoRoot)
	path = cleanPath(path)
	if repoRoot == "" || path == "" {
		return TSConfigImportResolver{}
	}

	configPath, ok := nearestTSConfig(repoRoot, path)
	if !ok {
		return TSConfigImportResolver{}
	}

	options := tsConfigCompilerOptions(configPath)
	baseURL := options.BaseURL
	if baseURL == "" && len(options.Paths) > 0 {
		baseURL = "."
	}
	if baseURL == "" || filepath.IsAbs(baseURL) {
		return TSConfigImportResolver{}
	}

	baseDir := cleanPath(filepath.Join(filepath.Dir(configPath), filepath.FromSlash(baseURL)))
	if !pathWithin(repoRoot, baseDir) {
		return TSConfigImportResolver{}
	}
	return TSConfigImportResolver{
		repoRoot: repoRoot,
		baseDir:  baseDir,
		paths:    options.Paths,
		ok:       true,
	}
}

// ResolveSource returns the repository-relative source file matched by an
// import, or an empty string when the import is external, relative, unresolved,
// or outside the repository.
func (r TSConfigImportResolver) ResolveSource(source string) string {
	source = strings.TrimSpace(source)
	if !r.ok || source == "" || strings.HasPrefix(source, ".") || filepath.IsAbs(source) {
		return ""
	}

	for _, target := range r.resolvePathMappedSources(source) {
		if resolved := r.resolveBaseRelativeSource(target); resolved != "" {
			return resolved
		}
	}
	return r.resolveBaseRelativeSource(source)
}

func (r TSConfigImportResolver) resolveBaseRelativeSource(source string) string {
	basePath := cleanPath(filepath.Join(r.baseDir, filepath.FromSlash(source)))
	for _, candidate := range TSConfigSourceCandidates(basePath) {
		if !pathWithin(r.repoRoot, candidate) {
			continue
		}
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			rel, relErr := filepath.Rel(r.repoRoot, candidate)
			if relErr != nil {
				return ""
			}
			return filepath.ToSlash(rel)
		}
	}
	return ""
}

func nearestTSConfig(repoRoot string, path string) (string, bool) {
	dir := filepath.Dir(path)
	for pathWithin(repoRoot, dir) {
		candidate := filepath.Join(dir, "tsconfig.json")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, true
		}
		if dir == repoRoot {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", false
}

func tsConfigCompilerOptions(path string) tsConfigOptions {
	raw, err := os.ReadFile(path)
	if err != nil {
		return tsConfigOptions{}
	}
	var config struct {
		CompilerOptions tsConfigOptions `json:"compilerOptions"`
	}
	if err := json.Unmarshal(stripJSONC(raw), &config); err != nil {
		return tsConfigOptions{}
	}
	config.CompilerOptions.BaseURL = strings.TrimSpace(config.CompilerOptions.BaseURL)
	return config.CompilerOptions
}

func (r TSConfigImportResolver) resolvePathMappedSources(source string) []string {
	mapped := make([]string, 0)
	for pattern, targets := range r.paths {
		if len(targets) == 0 {
			continue
		}
		if match, ok := tsConfigPathPatternMatch(pattern, source); ok {
			for _, target := range targets {
				target = strings.TrimSpace(target)
				if target == "" || filepath.IsAbs(target) {
					continue
				}
				mapped = appendUniqueString(mapped, strings.ReplaceAll(target, "*", match))
			}
		}
	}
	return mapped
}

func tsConfigPathPatternMatch(pattern string, source string) (string, bool) {
	pattern = strings.TrimSpace(pattern)
	source = strings.TrimSpace(source)
	if pattern == "" || source == "" {
		return "", false
	}
	if !strings.Contains(pattern, "*") {
		return "", pattern == source
	}
	parts := strings.Split(pattern, "*")
	if len(parts) != 2 {
		return "", false
	}
	prefix, suffix := parts[0], parts[1]
	if !strings.HasPrefix(source, prefix) || !strings.HasSuffix(source, suffix) {
		return "", false
	}
	match := strings.TrimSuffix(strings.TrimPrefix(source, prefix), suffix)
	return match, true
}

func stripJSONC(raw []byte) []byte {
	withoutComments := make([]byte, 0, len(raw))
	inString := false
	escaped := false
	for i := 0; i < len(raw); i++ {
		ch := raw[i]
		if inString {
			withoutComments = append(withoutComments, ch)
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		if ch == '"' {
			inString = true
			withoutComments = append(withoutComments, ch)
			continue
		}
		if ch == '/' && i+1 < len(raw) {
			switch raw[i+1] {
			case '/':
				i += 2
				for i < len(raw) && raw[i] != '\n' && raw[i] != '\r' {
					i++
				}
				if i < len(raw) {
					withoutComments = append(withoutComments, raw[i])
				}
				continue
			case '*':
				i += 2
				for i+1 < len(raw) && (raw[i] != '*' || raw[i+1] != '/') {
					i++
				}
				if i+1 < len(raw) {
					i++
				}
				continue
			}
		}
		withoutComments = append(withoutComments, ch)
	}
	return stripJSONCTrailingCommas(withoutComments)
}

func stripJSONCTrailingCommas(raw []byte) []byte {
	out := make([]byte, 0, len(raw))
	inString := false
	escaped := false
	for i := 0; i < len(raw); i++ {
		ch := raw[i]
		if inString {
			out = append(out, ch)
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		if ch == '"' {
			inString = true
			out = append(out, ch)
			continue
		}
		if ch == ',' {
			next := i + 1
			for next < len(raw) && (raw[next] == ' ' || raw[next] == '\t' || raw[next] == '\n' || raw[next] == '\r') {
				next++
			}
			if next < len(raw) && (raw[next] == '}' || raw[next] == ']') {
				continue
			}
		}
		out = append(out, ch)
	}
	return out
}

// TSConfigSourceCandidates returns the deterministic file candidates used to
// resolve an extensionless TypeScript or JavaScript import base path.
func TSConfigSourceCandidates(basePath string) []string {
	candidates := make([]string, 0, 16)
	appendCandidate := func(path string) {
		path = cleanPath(path)
		if path == "" {
			return
		}
		candidates = appendUniqueString(candidates, path)
	}

	appendCandidate(basePath)
	extensions := []string{".js", ".jsx", ".ts", ".tsx", ".d.ts", ".mjs", ".cjs", ".mts", ".cts"}
	if filepath.Ext(basePath) == "" {
		for _, ext := range extensions {
			appendCandidate(basePath + ext)
		}
		for _, ext := range extensions {
			appendCandidate(filepath.Join(basePath, "index"+ext))
		}
	}
	return candidates
}

func cleanPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	return filepath.Clean(abs)
}

func pathWithin(root string, path string) bool {
	root = cleanPath(root)
	path = cleanPath(path)
	if root == "" || path == "" {
		return false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
