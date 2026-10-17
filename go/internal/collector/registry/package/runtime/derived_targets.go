// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/registry/package"
	"github.com/eshu-hq/eshu/go/internal/packageidentity"
)

func normalizeDerivedTargetConfig(config DerivedTargetConfig) DerivedTargetConfig {
	if !config.Enabled {
		return DerivedTargetConfig{}
	}
	ecosystems := make([]packages.Ecosystem, 0, len(config.Ecosystems))
	seen := map[packages.Ecosystem]struct{}{}
	source := derivedTargetEcosystems(config.Ecosystems)
	for _, ecosystem := range source {
		if ecosystem == "" {
			continue
		}
		if _, ok := seen[ecosystem]; ok {
			continue
		}
		seen[ecosystem] = struct{}{}
		ecosystems = append(ecosystems, ecosystem)
	}
	packageLimit := config.PackageLimit
	if packageLimit <= 0 {
		packageLimit = 1
	}
	versionLimit := config.VersionLimit
	if versionLimit <= 0 {
		versionLimit = 1
	}
	return DerivedTargetConfig{
		Enabled:      len(ecosystems) > 0,
		Ecosystems:   ecosystems,
		PackageLimit: packageLimit,
		VersionLimit: versionLimit,
	}
}

func (s *ClaimedSource) derivedTargetForScope(scopeID string) (TargetConfig, error) {
	if !s.derivedTargets.Enabled {
		return TargetConfig{}, fmt.Errorf("package registry target scope_id %q is not configured", scopeID)
	}
	target, ok := derivedTarget(scopeID, s.derivedTargets)
	if !ok {
		return TargetConfig{}, fmt.Errorf("package registry target scope_id %q is not configured", scopeID)
	}
	return target, nil
}

func derivedTarget(scopeID string, config DerivedTargetConfig) (TargetConfig, bool) {
	if target, ok := derivedNPMTarget(scopeID, config); ok {
		return target, true
	}
	if target, ok := derivedPyPITarget(scopeID, config); ok {
		return target, true
	}
	return derivedUnsupportedTarget(scopeID, config)
}

func derivedNPMTarget(scopeID string, config DerivedTargetConfig) (TargetConfig, bool) {
	if !derivedTargetEcosystemEnabled(config, packages.EcosystemNPM) {
		return TargetConfig{}, false
	}
	parsed, err := url.Parse(scopeID)
	if err != nil || parsed.Scheme != string(packages.EcosystemNPM) || parsed.Host != "registry.npmjs.org" {
		return TargetConfig{}, false
	}
	packageName, err := url.PathUnescape(strings.TrimPrefix(parsed.EscapedPath(), "/"))
	if err != nil || strings.TrimSpace(packageName) == "" {
		return TargetConfig{}, false
	}
	identity, err := packages.NormalizePackageIdentity(packages.PackageIdentity{
		Ecosystem: packages.EcosystemNPM,
		Registry:  "https://registry.npmjs.org",
		RawName:   packageName,
	})
	if err != nil || identity.PackageID != scopeID {
		return TargetConfig{}, false
	}
	metadataURL := "https://registry.npmjs.org/" + url.PathEscape(identity.NormalizedName)
	return TargetConfig{
		Base: packages.TargetConfig{
			Provider:     "npm",
			Ecosystem:    packages.EcosystemNPM,
			Registry:     "https://registry.npmjs.org",
			ScopeID:      identity.PackageID,
			Namespace:    identity.Namespace,
			Packages:     []string{identity.NormalizedName},
			PackageLimit: config.PackageLimit,
			VersionLimit: config.VersionLimit,
			Visibility:   packages.VisibilityUnknown,
			SourceURI:    metadataURL,
		},
		MetadataURL: metadataURL,
	}, true
}

func derivedPyPITarget(scopeID string, config DerivedTargetConfig) (TargetConfig, bool) {
	if !derivedTargetEcosystemEnabled(config, packages.EcosystemPyPI) {
		return TargetConfig{}, false
	}
	identity, ok := normalizedDerivedIdentity(scopeID, packages.EcosystemPyPI)
	if !ok || identity.Registry != "pypi.org/pypi" {
		return TargetConfig{}, false
	}
	metadataURL := "https://pypi.org/pypi/" + url.PathEscape(identity.NormalizedName) + "/json"
	return TargetConfig{
		Base: packages.TargetConfig{
			Provider:     "pypi",
			Ecosystem:    packages.EcosystemPyPI,
			Registry:     "https://pypi.org/pypi",
			ScopeID:      identity.PackageID,
			Packages:     []string{identity.NormalizedName},
			PackageLimit: config.PackageLimit,
			VersionLimit: config.VersionLimit,
			Visibility:   packages.VisibilityUnknown,
			SourceURI:    metadataURL,
		},
		MetadataURL: metadataURL,
	}, true
}

func derivedUnsupportedTarget(scopeID string, config DerivedTargetConfig) (TargetConfig, bool) {
	for _, ecosystem := range []packages.Ecosystem{
		packages.EcosystemGoModule,
		packages.EcosystemMaven,
		packages.EcosystemNuGet,
		packages.EcosystemComposer,
		packages.EcosystemRubyGems,
		packages.EcosystemCargo,
	} {
		if !derivedTargetEcosystemEnabled(config, ecosystem) {
			continue
		}
		identity, ok := normalizedDerivedIdentity(scopeID, ecosystem)
		if !ok {
			continue
		}
		return TargetConfig{
			Base: packages.TargetConfig{
				Provider:     string(ecosystem),
				Ecosystem:    ecosystem,
				Registry:     identity.Registry,
				ScopeID:      identity.PackageID,
				Namespace:    identity.Namespace,
				Packages:     []string{identity.NormalizedName},
				PackageLimit: config.PackageLimit,
				VersionLimit: config.VersionLimit,
				Visibility:   packages.VisibilityUnknown,
				SourceURI:    identity.PackageID,
			},
		}, true
	}
	return TargetConfig{}, false
}

func normalizedDerivedIdentity(scopeID string, ecosystem packages.Ecosystem) (packages.NormalizedPackageIdentity, bool) {
	parsed, err := url.Parse(scopeID)
	if err != nil || parsed.Scheme != string(ecosystem) {
		return packages.NormalizedPackageIdentity{}, false
	}
	registry, rawPath, ok := derivedRegistryAndRawPath(scopeID, ecosystem)
	if !ok {
		return packages.NormalizedPackageIdentity{}, false
	}
	rawName, namespace, ok := rawNameAndNamespaceFromDerivedPath(rawPath, ecosystem)
	if !ok {
		return packages.NormalizedPackageIdentity{}, false
	}
	identity, err := packages.NormalizePackageIdentity(packages.PackageIdentity{
		Ecosystem: ecosystem,
		Registry:  registry,
		RawName:   rawName,
		Namespace: namespace,
	})
	if err != nil || identity.PackageID != scopeID {
		return packages.NormalizedPackageIdentity{}, false
	}
	return identity, true
}

func derivedRegistryAndRawPath(scopeID string, ecosystem packages.Ecosystem) (string, string, bool) {
	for _, candidate := range []struct {
		ecosystem packages.Ecosystem
		prefix    string
		registry  string
	}{
		{packages.EcosystemPyPI, "pypi://pypi.org/pypi/", "https://pypi.org/pypi"},
		{packages.EcosystemGoModule, "gomod://proxy.golang.org/", "https://proxy.golang.org"},
		{packages.EcosystemMaven, "maven://repo.maven.apache.org/maven2/", "https://repo.maven.apache.org/maven2"},
		{packages.EcosystemNuGet, "nuget://api.nuget.org/v3/index.json/", "https://api.nuget.org/v3/index.json"},
		{packages.EcosystemComposer, "composer://repo.packagist.org/", "https://repo.packagist.org"},
		{packages.EcosystemRubyGems, "rubygems://rubygems.org/", "https://rubygems.org"},
		{packages.EcosystemCargo, "cargo://crates.io/", "https://crates.io"},
	} {
		if candidate.ecosystem != ecosystem || !strings.HasPrefix(scopeID, candidate.prefix) {
			continue
		}
		rawPath, err := url.PathUnescape(strings.TrimPrefix(scopeID, candidate.prefix))
		if err != nil || strings.TrimSpace(rawPath) == "" {
			return "", "", false
		}
		return candidate.registry, rawPath, true
	}
	return "", "", false
}

func rawNameAndNamespaceFromDerivedPath(pathValue string, ecosystem packages.Ecosystem) (string, string, bool) {
	if strings.TrimSpace(pathValue) == "" {
		return "", "", false
	}
	if ecosystem != packages.EcosystemMaven {
		return pathValue, "", true
	}
	namespace, rawName, ok := strings.Cut(pathValue, ":")
	if !ok {
		return "", "", false
	}
	return strings.TrimSpace(rawName), strings.TrimSpace(namespace), true
}

func derivedTargetEcosystems(raw []packages.Ecosystem) []packages.Ecosystem {
	if len(raw) == 0 {
		return []packages.Ecosystem{packages.EcosystemNPM}
	}
	out := make([]packages.Ecosystem, 0, len(raw))
	for _, ecosystem := range raw {
		out = append(out, packageidentity.NormalizeEcosystem(ecosystem))
	}
	return out
}

func derivedTargetEcosystemEnabled(config DerivedTargetConfig, ecosystem packages.Ecosystem) bool {
	for _, candidate := range config.Ecosystems {
		if candidate == ecosystem {
			return true
		}
	}
	return false
}
