// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker/sbomgenerator"
	"github.com/eshu-hq/eshu/go/internal/packageidentity"
)

type cargoLockPackage struct {
	name         string
	version      string
	source       string
	dependencies []string
}

func parseCargoLockComponents(relativePath string, body []byte) []sbomgenerator.Component {
	packages := cargoLockPackages(string(body))
	chains := cargoLockDependencyChains(packages)
	components := make([]sbomgenerator.Component, 0, len(packages))
	for _, pkg := range packages {
		if strings.TrimSpace(pkg.source) == "" {
			continue
		}
		dependencyType := ""
		if chain := chains[cargoLockPackageKey(pkg)]; len(chain) > 0 {
			if len(chain) == 1 {
				dependencyType = "direct"
			} else {
				dependencyType = "transitive"
			}
		}
		component, ok := newLockfileComponent(
			packageidentity.EcosystemCargo,
			pkg.name,
			pkg.version,
			relativePath,
			"",
			dependencyType,
		)
		if ok {
			components = append(components, component)
		}
	}
	return components
}

func cargoLockPackages(body string) []cargoLockPackage {
	var packages []cargoLockPackage
	current := cargoLockPackage{}
	inPackage := false
	inDependencies := false
	for _, rawLine := range strings.Split(body, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if line == "[[package]]" {
			if inPackage {
				packages = append(packages, current)
			}
			current = cargoLockPackage{}
			inPackage = true
			inDependencies = false
			continue
		}
		if !inPackage {
			continue
		}
		if inDependencies {
			if line == "]" {
				inDependencies = false
				continue
			}
			if value := cargoLockQuotedListValue(line); value != "" {
				current.dependencies = append(current.dependencies, value)
			}
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		switch key {
		case "name":
			current.name = trimQuoted(value)
		case "version":
			current.version = trimQuoted(value)
		case "source":
			current.source = trimQuoted(value)
		case "dependencies":
			if value == "[" {
				inDependencies = true
				continue
			}
			current.dependencies = append(current.dependencies, cargoInlineDependencies(value)...)
		}
	}
	if inPackage {
		packages = append(packages, current)
	}
	return packages
}

func cargoInlineDependencies(value string) []string {
	value = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(value, "["), "]"))
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if dep := trimQuoted(strings.TrimSpace(part)); dep != "" {
			out = append(out, dep)
		}
	}
	return out
}

func cargoLockQuotedListValue(line string) string {
	line = strings.TrimSuffix(strings.TrimSpace(line), ",")
	return trimQuoted(line)
}

func cargoLockDependencyChains(packages []cargoLockPackage) map[string][]string {
	byName := make(map[string]cargoLockPackage, len(packages))
	for _, pkg := range packages {
		if strings.TrimSpace(pkg.name) == "" {
			continue
		}
		if _, exists := byName[pkg.name]; !exists {
			byName[pkg.name] = pkg
		}
	}
	chains := make(map[string][]string)
	for _, root := range packages {
		if strings.TrimSpace(root.source) != "" {
			continue
		}
		for _, dependency := range root.dependencies {
			child, ok := byName[cargoDependencyName(dependency)]
			if !ok {
				continue
			}
			cargoWalkDependencyChains(child, []string{child.name}, byName, chains, map[string]struct{}{})
		}
	}
	return chains
}

func cargoWalkDependencyChains(
	pkg cargoLockPackage,
	chain []string,
	byName map[string]cargoLockPackage,
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
	for _, dependency := range pkg.dependencies {
		child, ok := byName[cargoDependencyName(dependency)]
		if !ok {
			continue
		}
		childKey := cargoLockPackageKey(child)
		if _, ok := seen[childKey]; ok {
			continue
		}
		next := append(append([]string(nil), chain...), child.name)
		cargoWalkDependencyChains(child, next, byName, chains, seen)
	}
}

func cargoDependencyName(raw string) string {
	fields := strings.Fields(strings.TrimSpace(raw))
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func cargoLockPackageKey(pkg cargoLockPackage) string {
	return strings.TrimSpace(pkg.name) + "\x00" + strings.TrimSpace(pkg.version) + "\x00" + strings.TrimSpace(pkg.source)
}

type composerLockFile struct {
	Packages    []composerLockPackage `json:"packages"`
	PackagesDev []composerLockPackage `json:"packages-dev"`
}

type composerLockPackage struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

func parseComposerLockComponents(relativePath string, body []byte) ([]sbomgenerator.Component, []sbomgenerator.Warning) {
	var lock composerLockFile
	if err := json.Unmarshal(body, &lock); err != nil {
		return nil, []sbomgenerator.Warning{malformedLockfileWarning(relativePath, packageidentity.EcosystemComposer, err)}
	}
	components := make([]sbomgenerator.Component, 0, len(lock.Packages)+len(lock.PackagesDev))
	components = append(components, composerComponents(relativePath, lock.Packages, "packages", "runtime")...)
	components = append(components, composerComponents(relativePath, lock.PackagesDev, "packages-dev", "development")...)
	return components, nil
}

func composerComponents(relativePath string, packages []composerLockPackage, scope string, dependencyType string) []sbomgenerator.Component {
	components := make([]sbomgenerator.Component, 0, len(packages))
	for _, pkg := range packages {
		component, ok := newLockfileComponent(packageidentity.EcosystemComposer, pkg.Name, pkg.Version, relativePath, scope, dependencyType)
		if ok {
			components = append(components, component)
		}
	}
	return components
}

type nugetLockFile struct {
	Dependencies map[string]map[string]nugetLockPackage `json:"dependencies"`
}

type nugetLockPackage struct {
	Type     string `json:"type"`
	Resolved string `json:"resolved"`
}

func parseNuGetLockComponents(relativePath string, body []byte) ([]sbomgenerator.Component, []sbomgenerator.Warning) {
	var lock nugetLockFile
	if err := json.Unmarshal(body, &lock); err != nil {
		return nil, []sbomgenerator.Warning{malformedLockfileWarning(relativePath, packageidentity.EcosystemNuGet, err)}
	}
	components := make([]sbomgenerator.Component, 0)
	for _, target := range sortedMapKeys(lock.Dependencies) {
		for _, name := range sortedMapKeys(lock.Dependencies[target]) {
			pkg := lock.Dependencies[target][name]
			if strings.EqualFold(pkg.Type, "project") {
				continue
			}
			component, ok := newLockfileComponent(packageidentity.EcosystemNuGet, name, pkg.Resolved, relativePath, target, strings.ToLower(strings.TrimSpace(pkg.Type)))
			if ok {
				components = append(components, component)
			}
		}
	}
	return components, nil
}

func trimQuoted(value string) string {
	value = strings.TrimSpace(strings.TrimSuffix(value, ","))
	value = strings.Trim(value, `"`)
	return strings.TrimSpace(value)
}
