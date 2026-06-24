// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker/sbomgenerator"
	"github.com/eshu-hq/eshu/go/internal/packageidentity"
)

const lockfileExactVersionReason = "lockfile_exact_version"

func isSupportedManifestName(name string) bool {
	switch name {
	case "package-lock.json", "npm-shrinkwrap.json", "go.mod", "Cargo.lock", "composer.lock", "packages.lock.json",
		"Pipfile.lock", "poetry.lock", "Gemfile.lock", "gradle.lockfile":
		return true
	default:
		return false
	}
}

func parseRepositoryManifest(relativePath string, name string, body []byte) ([]sbomgenerator.Component, []sbomgenerator.Warning) {
	switch name {
	case "package-lock.json", "npm-shrinkwrap.json":
		return parseNPMLockComponents(relativePath, body)
	case "go.mod":
		return parseGoModComponents(relativePath, body), nil
	case "Cargo.lock":
		return parseCargoLockComponents(relativePath, body), nil
	case "composer.lock":
		return parseComposerLockComponents(relativePath, body)
	case "packages.lock.json":
		return parseNuGetLockComponents(relativePath, body)
	case "Pipfile.lock":
		return parsePipfileLockComponents(relativePath, body)
	case "poetry.lock":
		return parsePoetryLockComponents(relativePath, body), nil
	case "Gemfile.lock":
		return parseGemfileLockComponents(relativePath, body), nil
	case "gradle.lockfile":
		return parseGradleLockfileComponents(relativePath, body), nil
	default:
		return nil, nil
	}
}

func newLockfileComponent(
	ecosystem packageidentity.Ecosystem,
	name string,
	version string,
	lockfilePath string,
	dependencyScope string,
	dependencyType string,
) (sbomgenerator.Component, bool) {
	name = strings.TrimSpace(name)
	version = strings.TrimSpace(version)
	if name == "" || version == "" {
		return sbomgenerator.Component{}, false
	}
	identity, err := packageidentity.Normalize(packageidentity.RawIdentity{
		Ecosystem: ecosystem,
		Registry:  registryForEcosystem(ecosystem),
		RawName:   packageRawName(ecosystem, name),
		Namespace: mavenNamespace(ecosystem, name),
		Version:   version,
	})
	if err != nil {
		return sbomgenerator.Component{}, false
	}
	return sbomgenerator.Component{
		PURL:             identity.PURL,
		Name:             name,
		Version:          version,
		Type:             "library",
		BomRef:           identity.BOMRef,
		Ecosystem:        string(identity.Ecosystem),
		EvidenceSource:   "repository_lockfile",
		LockfilePath:     filepath.ToSlash(strings.TrimSpace(lockfilePath)),
		DependencyScope:  strings.TrimSpace(dependencyScope),
		DependencyType:   strings.TrimSpace(dependencyType),
		ExtractionReason: lockfileExactVersionReason,
	}, true
}

func registryForEcosystem(ecosystem packageidentity.Ecosystem) string {
	switch ecosystem {
	case packageidentity.EcosystemNPM:
		return "registry.npmjs.org"
	case packageidentity.EcosystemGoModule:
		return "proxy.golang.org"
	case packageidentity.EcosystemPyPI:
		return "pypi.org"
	case packageidentity.EcosystemComposer:
		return "repo.packagist.org"
	case packageidentity.EcosystemNuGet:
		return "api.nuget.org/v3/index.json"
	case packageidentity.EcosystemCargo:
		return "crates.io"
	case packageidentity.EcosystemRubyGems:
		return "rubygems.org"
	case packageidentity.EcosystemMaven:
		return "repo.maven.apache.org/maven2"
	default:
		return "scanner-worker.local"
	}
}

func packageRawName(ecosystem packageidentity.Ecosystem, name string) string {
	if ecosystem != packageidentity.EcosystemMaven {
		return name
	}
	_, artifactID, ok := strings.Cut(strings.TrimSpace(name), ":")
	if !ok {
		return name
	}
	return strings.TrimSpace(artifactID)
}

func mavenNamespace(ecosystem packageidentity.Ecosystem, name string) string {
	if ecosystem != packageidentity.EcosystemMaven {
		return ""
	}
	groupID, _, ok := strings.Cut(strings.TrimSpace(name), ":")
	if !ok {
		return ""
	}
	return strings.TrimSpace(groupID)
}

func malformedLockfileWarning(relativePath string, ecosystem packageidentity.Ecosystem, err error) sbomgenerator.Warning {
	return sbomgenerator.Warning{
		Reason:           sbomgenerator.WarningReasonLockfileMalformed,
		Summary:          fmt.Sprintf("%s could not be parsed as %s lockfile evidence: %s", filepath.ToSlash(relativePath), ecosystem, boundedParseError(err)),
		Ecosystem:        string(ecosystem),
		EvidenceSource:   "repository_lockfile",
		LockfilePath:     filepath.ToSlash(relativePath),
		ExtractionReason: "lockfile_malformed",
	}
}

func boundedParseError(err error) string {
	if err == nil {
		return "malformed"
	}
	value := strings.TrimSpace(err.Error())
	if value == "" {
		return "malformed"
	}
	if len(value) > 120 {
		return value[:120]
	}
	return value
}

type npmLockFile struct {
	Packages     map[string]npmLockPackage    `json:"packages"`
	Dependencies map[string]npmLockDependency `json:"dependencies"`
}

type npmLockPackage struct {
	Name         string                       `json:"name"`
	Version      string                       `json:"version"`
	Dev          bool                         `json:"dev"`
	Optional     bool                         `json:"optional"`
	Peer         bool                         `json:"peer"`
	Dependencies map[string]npmLockDependency `json:"dependencies"`
}

type npmLockDependency struct {
	Version      string
	Dev          bool
	Optional     bool
	Peer         bool
	Dependencies map[string]npmLockDependency
}

func (d *npmLockDependency) UnmarshalJSON(body []byte) error {
	var versionRange string
	if err := json.Unmarshal(body, &versionRange); err == nil {
		*d = npmLockDependency{}
		return nil
	}
	var object struct {
		Version      string                       `json:"version"`
		Dev          bool                         `json:"dev"`
		Optional     bool                         `json:"optional"`
		Peer         bool                         `json:"peer"`
		Dependencies map[string]npmLockDependency `json:"dependencies"`
	}
	if err := json.Unmarshal(body, &object); err != nil {
		return err
	}
	*d = npmLockDependency{
		Version:      object.Version,
		Dev:          object.Dev,
		Optional:     object.Optional,
		Peer:         object.Peer,
		Dependencies: object.Dependencies,
	}
	return nil
}

func parseNPMLockComponents(relativePath string, body []byte) ([]sbomgenerator.Component, []sbomgenerator.Warning) {
	var lock npmLockFile
	if err := json.Unmarshal(body, &lock); err != nil {
		return nil, []sbomgenerator.Warning{malformedLockfileWarning(relativePath, packageidentity.EcosystemNPM, err)}
	}
	components := make([]sbomgenerator.Component, 0, len(lock.Packages)+len(lock.Dependencies))
	for _, path := range sortedMapKeys(lock.Packages) {
		if strings.TrimSpace(path) == "" {
			continue
		}
		pkg := lock.Packages[path]
		name := firstNonBlank(pkg.Name, npmNameFromPackagePath(path))
		if component, ok := newLockfileComponent(packageidentity.EcosystemNPM, name, pkg.Version, relativePath, npmDependencyScope(pkg.Dev, pkg.Optional, pkg.Peer), npmDependencyType(path)); ok {
			components = append(components, component)
		}
	}
	appendNPMDependencies(&components, lock.Dependencies, relativePath)
	return components, nil
}

func appendNPMDependencies(components *[]sbomgenerator.Component, deps map[string]npmLockDependency, relativePath string) {
	for _, name := range sortedMapKeys(deps) {
		dep := deps[name]
		if component, ok := newLockfileComponent(packageidentity.EcosystemNPM, name, dep.Version, relativePath, npmDependencyScope(dep.Dev, dep.Optional, dep.Peer), "transitive"); ok {
			*components = append(*components, component)
		}
		appendNPMDependencies(components, dep.Dependencies, relativePath)
	}
}

func npmDependencyScope(dev bool, optional bool, peer bool) string {
	switch {
	case dev:
		return "dev"
	case optional:
		return "optional"
	case peer:
		return "peer"
	default:
		return "runtime"
	}
}

func npmDependencyType(path string) string {
	if strings.Count(filepath.ToSlash(strings.TrimSpace(path)), "node_modules/") == 1 {
		return "direct"
	}
	return "transitive"
}

func npmNameFromPackagePath(path string) string {
	normalized := filepath.ToSlash(strings.TrimSpace(path))
	index := strings.LastIndex(normalized, "node_modules/")
	if index < 0 {
		return ""
	}
	return strings.TrimSpace(normalized[index+len("node_modules/"):])
}

func parseGoModComponents(relativePath string, body []byte) []sbomgenerator.Component {
	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	components := make([]sbomgenerator.Component, 0)
	inRequireBlock := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		stripped := stripGoComment(line)
		if stripped == "" {
			continue
		}
		if inRequireBlock {
			if stripped == ")" {
				inRequireBlock = false
				continue
			}
			if component, ok := goRequireComponent(line, relativePath); ok {
				components = append(components, component)
			}
			continue
		}
		if stripped == "require (" {
			inRequireBlock = true
			continue
		}
		if rest, ok := strings.CutPrefix(line, "require "); ok {
			if component, ok := goRequireComponent(rest, relativePath); ok {
				components = append(components, component)
			}
		}
	}
	return components
}

func goRequireComponent(line string, relativePath string) (sbomgenerator.Component, bool) {
	dependencyType := "direct"
	if strings.Contains(line, "// indirect") {
		dependencyType = "transitive"
	}
	fields := strings.Fields(stripGoComment(line))
	if len(fields) < 2 {
		return sbomgenerator.Component{}, false
	}
	return newLockfileComponent(packageidentity.EcosystemGoModule, fields[0], fields[1], relativePath, "runtime", dependencyType)
}

func stripGoComment(line string) string {
	if before, _, ok := strings.Cut(line, "//"); ok {
		line = before
	}
	return strings.TrimSpace(line)
}

func sortedMapKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
