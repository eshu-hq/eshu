package nodelockfile

import (
	"sort"
	"strings"

	yamlv3 "gopkg.in/yaml.v3"
)

// parsePnpmLockfile parses pnpm-lock.yaml (v6+) into dependency rows. The
// lockfile is YAML; we use a strict YAML decode to avoid hand-rolling another
// format reader. v9+ moves transitive details into a separate snapshots
// section but the per-package `packages:` map still carries the version
// portion of the key and the dependencies block, which is what we need for
// chain evidence.
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

	directScopeByName := make(map[string]string, len(imports))
	for _, dep := range imports {
		if isLocalSpecifier(dep.specifier) || isLocalVersion(dep.version) {
			// Workspace/file/link specifiers must not become remote rows.
			continue
		}
		// Prefer runtime scope over dev when the same package shows up twice.
		if existing := directScopeByName[dep.name]; existing == "runtime" {
			continue
		}
		directScopeByName[dep.name] = dep.scope
	}

	type pkg struct {
		name       string
		version    string
		scope      string
		direct     bool
		lineNumber int
	}

	depAdjacency := make(map[string][]string, len(packages))
	versions := make(map[string]string, len(packages))
	for name, pkgInfo := range packages {
		versions[name] = pkgInfo.version
		depAdjacency[name] = sortedKeys(pkgInfo.deps)
	}
	for name, snapDeps := range snapshots {
		if _, ok := depAdjacency[name]; !ok && len(snapDeps) > 0 {
			depAdjacency[name] = sortedKeys(snapDeps)
		}
	}

	roots := make([]string, 0, len(directScopeByName))
	for name := range directScopeByName {
		if _, ok := packages[name]; ok {
			roots = append(roots, name)
		}
	}
	sort.Strings(roots)

	chains := make(map[string][]string)
	for _, root := range roots {
		walkChain(root, []string{root}, depAdjacency, chains)
	}

	out := make([]pkg, 0, len(packages))
	for name, pkgInfo := range packages {
		scope := "pnpm-package"
		direct := false
		if directScope, ok := directScopeByName[name]; ok {
			scope = directScope
			direct = true
		}
		out = append(out, pkg{
			name:       name,
			version:    pkgInfo.version,
			scope:      scope,
			direct:     direct,
			lineNumber: pkgInfo.lineNumber,
		})
		_ = versions
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].name < out[j].name
	})

	rows := make([]map[string]any, 0, len(out))
	for index, p := range out {
		chain := chains[p.name]
		if len(chain) == 0 {
			chain = []string{p.name}
		}
		row := dependencyRow(p.name, p.version, p.scope, "pnpm", "pnpm", index+1, chain)
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
					dep.version = typed
					dep.specifier = typed
				case map[string]any:
					dep.specifier = stringField(typed, "specifier")
					dep.version = stringField(typed, "version")
				}
				out = append(out, dep)
			}
		}
	}
	return out
}

type pnpmPackage struct {
	version    string
	deps       map[string]string
	lineNumber int
}

// parsePnpmPackages collects every entry from the `packages:` section. The
// keys look like `/name@version`, `/@scope/name@version`, or for v9+
// `name@version`. Suffixes after the version like `(peer@1)` are part of the
// key but not part of the installed version - we strip them.
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
		// Last write wins; pnpm keys are unique anyway.
		out[name] = pnpmPackage{
			version: version,
			deps:    deps,
		}
	}
	return out
}

// parsePnpmSnapshots covers pnpm v9 where transitive dependency edges moved
// into the `snapshots:` section. We just borrow the dependencies map keyed by
// the same name@version key shape.
func parsePnpmSnapshots(document map[string]any) map[string]map[string]string {
	out := make(map[string]map[string]string)
	snapshots, ok := document["snapshots"].(map[string]any)
	if !ok {
		return out
	}
	for key, value := range snapshots {
		name, _ := parsePnpmPackageKey(key)
		if name == "" {
			continue
		}
		entry, _ := value.(map[string]any)
		if deps := readPnpmDependencies(entry); len(deps) > 0 {
			out[name] = deps
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
			version = strings.TrimSpace(version)
			if idx := strings.IndexByte(version, '('); idx > 0 {
				version = version[:idx]
			}
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
