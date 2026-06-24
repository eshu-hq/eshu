// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker/sbomgenerator"
	"github.com/eshu-hq/eshu/go/internal/packageidentity"
)

type pipfileLockFile struct {
	Default map[string]pipfileLockPackage `json:"default"`
	Develop map[string]pipfileLockPackage `json:"develop"`
}

type pipfileLockPackage struct {
	Version string `json:"version"`
	Git     string `json:"git"`
	Path    string `json:"path"`
	File    string `json:"file"`
}

func parsePipfileLockComponents(relativePath string, body []byte) ([]sbomgenerator.Component, []sbomgenerator.Warning) {
	var lock pipfileLockFile
	if err := json.Unmarshal(body, &lock); err != nil {
		return nil, []sbomgenerator.Warning{malformedLockfileWarning(relativePath, packageidentity.EcosystemPyPI, err)}
	}
	components := make([]sbomgenerator.Component, 0, len(lock.Default)+len(lock.Develop))
	components = append(components, pipfileLockComponents(relativePath, lock.Default, "default", "runtime")...)
	components = append(components, pipfileLockComponents(relativePath, lock.Develop, "develop", "development")...)
	return components, nil
}

func pipfileLockComponents(
	relativePath string,
	packages map[string]pipfileLockPackage,
	scope string,
	dependencyType string,
) []sbomgenerator.Component {
	components := make([]sbomgenerator.Component, 0, len(packages))
	for _, name := range sortedMapKeys(packages) {
		pkg := packages[name]
		if strings.TrimSpace(pkg.Git) != "" || strings.TrimSpace(pkg.Path) != "" || strings.TrimSpace(pkg.File) != "" {
			continue
		}
		version := strings.TrimPrefix(strings.TrimSpace(pkg.Version), "==")
		component, ok := newLockfileComponent(packageidentity.EcosystemPyPI, name, version, relativePath, scope, dependencyType)
		if ok {
			components = append(components, component)
		}
	}
	return components
}

type poetryLockPackage struct {
	name         string
	version      string
	category     string
	sourceType   string
	sourceURL    string
	sourceScoped bool
}

func parsePoetryLockComponents(relativePath string, body []byte) []sbomgenerator.Component {
	packages := poetryLockPackages(string(body))
	components := make([]sbomgenerator.Component, 0, len(packages))
	for _, pkg := range packages {
		if pkg.sourceScoped && !poetryRegistrySource(pkg.sourceType) {
			continue
		}
		scope := firstNonBlank(pkg.category, "main")
		dependencyType := "runtime"
		if isPythonDevelopmentScope(scope) {
			dependencyType = "development"
		}
		component, ok := newLockfileComponent(packageidentity.EcosystemPyPI, pkg.name, pkg.version, relativePath, scope, dependencyType)
		if ok {
			components = append(components, component)
		}
	}
	return components
}

func poetryLockPackages(body string) []poetryLockPackage {
	packages := make([]poetryLockPackage, 0)
	var current *poetryLockPackage
	inSource := false
	for _, rawLine := range strings.Split(body, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		switch line {
		case "[[package]]":
			if current != nil {
				packages = append(packages, *current)
			}
			current = &poetryLockPackage{}
			inSource = false
			continue
		case "[package.source]":
			if current != nil {
				current.sourceScoped = true
			}
			inSource = true
			continue
		}
		if current == nil {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = trimQuoted(value)
		if inSource {
			switch key {
			case "type":
				current.sourceType = strings.ToLower(value)
			case "url":
				current.sourceURL = value
			}
			continue
		}
		switch key {
		case "name":
			current.name = value
		case "version":
			current.version = value
		case "category", "group":
			current.category = value
		}
	}
	if current != nil {
		packages = append(packages, *current)
	}
	return packages
}

func poetryRegistrySource(sourceType string) bool {
	switch strings.ToLower(strings.TrimSpace(sourceType)) {
	case "", "legacy", "default", "primary", "supplemental", "explicit":
		return true
	default:
		return false
	}
}

func isPythonDevelopmentScope(scope string) bool {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "dev", "develop", "test", "tests", "testing", "lint", "ci", "qa":
		return true
	default:
		return false
	}
}

var (
	gemfileLockSpecPattern       = regexp.MustCompile(`^    ([A-Za-z0-9_.-]+) \(([^)]+)\)\s*$`)
	gemfileLockDependencyPattern = regexp.MustCompile(`^      ([A-Za-z0-9_.-]+)(?: \(([^)]+)\))?\s*$`)
	gemfileLockDirectPattern     = regexp.MustCompile(`^  ([A-Za-z0-9_.-]+)!?(?: \(([^)]+)\))?\s*$`)
)

type gemfileLockSpec struct {
	name         string
	version      string
	registryBack bool
}

func parseGemfileLockComponents(relativePath string, body []byte) []sbomgenerator.Component {
	specs, edges, directNames := parseGemfileLock(string(body))
	chains := gemfileLockDependencyChains(edges, directNames)
	names := sortedMapKeys(specs)
	components := make([]sbomgenerator.Component, 0, len(names))
	for _, name := range names {
		spec := specs[name]
		if !spec.registryBack {
			continue
		}
		dependencyType := ""
		if chain := chains[name]; len(chain) == 1 {
			dependencyType = "direct"
		} else if len(chain) > 1 {
			dependencyType = "transitive"
		}
		scope := "GEM"
		if dependencyType == "direct" {
			scope = "DEPENDENCIES"
		}
		component, ok := newLockfileComponent(packageidentity.EcosystemRubyGems, spec.name, spec.version, relativePath, scope, dependencyType)
		if ok {
			components = append(components, component)
		}
	}
	return components
}

func parseGemfileLock(source string) (map[string]gemfileLockSpec, map[string][]string, []string) {
	specs := make(map[string]gemfileLockSpec)
	edges := make(map[string][]string)
	directNames := make([]string, 0)
	section := ""
	inSpecs := false
	currentSpec := ""
	for _, rawLine := range strings.Split(source, "\n") {
		trimmed := strings.TrimSpace(rawLine)
		if trimmed == "" {
			continue
		}
		if isGemfileLockSectionHeader(rawLine, trimmed) {
			section = trimmed
			inSpecs = false
			currentSpec = ""
			continue
		}
		switch section {
		case "GEM":
			if trimmed == "specs:" {
				inSpecs = true
				continue
			}
			if !inSpecs {
				continue
			}
			if matches := gemfileLockSpecPattern.FindStringSubmatch(rawLine); len(matches) == 3 {
				currentSpec = strings.TrimSpace(matches[1])
				specs[currentSpec] = gemfileLockSpec{name: currentSpec, version: strings.TrimSpace(matches[2]), registryBack: true}
				continue
			}
			if matches := gemfileLockDependencyPattern.FindStringSubmatch(rawLine); len(matches) >= 2 && currentSpec != "" {
				edges[currentSpec] = append(edges[currentSpec], strings.TrimSpace(matches[1]))
			}
		case "DEPENDENCIES":
			if matches := gemfileLockDirectPattern.FindStringSubmatch(rawLine); len(matches) >= 2 {
				directNames = append(directNames, strings.TrimSpace(matches[1]))
			}
		}
	}
	return specs, edges, uniqueNonBlankSorted(directNames)
}

func isGemfileLockSectionHeader(rawLine string, trimmed string) bool {
	return strings.TrimRight(rawLine, "\r") == trimmed && strings.ToUpper(trimmed) == trimmed
}

func gemfileLockDependencyChains(edges map[string][]string, directNames []string) map[string][]string {
	chains := make(map[string][]string)
	for _, direct := range directNames {
		walkGemfileLockDependency(chains, edges, direct, []string{direct}, map[string]struct{}{direct: {}})
	}
	return chains
}

func walkGemfileLockDependency(
	chains map[string][]string,
	edges map[string][]string,
	name string,
	path []string,
	seen map[string]struct{},
) {
	if existing := chains[name]; len(existing) == 0 || len(path) < len(existing) || strings.Join(path, "\x00") < strings.Join(existing, "\x00") {
		chains[name] = append([]string(nil), path...)
	}
	for _, child := range uniqueNonBlankSorted(edges[name]) {
		if _, ok := seen[child]; ok {
			continue
		}
		seen[child] = struct{}{}
		next := append(append([]string(nil), path...), child)
		walkGemfileLockDependency(chains, edges, child, next, seen)
		delete(seen, child)
	}
}

func parseGradleLockfileComponents(relativePath string, body []byte) []sbomgenerator.Component {
	components := make([]sbomgenerator.Component, 0)
	for _, rawLine := range strings.Split(string(body), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") || line == "empty=empty" {
			continue
		}
		coordinate, configurations, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		groupID, artifactID, version, ok := gradleLockCoordinate(coordinate)
		if !ok {
			continue
		}
		component, ok := newLockfileComponent(
			packageidentity.EcosystemMaven,
			groupID+":"+artifactID,
			version,
			relativePath,
			strings.TrimSpace(configurations),
			"resolved",
		)
		if ok {
			components = append(components, component)
		}
	}
	return components
}

func gradleLockCoordinate(value string) (string, string, string, bool) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 3 {
		return "", "", "", false
	}
	groupID := strings.TrimSpace(parts[0])
	artifactID := strings.TrimSpace(parts[1])
	version := strings.TrimSpace(parts[2])
	return groupID, artifactID, version, groupID != "" && artifactID != "" && version != ""
}

func uniqueNonBlankSorted(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return sortedStringValues(out)
}

func sortedStringValues(values []string) []string {
	out := append([]string(nil), values...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
