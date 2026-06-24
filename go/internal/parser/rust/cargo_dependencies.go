// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rust

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

const cargoPackageManager = "cargo"

// IsCargoDependencyFile reports whether path is a Cargo dependency manifest or
// lockfile handled by the Rust parser package.
func IsCargoDependencyFile(path string) bool {
	switch strings.ToLower(filepath.Base(path)) {
	case "cargo.toml", "cargo.lock":
		return true
	default:
		return false
	}
}

// ParseCargoDependencyFile parses Cargo.toml and Cargo.lock files into
// dependency rows consumed by content_entity materialization.
func ParseCargoDependencyFile(repoRoot string, path string, isDependency bool) (map[string]any, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, err
	}
	payload := shared.BasePayload(path, cargoPackageManager, isDependency)
	switch strings.ToLower(filepath.Base(path)) {
	case "cargo.toml":
		var document map[string]any
		if err := toml.Unmarshal(source, &document); err != nil {
			return nil, fmt.Errorf("parse Cargo.toml %q: %w", path, err)
		}
		payload["variables"] = cargoManifestDependencyRows(repoRoot, path, document)
	case "cargo.lock":
		var lock cargoLockDocument
		if err := toml.Unmarshal(source, &lock); err != nil {
			return nil, fmt.Errorf("parse Cargo.lock %q: %w", path, err)
		}
		payload["variables"] = cargoLockDependencyRows(repoRoot, path, lock)
	default:
		return nil, fmt.Errorf("unsupported Cargo dependency file %q", path)
	}
	payload["framework_semantics"] = map[string]any{"frameworks": []string{}}
	return payload, nil
}

type cargoDependencySpec struct {
	ManifestName        string
	PackageName         string
	Version             string
	WorkspaceDependency bool
}

func cargoManifestDependencyRows(repoRoot string, path string, document map[string]any) []map[string]any {
	workspace := cargoWorkspaceDependencies(repoRoot, path, document)
	var rows []map[string]any
	sourcePath := cargoSourcePath(repoRoot, path)
	for _, section := range []string{"dependencies", "dev-dependencies", "build-dependencies"} {
		rows = append(rows, cargoManifestSectionRows(
			documentMap(document, section),
			section,
			cargoDependencyScope(section),
			"",
			sourcePath,
			workspace,
			len(rows)+1,
		)...)
	}
	targets := documentMap(document, "target")
	for _, target := range sortedCargoMapKeys(targets) {
		targetDoc := documentMap(targets, target)
		for _, section := range []string{"dependencies", "dev-dependencies", "build-dependencies"} {
			deps := documentMap(targetDoc, section)
			if len(deps) == 0 {
				continue
			}
			rows = append(rows, cargoManifestSectionRows(
				deps,
				"target."+target+"."+section,
				cargoDependencyScope(section),
				target,
				sourcePath,
				workspace,
				len(rows)+1,
			)...)
		}
	}
	return rows
}

func cargoManifestSectionRows(
	dependencies map[string]any,
	section string,
	scope string,
	target string,
	sourcePath string,
	workspace map[string]cargoDependencySpec,
	startLine int,
) []map[string]any {
	if len(dependencies) == 0 {
		return nil
	}
	names := sortedCargoMapKeys(dependencies)
	rows := make([]map[string]any, 0, len(names))
	for _, manifestName := range names {
		spec, ok := cargoDependencySpecFromValue(manifestName, dependencies[manifestName], workspace)
		if !ok {
			continue
		}
		row := cargoDependencyRow(spec.PackageName, spec.Version, section, scope, sourcePath, startLine+len(rows))
		row["manifest_name"] = spec.ManifestName
		if spec.ManifestName != spec.PackageName {
			row["dependency_alias"] = spec.ManifestName
		}
		if spec.WorkspaceDependency {
			row["workspace_dependency"] = true
		}
		if target != "" {
			if strings.HasPrefix(target, "cfg(") {
				row["target_cfg"] = target
			} else {
				row["target_triple"] = target
			}
		}
		rows = append(rows, row)
	}
	return rows
}

func cargoDependencySpecFromValue(
	manifestName string,
	raw any,
	workspace map[string]cargoDependencySpec,
) (cargoDependencySpec, bool) {
	manifestName = strings.TrimSpace(manifestName)
	if manifestName == "" {
		return cargoDependencySpec{}, false
	}
	spec := cargoDependencySpec{
		ManifestName: manifestName,
		PackageName:  manifestName,
	}
	switch value := raw.(type) {
	case string:
		spec.Version = strings.TrimSpace(value)
		return spec, true
	case map[string]any:
		if cargoBool(value, "workspace") {
			workspaceSpec, ok := workspace[manifestName]
			if !ok {
				return cargoDependencySpec{}, false
			}
			workspaceSpec.ManifestName = manifestName
			workspaceSpec.WorkspaceDependency = true
			return workspaceSpec, true
		}
		if packageName := cargoString(value, "package"); packageName != "" {
			spec.PackageName = packageName
		}
		spec.Version = cargoString(value, "version")
		return spec, true
	default:
		return cargoDependencySpec{}, false
	}
}

func cargoWorkspaceDependencies(repoRoot string, path string, document map[string]any) map[string]cargoDependencySpec {
	out := cargoWorkspaceDependenciesFromDocument(document)
	for key, value := range cargoAncestorWorkspaceDependencies(repoRoot, path) {
		if _, exists := out[key]; !exists {
			out[key] = value
		}
	}
	return out
}

func cargoAncestorWorkspaceDependencies(repoRoot string, path string) map[string]cargoDependencySpec {
	out := map[string]cargoDependencySpec{}
	root, rootErr := filepath.Abs(repoRoot)
	current, currentErr := filepath.Abs(filepath.Dir(path))
	if rootErr != nil || currentErr != nil {
		return out
	}
	currentFileDir := current
	for {
		if current == currentFileDir {
			// The caller already parsed the current document.
		} else {
			manifest := filepath.Join(current, "Cargo.toml")
			if source, err := shared.ReadSource(manifest); err == nil {
				var document map[string]any
				if toml.Unmarshal(source, &document) == nil {
					for key, value := range cargoWorkspaceDependenciesFromDocument(document) {
						out[key] = value
					}
				}
			}
		}
		if current == root || current == filepath.Dir(current) {
			return out
		}
		rel, err := filepath.Rel(root, current)
		if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
			return out
		}
		current = filepath.Dir(current)
	}
}

func cargoWorkspaceDependenciesFromDocument(document map[string]any) map[string]cargoDependencySpec {
	deps := documentMap(documentMap(document, "workspace"), "dependencies")
	out := make(map[string]cargoDependencySpec, len(deps))
	for _, name := range sortedCargoMapKeys(deps) {
		spec, ok := cargoDependencySpecFromWorkspaceValue(name, deps[name])
		if ok {
			out[name] = spec
		}
	}
	return out
}

func cargoDependencySpecFromWorkspaceValue(name string, raw any) (cargoDependencySpec, bool) {
	spec := cargoDependencySpec{
		ManifestName: strings.TrimSpace(name),
		PackageName:  strings.TrimSpace(name),
	}
	if spec.ManifestName == "" {
		return cargoDependencySpec{}, false
	}
	switch value := raw.(type) {
	case string:
		spec.Version = strings.TrimSpace(value)
	case map[string]any:
		if packageName := cargoString(value, "package"); packageName != "" {
			spec.PackageName = packageName
		}
		spec.Version = cargoString(value, "version")
	default:
		return cargoDependencySpec{}, false
	}
	return spec, true
}

func cargoDependencyRow(name, value, section, scope, sourcePath string, lineNumber int) map[string]any {
	return map[string]any{
		"name":             strings.TrimSpace(name),
		"line_number":      lineNumber,
		"value":            strings.TrimSpace(value),
		"section":          strings.TrimSpace(section),
		"config_kind":      "dependency",
		"package_manager":  cargoPackageManager,
		"dependency_scope": strings.TrimSpace(scope),
		"source_path":      sourcePath,
		"lang":             cargoPackageManager,
	}
}

func cargoDependencyScope(section string) string {
	switch section {
	case "dev-dependencies":
		return "dev"
	case "build-dependencies":
		return "build"
	default:
		return "runtime"
	}
}

func documentMap(document map[string]any, key string) map[string]any {
	raw, ok := document[key]
	if !ok {
		return nil
	}
	typed, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	return typed
}

func cargoString(document map[string]any, key string) string {
	value, ok := document[key].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func cargoBool(document map[string]any, key string) bool {
	value, ok := document[key].(bool)
	return ok && value
}

func sortedCargoMapKeys(values map[string]any) []string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func cargoSourcePath(repoRoot string, path string) string {
	rel, err := filepath.Rel(repoRoot, path)
	if err != nil || rel == "." || rel == "" || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}
