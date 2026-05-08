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

	baseURL := javaScriptTSConfigBaseURL(configPath)
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

func javaScriptTSConfigBaseURL(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var config struct {
		CompilerOptions struct {
			BaseURL string `json:"baseUrl"`
		} `json:"compilerOptions"`
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		return ""
	}
	return strings.TrimSpace(config.CompilerOptions.BaseURL)
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
