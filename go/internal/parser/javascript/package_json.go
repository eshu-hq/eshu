package javascript

import (
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type packageManifest struct {
	Main    string            `json:"main"`
	Module  string            `json:"module"`
	Types   string            `json:"types"`
	Exports any               `json:"exports"`
	Bin     any               `json:"bin"`
	Scripts map[string]string `json:"scripts"`
}

// PackageFileRootKinds returns package-level dead-code root evidence for one
// source file. Compiled package targets are mapped back only to same-repository
// source paths with matching basenames.
func PackageFileRootKinds(repoRoot string, path string) []string {
	manifest, packageRoot, ok := nearestPackageManifest(repoRoot, path)
	if !ok {
		return nil
	}
	relativePath, ok := relativeSlashPath(packageRoot, path)
	if !ok {
		return nil
	}

	rootKinds := []string{}
	for _, target := range []string{manifest.Main, manifest.Module} {
		if packageTargetMatchesSource(target, relativePath) {
			rootKinds = appendUniqueString(rootKinds, "javascript.node_package_entrypoint")
		}
	}
	for _, target := range packageBinTargets(manifest.Bin) {
		if packageTargetMatchesSource(target, relativePath) {
			rootKinds = appendUniqueString(rootKinds, "javascript.node_package_bin")
		}
	}
	for _, target := range packageScriptTargets(manifest.Scripts) {
		if packageTargetMatchesSource(target, relativePath) {
			rootKinds = appendUniqueString(rootKinds, "javascript.node_package_script")
		}
	}
	for _, target := range packageExportTargets(manifest.Exports) {
		if packageTargetMatchesSource(target, relativePath) {
			rootKinds = appendUniqueString(rootKinds, "javascript.node_package_export")
		}
	}
	if manifest.Types != "" && packageTargetMatchesSource(manifest.Types, relativePath) {
		rootKinds = appendUniqueString(rootKinds, "javascript.node_package_export")
	}
	return rootKinds
}

// NearestPackageRoot returns the closest owning package.json directory for a
// source path, bounded by repoRoot.
func NearestPackageRoot(repoRoot string, path string) (string, bool) {
	_, packageRoot, ok := nearestPackageJSON(repoRoot, path)
	return packageRoot, ok
}

// PackagePublicSourcePaths returns absolute source paths exposed through the
// nearest package.json exports or types fields.
func PackagePublicSourcePaths(repoRoot string, path string) []string {
	manifest, packageRoot, ok := nearestPackageManifest(repoRoot, path)
	if !ok {
		return nil
	}
	targets := append([]string{}, packageExportTargets(manifest.Exports)...)
	if manifest.Types != "" {
		targets = append(targets, manifest.Types)
	}
	paths := make([]string, 0, len(targets))
	for _, target := range targets {
		for _, candidate := range packageSourceCandidates(target) {
			candidatePath := filepath.Join(packageRoot, filepath.FromSlash(candidate))
			if info, err := os.Stat(candidatePath); err == nil && !info.IsDir() {
				paths = appendUniqueString(paths, cleanPath(candidatePath))
			}
		}
	}
	return paths
}

func nearestPackageManifest(repoRoot string, path string) (packageManifest, string, bool) {
	packagePath, packageRoot, ok := nearestPackageJSON(repoRoot, path)
	if !ok {
		return packageManifest{}, "", false
	}
	body, err := os.ReadFile(packagePath)
	if err != nil {
		return packageManifest{}, "", false
	}
	var manifest packageManifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return packageManifest{}, "", false
	}
	return manifest, packageRoot, true
}

func nearestPackageJSON(repoRoot string, path string) (string, string, bool) {
	repoRoot = cleanPath(repoRoot)
	path = cleanPath(path)
	if repoRoot == "" || path == "" {
		return "", "", false
	}
	dir := path
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		dir = filepath.Dir(path)
	}
	for pathWithin(repoRoot, dir) {
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

func packageBinTargets(raw any) []string {
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

func packageScriptTargets(scripts map[string]string) []string {
	if len(scripts) == 0 {
		return nil
	}
	targets := []string{}
	for _, command := range scripts {
		for _, token := range strings.Fields(command) {
			if target, ok := packageScriptTokenTarget(token); ok {
				targets = append(targets, target)
			}
		}
	}
	return targets
}

func packageScriptTokenTarget(token string) (string, bool) {
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

func packageExportTargets(exports any) []string {
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

func packageTargetMatchesSource(target string, relativeSourcePath string) bool {
	target = normalizePackageTarget(target)
	relativeSourcePath = filepath.ToSlash(filepath.Clean(relativeSourcePath))
	if target == "" || relativeSourcePath == "" {
		return false
	}
	for _, candidate := range packageSourceCandidates(target) {
		if packageSourceCandidateMatches(candidate, relativeSourcePath) {
			return true
		}
	}
	return false
}

func packageSourceCandidateMatches(candidate string, relativeSourcePath string) bool {
	if !strings.Contains(candidate, "*") {
		return candidate == relativeSourcePath
	}
	matched, err := path.Match(candidate, relativeSourcePath)
	return err == nil && matched
}

func normalizePackageTarget(target string) string {
	target = strings.TrimSpace(target)
	target = strings.TrimPrefix(target, "./")
	target = filepath.ToSlash(filepath.Clean(target))
	if target == "." {
		return ""
	}
	return target
}

func packageSourceCandidates(target string) []string {
	target = normalizePackageTarget(target)
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
	for _, extension := range []string{".ts", ".tsx", ".d.ts", ".js", ".jsx", ".mts", ".cts", ".mjs", ".cjs"} {
		candidates = appendUniqueString(candidates, withoutExtension+extension)
		if !strings.HasPrefix(withoutExtension, "src/") {
			candidates = appendUniqueString(candidates, "src/"+withoutExtension+extension)
		}
	}
	return candidates
}
