// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package nodelockfile

import (
	"sort"
	"strings"

	yamlv3 "gopkg.in/yaml.v3"
)

// parsePnpmLockfile parses pnpm-lock.yaml (v6+) into dependency rows. The
// lockfile is YAML; we decode it with yaml.v3 into a generic document tree.
// We do not require unknown-field strictness because pnpm has rolled new
// top-level fields (settings, snapshots, time, etc.) across point releases
// and we tolerate them by reading only the keys we understand. v9+ moves
// transitive details into a separate snapshots section but the per-package
// `packages:` map still carries the resolved version in the key and a
// dependencies block, which is what we need for chain evidence.
//
// Multiple installed versions of the same name (e.g. lodash@4.x consumed by
// one importer and lodash@3.x consumed by another) are recorded as separate
// rows. All internal maps are keyed by "name@version" so different versions
// do not silently overwrite each other in the dependency graph.
func parsePnpmLockfile(source []byte, payload map[string]any) []map[string]any {
	var document map[string]any
	if err := yamlv3.Unmarshal(source, &document); err != nil {
		payload["lockfile_parse_state"] = "malformed"
		return []map[string]any{}
	}
	if len(document) == 0 {
		payload["lockfile_parse_state"] = "empty"
		return []map[string]any{}
	}

	imports := parsePnpmImporters(document)
	packages := parsePnpmPackages(document)
	snapshots := parsePnpmSnapshots(document)

	// directScopeByKey records runtime/dev/optional/peer scope for the
	// resolved package instance an importer points at. Local protocols
	// (workspace:, link:, file:, portal:) never become remote rows.
	directScopeByKey := make(map[string]string, len(imports))
	for _, dep := range imports {
		if isLocalSpecifier(dep.specifier) || isLocalVersion(dep.version) {
			continue
		}
		// Prefer runtime scope over dev when the same package shows up twice.
		key := pnpmInstanceKey(dep.name, dep.version)
		if existing := directScopeByKey[key]; existing == "runtime" {
			continue
		}
		directScopeByKey[key] = dep.scope
	}

	// Build the adjacency map by composite (name, version) key so two
	// instances of the same name resolve their dependencies independently.
	depAdjacency := make(map[string][]string, len(packages))
	for key, pkg := range packages {
		depAdjacency[key] = pnpmChildKeysForDeps(pkg.deps, packages)
	}
	for key, snapDeps := range snapshots {
		if existing := depAdjacency[key]; len(existing) == 0 && len(snapDeps) > 0 {
			depAdjacency[key] = pnpmChildKeysForDeps(snapDeps, packages)
		}
	}

	// Roots are the importer-declared instance keys that resolve to a real
	// package entry. Chain reconstruction starts from these.
	roots := make([]string, 0, len(directScopeByKey))
	for key := range directScopeByKey {
		if _, ok := packages[key]; ok {
			roots = append(roots, key)
		}
	}
	sort.Strings(roots)

	nameByInstance := make(map[string]string, len(packages))
	for key, pkg := range packages {
		nameByInstance[key] = pkg.name
	}

	chains := make(map[string][]string)
	for _, root := range roots {
		rootName := packages[root].name
		walkChainByInstance(root, []string{rootName}, depAdjacency, nameByInstance, chains)
	}

	keys := make([]string, 0, len(packages))
	for key := range packages {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	rows := make([]map[string]any, 0, len(keys))
	for index, key := range keys {
		pkg := packages[key]
		scope := "pnpm-package"
		if directScope, ok := directScopeByKey[key]; ok {
			scope = directScope
		}
		chain := chains[key]
		if len(chain) == 0 {
			chain = []string{pkg.name}
		}
		row := dependencyRow(pkg.name, pkg.version, scope, "pnpm", "pnpm", index+1, chain)
		rows = append(rows, row)
	}
	return rows
}

type pnpmDirectDependency struct {
	name      string
	scope     string
	specifier string
	version   string
}

// parsePnpmImporters walks importers[*].dependencies and devDependencies.
// Each entry is `name: {specifier, version}` (v6+) or `name: version` (v5/legacy).
func parsePnpmImporters(document map[string]any) []pnpmDirectDependency {
	out := make([]pnpmDirectDependency, 0)
	importers, ok := document["importers"].(map[string]any)
	if !ok {
		return out
	}
	for _, importerName := range sortedMapKeys(importers) {
		importer, ok := importers[importerName].(map[string]any)
		if !ok {
			continue
		}
		for _, scope := range []struct {
			key, label string
		}{
			{"dependencies", "runtime"},
			{"devDependencies", "dev"},
			{"optionalDependencies", "optional"},
			{"peerDependencies", "peer"},
		} {
			block, ok := importer[scope.key].(map[string]any)
			if !ok {
				continue
			}
			for _, name := range sortedMapKeys(block) {
				dep := pnpmDirectDependency{name: name, scope: scope.label}
				switch typed := block[name].(type) {
				case string:
					dep.version = stripPnpmPeerSuffix(typed)
					dep.specifier = typed
				case map[string]any:
					dep.specifier = stringField(typed, "specifier")
					dep.version = stripPnpmPeerSuffix(stringField(typed, "version"))
				}
				out = append(out, dep)
			}
		}
	}
	return out
}

type pnpmPackage struct {
	name    string
	version string
	deps    map[string]string
}

// parsePnpmPackages collects every entry from the `packages:` section. The
// keys look like `/name@version`, `/@scope/name@version`, or for v9+
// `name@version`. Suffixes after the version like `(peer@1)` are part of the
// key but not part of the installed version - we strip them. Returned map is
// keyed by composite "name@version" so multiple resolved versions of the
// same package name stay independent.
func parsePnpmPackages(document map[string]any) map[string]pnpmPackage {
	out := make(map[string]pnpmPackage)
	packages, ok := document["packages"].(map[string]any)
	if !ok {
		return out
	}
	for key, value := range packages {
		name, version := parsePnpmPackageKey(key)
		if name == "" || version == "" {
			continue
		}
		entry, _ := value.(map[string]any)
		deps := readPnpmDependencies(entry)
		out[pnpmInstanceKey(name, version)] = pnpmPackage{
			name:    name,
			version: version,
			deps:    deps,
		}
	}
	return out
}

// parsePnpmSnapshots covers pnpm v9 where transitive dependency edges moved
// into the `snapshots:` section. The map is keyed by composite
// "name@version" so transitive edges for one version of a package never
// overwrite another.
func parsePnpmSnapshots(document map[string]any) map[string]map[string]string {
	out := make(map[string]map[string]string)
	snapshots, ok := document["snapshots"].(map[string]any)
	if !ok {
		return out
	}
	for key, value := range snapshots {
		name, version := parsePnpmPackageKey(key)
		if name == "" || version == "" {
			continue
		}
		entry, _ := value.(map[string]any)
		if deps := readPnpmDependencies(entry); len(deps) > 0 {
			out[pnpmInstanceKey(name, version)] = deps
		}
	}
	return out
}

// parsePnpmPackageKey decodes `/lodash@4.17.21`, `/@scope/api@1.2.3`, or
// `vitest@2.0.0(@types/node@20.5.0)` into (name, version) with any trailing
// peer-suffix stripped.
func parsePnpmPackageKey(key string) (string, string) {
	key = strings.TrimPrefix(strings.TrimSpace(key), "/")
	if key == "" {
		return "", ""
	}
	// Strip trailing peer suffix like `(@types/node@20.5.0)`.
	if idx := strings.IndexByte(key, '('); idx > 0 {
		key = key[:idx]
	}
	prefix := ""
	working := key
	if strings.HasPrefix(working, "@") {
		prefix = "@"
		working = working[1:]
	}
	atIndex := strings.LastIndex(working, "@")
	if atIndex <= 0 {
		return "", ""
	}
	name := prefix + working[:atIndex]
	version := working[atIndex+1:]
	return strings.TrimSpace(name), strings.TrimSpace(version)
}

func readPnpmDependencies(entry map[string]any) map[string]string {
	out := make(map[string]string)
	if entry == nil {
		return out
	}
	for _, key := range []string{"dependencies", "optionalDependencies", "peerDependencies"} {
		block, ok := entry[key].(map[string]any)
		if !ok {
			continue
		}
		for name, value := range block {
			version, _ := value.(string)
			version = stripPnpmPeerSuffix(strings.TrimSpace(version))
			out[strings.TrimSpace(name)] = version
		}
	}
	return out
}

func stringField(entry map[string]any, key string) string {
	if entry == nil {
		return ""
	}
	value, _ := entry[key].(string)
	return strings.TrimSpace(value)
}

func sortedMapKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// pnpmInstanceKey builds the composite key used to index pnpm package
// instances. Using a name+version key prevents multiple installed versions
// of the same package from overwriting each other in adjacency maps.
func pnpmInstanceKey(name, version string) string {
	return strings.TrimSpace(name) + "@" + strings.TrimSpace(version)
}

// pnpmChildKeysForDeps resolves a package's dependency block (name -> child
// version) into the set of child instance keys that exist in the packages
// table. Children whose version does not match a recorded package are
// dropped from the chain instead of being silently coerced.
func pnpmChildKeysForDeps(deps map[string]string, packages map[string]pnpmPackage) []string {
	if len(deps) == 0 {
		return nil
	}
	out := make([]string, 0, len(deps))
	for _, name := range sortedKeysString(deps) {
		version := deps[name]
		key := pnpmInstanceKey(name, version)
		if _, ok := packages[key]; ok {
			out = append(out, key)
		}
	}
	return out
}

func sortedKeysString(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// stripPnpmPeerSuffix removes the parenthesized peer-suffix pnpm adds to
// version strings like "2.0.0(@types/node@20.5.0)".
func stripPnpmPeerSuffix(value string) string {
	value = strings.TrimSpace(value)
	if idx := strings.IndexByte(value, '('); idx > 0 {
		return strings.TrimSpace(value[:idx])
	}
	return value
}

// isLocalSpecifier covers the importer specifier protocols pnpm uses to point
// at local or workspace code (`workspace:*`, `file:./path`, `link:./path`,
// `portal:./path`). Lockfile evidence for these does not prove a remote
// registry identity.
func isLocalSpecifier(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	lower := strings.ToLower(value)
	switch {
	case strings.HasPrefix(lower, "workspace:"),
		strings.HasPrefix(lower, "file:"),
		strings.HasPrefix(lower, "link:"),
		strings.HasPrefix(lower, "portal:"):
		return true
	}
	return false
}

func isLocalVersion(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	lower := strings.ToLower(value)
	switch {
	case strings.HasPrefix(lower, "link:"),
		strings.HasPrefix(lower, "file:"),
		strings.HasPrefix(lower, "workspace:"),
		strings.HasPrefix(lower, "portal:"):
		return true
	}
	return false
}
