package parser

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type javaScriptTSConfigImportResolver struct {
	repoRoot string
	baseDir  string
	paths    map[string][]string
	ok       bool
}

func newJavaScriptTSConfigImportResolver(repoRoot string, path string) javaScriptTSConfigImportResolver {
	repoRoot = cleanJavaScriptTSConfigPath(repoRoot)
	path = cleanJavaScriptTSConfigPath(path)
	if repoRoot == "" || path == "" {
		return javaScriptTSConfigImportResolver{}
	}

	configPath, ok := nearestJavaScriptTSConfig(repoRoot, path)
	if !ok {
		return javaScriptTSConfigImportResolver{}
	}

	options := javaScriptTSConfigCompilerOptions(configPath)
	baseURL := options.BaseURL
	if baseURL == "" && len(options.Paths) > 0 {
		baseURL = "."
	}
	if baseURL == "" || filepath.IsAbs(baseURL) {
		return javaScriptTSConfigImportResolver{}
	}

	baseDir := cleanJavaScriptTSConfigPath(filepath.Join(filepath.Dir(configPath), filepath.FromSlash(baseURL)))
	if !javaScriptPathWithin(repoRoot, baseDir) {
		return javaScriptTSConfigImportResolver{}
	}
	return javaScriptTSConfigImportResolver{
		repoRoot: repoRoot,
		baseDir:  baseDir,
		paths:    options.Paths,
		ok:       true,
	}
}

func (r javaScriptTSConfigImportResolver) annotateImport(item map[string]any) {
	if !r.ok || item == nil {
		return
	}
	source, _ := item["source"].(string)
	source = strings.TrimSpace(source)
	if resolved := r.resolveSource(source); resolved != "" {
		item["resolved_source"] = resolved
	}
}

func (r javaScriptTSConfigImportResolver) resolveSource(source string) string {
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

func (r javaScriptTSConfigImportResolver) resolveBaseRelativeSource(source string) string {
	basePath := cleanJavaScriptTSConfigPath(filepath.Join(r.baseDir, filepath.FromSlash(source)))
	for _, candidate := range javaScriptTSConfigSourceCandidates(basePath) {
		if !javaScriptPathWithin(r.repoRoot, candidate) {
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

func nearestJavaScriptTSConfig(repoRoot string, path string) (string, bool) {
	dir := filepath.Dir(path)
	for javaScriptPathWithin(repoRoot, dir) {
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

type javaScriptTSConfigOptions struct {
	BaseURL string              `json:"baseUrl"`
	Paths   map[string][]string `json:"paths"`
}

func javaScriptTSConfigCompilerOptions(path string) javaScriptTSConfigOptions {
	raw, err := os.ReadFile(path)
	if err != nil {
		return javaScriptTSConfigOptions{}
	}
	var config struct {
		CompilerOptions javaScriptTSConfigOptions `json:"compilerOptions"`
	}
	if err := json.Unmarshal(stripJavaScriptJSONC(raw), &config); err != nil {
		return javaScriptTSConfigOptions{}
	}
	config.CompilerOptions.BaseURL = strings.TrimSpace(config.CompilerOptions.BaseURL)
	return config.CompilerOptions
}

func (r javaScriptTSConfigImportResolver) resolvePathMappedSources(source string) []string {
	mapped := make([]string, 0)
	for pattern, targets := range r.paths {
		if len(targets) == 0 {
			continue
		}
		if match, ok := javaScriptTSConfigPathPatternMatch(pattern, source); ok {
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

func javaScriptTSConfigPathPatternMatch(pattern string, source string) (string, bool) {
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

func stripJavaScriptJSONC(raw []byte) []byte {
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
	return stripJavaScriptJSONCTrailingCommas(withoutComments)
}

func stripJavaScriptJSONCTrailingCommas(raw []byte) []byte {
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

func javaScriptTSConfigSourceCandidates(basePath string) []string {
	candidates := make([]string, 0, 16)
	appendCandidate := func(path string) {
		path = cleanJavaScriptTSConfigPath(path)
		if path == "" {
			return
		}
		for _, existing := range candidates {
			if existing == path {
				return
			}
		}
		candidates = append(candidates, path)
	}

	appendCandidate(basePath)
	extensions := []string{".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs", ".mts", ".cts"}
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

func cleanJavaScriptTSConfigPath(path string) string {
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

func javaScriptPathWithin(root string, path string) bool {
	root = cleanJavaScriptTSConfigPath(root)
	path = cleanJavaScriptTSConfigPath(path)
	if root == "" || path == "" {
		return false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
