// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rust

import (
	"sort"
	"strings"
)

type cargoLockDocument struct {
	Packages []cargoLockPackage `toml:"package"`
}

type cargoLockPackage struct {
	Name         string   `toml:"name"`
	Version      string   `toml:"version"`
	Source       string   `toml:"source"`
	Dependencies []string `toml:"dependencies"`
}

func cargoLockDependencyRows(repoRoot string, path string, lock cargoLockDocument) []map[string]any {
	chains := cargoLockDependencyChains(lock.Packages)
	packages := append([]cargoLockPackage(nil), lock.Packages...)
	sort.SliceStable(packages, func(i, j int) bool {
		if packages[i].Name != packages[j].Name {
			return packages[i].Name < packages[j].Name
		}
		if packages[i].Version != packages[j].Version {
			return packages[i].Version < packages[j].Version
		}
		return packages[i].Source < packages[j].Source
	})

	sourcePath := cargoSourcePath(repoRoot, path)
	rows := make([]map[string]any, 0, len(packages))
	for _, pkg := range packages {
		if strings.TrimSpace(pkg.Source) == "" {
			continue
		}
		key := cargoLockPackageKey(pkg)
		row := cargoDependencyRow(pkg.Name, pkg.Version, "cargo-lock", "runtime", sourcePath, len(rows)+1)
		row["lockfile"] = true
		if source := strings.TrimSpace(pkg.Source); source != "" {
			row["package_source"] = source
		}
		if chain := chains[key]; len(chain) > 0 {
			row["dependency_path"] = chain
			row["dependency_depth"] = len(chain)
			row["direct_dependency"] = len(chain) == 1
		}
		rows = append(rows, row)
	}
	return rows
}

func cargoLockDependencyChains(packages []cargoLockPackage) map[string][]string {
	byName := cargoLockPackagesByName(packages)
	chains := map[string][]string{}
	for _, root := range cargoLockRootPackages(packages) {
		for _, dependency := range root.Dependencies {
			child, ok := cargoLockResolveDependency(dependency, byName)
			if !ok {
				continue
			}
			cargoLockWalkDependencyChains(child, []string{child.Name}, byName, chains, map[string]struct{}{})
		}
	}
	return chains
}

func cargoLockWalkDependencyChains(
	pkg cargoLockPackage,
	chain []string,
	byName map[string][]cargoLockPackage,
	chains map[string][]string,
	seen map[string]struct{},
) {
	key := cargoLockPackageKey(pkg)
	if existing, ok := chains[key]; ok && len(existing) <= len(chain) {
		return
	}
	chains[key] = append([]string(nil), chain...)
	seen[key] = struct{}{}
	defer delete(seen, key)
	for _, dependency := range pkg.Dependencies {
		child, ok := cargoLockResolveDependency(dependency, byName)
		if !ok {
			continue
		}
		childKey := cargoLockPackageKey(child)
		if _, ok := seen[childKey]; ok {
			continue
		}
		next := append(append([]string(nil), chain...), child.Name)
		cargoLockWalkDependencyChains(child, next, byName, chains, seen)
	}
}

func cargoLockRootPackages(packages []cargoLockPackage) []cargoLockPackage {
	var roots []cargoLockPackage
	for _, pkg := range packages {
		if strings.TrimSpace(pkg.Source) == "" {
			roots = append(roots, pkg)
		}
	}
	sort.SliceStable(roots, func(i, j int) bool {
		if roots[i].Name != roots[j].Name {
			return roots[i].Name < roots[j].Name
		}
		return roots[i].Version < roots[j].Version
	})
	return roots
}

func cargoLockPackagesByName(packages []cargoLockPackage) map[string][]cargoLockPackage {
	out := make(map[string][]cargoLockPackage)
	for _, pkg := range packages {
		name := strings.TrimSpace(pkg.Name)
		if name == "" {
			continue
		}
		out[name] = append(out[name], pkg)
	}
	for name := range out {
		sort.SliceStable(out[name], func(i, j int) bool {
			if out[name][i].Version != out[name][j].Version {
				return out[name][i].Version < out[name][j].Version
			}
			return out[name][i].Source < out[name][j].Source
		})
	}
	return out
}

func cargoLockResolveDependency(raw string, byName map[string][]cargoLockPackage) (cargoLockPackage, bool) {
	name, version, source := cargoLockDependencyIdentity(raw)
	candidates := byName[name]
	if len(candidates) == 0 {
		return cargoLockPackage{}, false
	}
	if version != "" {
		var matches []cargoLockPackage
		for _, candidate := range candidates {
			if candidate.Version == version && (source == "" || strings.TrimSpace(candidate.Source) == source) {
				matches = append(matches, candidate)
			}
		}
		if len(matches) == 1 {
			return matches[0], true
		}
		return cargoLockPackage{}, false
	}
	if len(candidates) == 1 {
		return candidates[0], true
	}
	return cargoLockPackage{}, false
}

func cargoLockDependencyIdentity(raw string) (string, string, string) {
	fields := strings.Fields(strings.TrimSpace(raw))
	if len(fields) == 0 {
		return "", "", ""
	}
	if len(fields) == 1 {
		return fields[0], "", ""
	}
	return fields[0], fields[1], cargoLockDependencySource(fields[2:])
}

func cargoLockDependencySource(fields []string) string {
	raw := strings.TrimSpace(strings.Join(fields, " "))
	if !strings.HasPrefix(raw, "(") || !strings.HasSuffix(raw, ")") {
		return ""
	}
	raw = strings.TrimPrefix(raw, "(")
	raw = strings.TrimSuffix(raw, ")")
	return strings.TrimSpace(raw)
}

func cargoLockPackageKey(pkg cargoLockPackage) string {
	return strings.TrimSpace(pkg.Name) + "\x00" + strings.TrimSpace(pkg.Version) + "\x00" + strings.TrimSpace(pkg.Source)
}
