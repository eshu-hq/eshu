// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package project

import (
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

type packageManifest struct {
	// Name stays untyped so a malformed non-string name cannot fail the
	// whole manifest decode that the dead-code root rules also depend on.
	Name any `json:"name"`
	// The dependency fields stay untyped for the same reason; only an object
	// value counts as a declaration (DeclaredDependencies).
	Dependencies         any               `json:"dependencies"`
	DevDependencies      any               `json:"devDependencies"`
	PeerDependencies     any               `json:"peerDependencies"`
	OptionalDependencies any               `json:"optionalDependencies"`
	Main                 string            `json:"main"`
	Module               string            `json:"module"`
	Types                string            `json:"types"`
	Exports              any               `json:"exports"`
	Bin                  any               `json:"bin"`
	Scripts              map[string]string `json:"scripts"`
}

// PackageFileRootKinds returns package-level dead-code root evidence for one
// source file. Compiled package targets are mapped back only to same-repository
// source paths with matching basenames.
func PackageFileRootKinds(repoRoot string, path string) []string {
	manifest, packageRoot, ok := nearestPackageManifest(repoRoot, path)
	if !ok {
		return nil
	}
	relativePath, ok := RelativeSlashPath(packageRoot, path)
	if !ok {
		return nil
	}

	rootKinds := []string{}
	for _, target := range []string{manifest.Main, manifest.Module} {
		if packageTargetMatchesSource(target, relativePath) {
			rootKinds = shared.AppendUniqueString(rootKinds, "javascript.node_package_entrypoint")
		}
	}
	for _, target := range packageBinTargets(manifest.Bin) {
		if packageTargetMatchesSource(target, relativePath) {
			rootKinds = shared.AppendUniqueString(rootKinds, "javascript.node_package_bin")
		}
	}
	for _, target := range packageScriptTargets(manifest.Scripts) {
		if packageTargetMatchesSource(target, relativePath) {
			rootKinds = shared.AppendUniqueString(rootKinds, "javascript.node_package_script")
		}
	}
	for _, target := range packageExportTargets(manifest.Exports) {
		if packageTargetMatchesSource(target, relativePath) {
			rootKinds = shared.AppendUniqueString(rootKinds, "javascript.node_package_export")
		}
	}
	if manifest.Types != "" && packageTargetMatchesSource(manifest.Types, relativePath) {
		rootKinds = shared.AppendUniqueString(rootKinds, "javascript.node_package_export")
	}
	return rootKinds
}

// NearestPackageRoot returns the closest owning package.json directory for a
// source path, bounded by repoRoot.
func NearestPackageRoot(repoRoot string, path string) (string, bool) {
	_, packageRoot, ok := nearestPackageJSON(repoRoot, path)
	return packageRoot, ok
}

// NearestPackageName returns the trimmed "name" of the nearest package.json
// that owns path, bounded by repoRoot. It returns "" when no manifest owns the
// path or the nearest one has no usable string name; an outer manifest is not
// consulted, because the nearest manifest is the package that publishes path.
func NearestPackageName(repoRoot string, path string) string {
	manifest, _, ok := nearestPackageManifest(repoRoot, path)
	if !ok {
		return ""
	}
	name, _ := manifest.Name.(string)
	return strings.TrimSpace(name)
}

// DeclaredDependencies returns the package names declared in the
// dependencies, devDependencies, peerDependencies, or optionalDependencies
// object of any package.json on the path from path's directory up to repoRoot.
// It is a union, so a workspace package sees dependencies hoisted to the
// repository root as well as its own. A field that is not an object declares
// nothing, and an unreadable manifest is skipped. Each manifest is read through
// the (path, stat) cache.
func DeclaredDependencies(repoRoot string, path string) map[string]struct{} {
	declared := map[string]struct{}{}
	for name := range declaredDependencySpecs(repoRoot, path) {
		declared[name] = struct{}{}
	}
	return declared
}

// NpmAliasTargets maps the npm-alias dependencies (`"alias": "npm:target@range"`)
// visible from path to the target package a producer publishes. It walks the
// same manifests as DeclaredDependencies, nearest first, so a nested manifest
// wins when two manifests declare one alias. An alias whose target does not
// parse as a bare package specifier maps to "", and callers must leave such a
// call unresolved: keying the alias would miss at best and match an unrelated
// publisher of the alias at worst.
func NpmAliasTargets(repoRoot string, path string) map[string]string {
	targets := map[string]string{}
	for name, spec := range declaredDependencySpecs(repoRoot, path) {
		if !strings.HasPrefix(spec, "npm:") {
			continue
		}
		target, ok := parseNpmAliasTarget(spec)
		if !ok {
			targets[name] = ""
			continue
		}
		targets[name] = target
	}
	return targets
}

// declaredDependencySpecs returns the raw version specs of every dependency
// declared on the path from path's directory up to repoRoot, walking the same
// manifests as DeclaredDependencies nearest first. A name declared by two
// manifests keeps the nearest spec; a non-string spec decodes as "".
func declaredDependencySpecs(repoRoot string, path string) map[string]string {
	specs := map[string]string{}
	repoRoot = CleanPath(repoRoot)
	path = CleanPath(path)
	if repoRoot == "" || path == "" {
		return specs
	}
	dir := path
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		dir = filepath.Dir(path)
	}
	for PathWithin(repoRoot, dir) {
		if manifest, ok := cachedPackageManifest(filepath.Join(dir, "package.json")); ok {
			for _, field := range []any{
				manifest.Dependencies,
				manifest.DevDependencies,
				manifest.PeerDependencies,
				manifest.OptionalDependencies,
			} {
				dependencies, _ := field.(map[string]any)
				for name, raw := range dependencies {
					name = strings.TrimSpace(name)
					if _, seen := specs[name]; seen {
						continue
					}
					spec, _ := raw.(string)
					specs[name] = strings.TrimSpace(spec)
				}
			}
		}
		parent := filepath.Dir(dir)
		if dir == repoRoot || parent == dir {
			break
		}
		dir = parent
	}
	return specs
}

// parseNpmAliasTarget returns the target package of an `npm:` dependency spec
// (`npm:bar@1` -> `bar`, `npm:@scope/bar@^2` -> `@scope/bar`), or false when
// the target is not a bare package specifier.
func parseNpmAliasTarget(spec string) (string, bool) {
	rest := strings.TrimSpace(strings.TrimPrefix(spec, "npm:"))
	if rest == "" {
		return "", false
	}
	target := rest
	if strings.HasPrefix(target, "@") {
		if at := strings.Index(target[1:], "@"); at >= 0 {
			target = target[:1+at]
		}
	} else if at := strings.Index(target, "@"); at >= 0 {
		target = target[:at]
	}
	if !isBareNpmPackageName(target) {
		return "", false
	}
	return target, true
}

// isBareNpmPackageName reports whether name is a whole package (`pkg` or
// `@scope/pkg`) rather than a subpath, relative path, URL, or protocol form.
func isBareNpmPackageName(name string) bool {
	if name == "" || strings.ContainsAny(name, " \t\r\n\\:") {
		return false
	}
	switch name[0] {
	case '.', '/', '#':
		return false
	}
	if scope, rest, ok := strings.Cut(name, "/"); ok {
		return strings.HasPrefix(scope, "@") && len(scope) > 1 && rest != "" && !strings.Contains(rest, "/")
	}
	return !strings.HasPrefix(name, "@")
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
				paths = shared.AppendUniqueString(paths, CleanPath(candidatePath))
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
	manifest, ok := cachedPackageManifest(packagePath)
	if !ok {
		return packageManifest{}, "", false
	}
	return manifest, packageRoot, true
}

func nearestPackageJSON(repoRoot string, path string) (string, string, bool) {
	repoRoot = CleanPath(repoRoot)
	path = CleanPath(path)
	if repoRoot == "" || path == "" {
		return "", "", false
	}
	dir := path
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		dir = filepath.Dir(path)
	}
	for PathWithin(repoRoot, dir) {
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
	candidates = shared.AppendUniqueString(candidates, withoutBuildDir)
	if !strings.HasPrefix(withoutBuildDir, "src/") {
		candidates = shared.AppendUniqueString(candidates, "src/"+withoutBuildDir)
	}
	withoutExtension := strings.TrimSuffix(withoutBuildDir, filepath.Ext(withoutBuildDir))
	if strings.HasSuffix(withoutBuildDir, ".d.ts") {
		withoutExtension = strings.TrimSuffix(withoutBuildDir, ".d.ts")
	}
	for _, extension := range []string{".ts", ".tsx", ".d.ts", ".js", ".jsx", ".mts", ".cts", ".mjs", ".cjs"} {
		candidates = shared.AppendUniqueString(candidates, withoutExtension+extension)
		if !strings.HasPrefix(withoutExtension, "src/") {
			candidates = shared.AppendUniqueString(candidates, "src/"+withoutExtension+extension)
		}
	}
	return candidates
}
