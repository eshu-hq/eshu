package parser

import (
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type javaScriptPackageManifest struct {
	Main    string            `json:"main"`
	Module  string            `json:"module"`
	Types   string            `json:"types"`
	Exports any               `json:"exports"`
	Bin     any               `json:"bin"`
	Scripts map[string]string `json:"scripts"`
}

// javaScriptPackageFileRootKinds returns package-level root evidence for one
// source file. The mapping is intentionally conservative: compiled dist targets
// are mapped back only to same-repository source paths with matching basenames.
func javaScriptPackageFileRootKinds(repoRoot string, path string) []string {
	manifest, packageRoot, ok := nearestJavaScriptPackageManifest(repoRoot, path)
	if !ok {
		return nil
	}
	relativePath, ok := relativeSlashPath(packageRoot, path)
	if !ok {
		return nil
	}

	rootKinds := []string{}
	for _, target := range []string{manifest.Main, manifest.Module} {
		if javaScriptPackageTargetMatchesSource(target, relativePath) {
			rootKinds = appendUniqueString(rootKinds, "javascript.node_package_entrypoint")
		}
	}
	for _, target := range javaScriptPackageBinTargets(manifest.Bin) {
		if javaScriptPackageTargetMatchesSource(target, relativePath) {
			rootKinds = appendUniqueString(rootKinds, "javascript.node_package_bin")
		}
	}
	for _, target := range javaScriptPackageScriptTargets(manifest.Scripts) {
		if javaScriptPackageTargetMatchesSource(target, relativePath) {
			rootKinds = appendUniqueString(rootKinds, "javascript.node_package_script")
		}
	}
	for _, target := range javaScriptPackageExportTargets(manifest.Exports) {
		if javaScriptPackageTargetMatchesSource(target, relativePath) {
			rootKinds = appendUniqueString(rootKinds, "javascript.node_package_export")
		}
	}
	if manifest.Types != "" && javaScriptPackageTargetMatchesSource(manifest.Types, relativePath) {
		rootKinds = appendUniqueString(rootKinds, "javascript.node_package_export")
	}
	return rootKinds
}

func nearestJavaScriptPackageManifest(repoRoot string, path string) (javaScriptPackageManifest, string, bool) {
	packagePath, packageRoot, ok := nearestJavaScriptPackageJSON(repoRoot, path)
	if !ok {
		return javaScriptPackageManifest{}, "", false
	}
	body, err := os.ReadFile(packagePath)
	if err != nil {
		return javaScriptPackageManifest{}, "", false
	}
	var manifest javaScriptPackageManifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return javaScriptPackageManifest{}, "", false
	}
	return manifest, packageRoot, true
}

func nearestJavaScriptPackageRoot(repoRoot string, path string) (string, bool) {
	_, packageRoot, ok := nearestJavaScriptPackageJSON(repoRoot, path)
	return packageRoot, ok
}

func nearestJavaScriptPackageJSON(repoRoot string, path string) (string, string, bool) {
	repoRoot = cleanJavaScriptPath(repoRoot)
	path = cleanJavaScriptPath(path)
	if repoRoot == "" || path == "" {
		return "", "", false
	}
	dir := path
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		dir = filepath.Dir(path)
	}
	for javaScriptPathWithin(repoRoot, dir) {
		candidate := filepath.Join(dir, "package.json")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, dir, true
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
	return "", "", false
}

func javaScriptPackageBinTargets(raw any) []string {
	switch value := raw.(type) {
	case string:
		return []string{value}
	case map[string]any:
		targets := []string{}
		for _, item := range value {
			if target, ok := item.(string); ok {
				targets = append(targets, target)
			}
		}
		return targets
	default:
		return nil
	}
}

func javaScriptPackageScriptTargets(scripts map[string]string) []string {
	if len(scripts) == 0 {
		return nil
	}
	targets := []string{}
	for _, command := range scripts {
		for _, token := range strings.Fields(command) {
			if target, ok := javaScriptPackageScriptTokenTarget(token); ok {
				targets = append(targets, target)
			}
		}
	}
	return targets
}

func javaScriptPackageScriptTokenTarget(token string) (string, bool) {
	target := strings.Trim(strings.TrimSpace(token), `"'`)
	if target == "" || strings.HasPrefix(target, "-") || strings.Contains(target, "=") || strings.Contains(target, "*") {
		return "", false
	}
	if strings.Contains(target, "://") {
		return "", false
	}
	switch filepath.Ext(target) {
	case ".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs", ".mts", ".cts":
		return target, true
	}
	if strings.HasPrefix(target, "./") || strings.HasPrefix(target, "../") || strings.Contains(filepath.ToSlash(target), "/") {
		return target, true
	}
	return "", false
}

func javaScriptPackageExportTargets(exports any) []string {
	targets := []string{}
	var walk func(any)
	walk = func(value any) {
		switch typed := value.(type) {
		case string:
			targets = append(targets, typed)
		case map[string]any:
			for _, child := range typed {
				walk(child)
			}
		case []any:
			for _, child := range typed {
				walk(child)
			}
		}
	}
	walk(exports)
	return targets
}

func javaScriptPackageTargetMatchesSource(target string, relativeSourcePath string) bool {
	target = normalizeJavaScriptPackageTarget(target)
	relativeSourcePath = filepath.ToSlash(filepath.Clean(relativeSourcePath))
	if target == "" || relativeSourcePath == "" {
		return false
	}
	for _, candidate := range javaScriptPackageSourceCandidates(target) {
		if javaScriptPackageSourceCandidateMatches(candidate, relativeSourcePath) {
			return true
		}
	}
	return false
}

func javaScriptPackageSourceCandidateMatches(candidate string, relativeSourcePath string) bool {
	if !strings.Contains(candidate, "*") {
		return candidate == relativeSourcePath
	}
	matched, err := path.Match(candidate, relativeSourcePath)
	return err == nil && matched
}

func normalizeJavaScriptPackageTarget(target string) string {
	target = strings.TrimSpace(target)
	target = strings.TrimPrefix(target, "./")
	target = filepath.ToSlash(filepath.Clean(target))
	if target == "." {
		return ""
	}
	return target
}

func javaScriptPackageSourceCandidates(target string) []string {
	target = normalizeJavaScriptPackageTarget(target)
	if target == "" {
		return nil
	}
	candidates := []string{target}
	withoutBuildDir := target
	for _, prefix := range []string{"dist/", "build/", "lib/"} {
		withoutBuildDir = strings.TrimPrefix(withoutBuildDir, prefix)
	}
	candidates = appendUniqueString(candidates, withoutBuildDir)
	if !strings.HasPrefix(withoutBuildDir, "src/") {
		candidates = appendUniqueString(candidates, "src/"+withoutBuildDir)
	}
	withoutExtension := strings.TrimSuffix(withoutBuildDir, filepath.Ext(withoutBuildDir))
	for _, extension := range []string{".ts", ".tsx", ".js", ".jsx", ".mts", ".cts", ".mjs", ".cjs"} {
		candidates = appendUniqueString(candidates, withoutExtension+extension)
		if !strings.HasPrefix(withoutExtension, "src/") {
			candidates = appendUniqueString(candidates, "src/"+withoutExtension+extension)
		}
	}
	return candidates
}

func relativeSlashPath(repoRoot string, path string) (string, bool) {
	relativePath, err := filepath.Rel(repoRoot, path)
	if err != nil || strings.HasPrefix(relativePath, "..") {
		return "", false
	}
	return filepath.ToSlash(filepath.Clean(relativePath)), true
}

func cleanJavaScriptPath(path string) string {
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
