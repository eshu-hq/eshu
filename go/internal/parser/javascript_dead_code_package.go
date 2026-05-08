package parser

import (
	"encoding/json"
	"os"
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
	manifest, ok := readJavaScriptPackageManifest(repoRoot)
	if !ok {
		return nil
	}
	relativePath, ok := relativeSlashPath(repoRoot, path)
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
			rootKinds = appendUniqueString(rootKinds, "javascript.node_package_entrypoint")
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

func readJavaScriptPackageManifest(repoRoot string) (javaScriptPackageManifest, bool) {
	if strings.TrimSpace(repoRoot) == "" {
		return javaScriptPackageManifest{}, false
	}
	body, err := os.ReadFile(filepath.Join(repoRoot, "package.json"))
	if err != nil {
		return javaScriptPackageManifest{}, false
	}
	var manifest javaScriptPackageManifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return javaScriptPackageManifest{}, false
	}
	return manifest, true
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
	for name, command := range scripts {
		switch name {
		case "start", "dev":
		default:
			continue
		}
		for _, token := range strings.Fields(command) {
			target := strings.Trim(token, `"'`)
			if target == "" || strings.HasPrefix(target, "-") {
				continue
			}
			switch filepath.Ext(target) {
			case ".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs", ".mts", ".cts":
				targets = append(targets, target)
			}
		}
	}
	return targets
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
		if candidate == relativeSourcePath {
			return true
		}
	}
	return false
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
	for _, prefix := range []string{"dist/", "build/"} {
		withoutBuildDir = strings.TrimPrefix(withoutBuildDir, prefix)
	}
	candidates = appendUniqueString(candidates, withoutBuildDir)
	withoutExtension := strings.TrimSuffix(withoutBuildDir, filepath.Ext(withoutBuildDir))
	for _, extension := range []string{".ts", ".tsx", ".js", ".jsx", ".mts", ".cts", ".mjs", ".cjs"} {
		candidates = appendUniqueString(candidates, withoutExtension+extension)
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
