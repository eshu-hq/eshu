// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packageruntime

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/packageregistry"
	"github.com/eshu-hq/eshu/go/internal/packageidentity"
)

func normalizeDerivedTargetConfig(config DerivedTargetConfig) DerivedTargetConfig {
	if !config.Enabled {
		return DerivedTargetConfig{}
	}
	ecosystems := make([]packageregistry.Ecosystem, 0, len(config.Ecosystems))
	seen := map[packageregistry.Ecosystem]struct{}{}
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
	if !derivedTargetEcosystemEnabled(config, packageregistry.EcosystemNPM) {
		return TargetConfig{}, false
	}
	parsed, err := url.Parse(scopeID)
	if err != nil || parsed.Scheme != string(packageregistry.EcosystemNPM) || parsed.Host != "registry.npmjs.org" {
		return TargetConfig{}, false
	}
	packageName, err := url.PathUnescape(strings.TrimPrefix(parsed.EscapedPath(), "/"))
	if err != nil || strings.TrimSpace(packageName) == "" {
		return TargetConfig{}, false
	}
	identity, err := packageregistry.NormalizePackageIdentity(packageregistry.PackageIdentity{
		Ecosystem: packageregistry.EcosystemNPM,
		Registry:  "https://registry.npmjs.org",
		RawName:   packageName,
	})
	if err != nil || identity.PackageID != scopeID {
		return TargetConfig{}, false
	}
	metadataURL := "https://registry.npmjs.org/" + url.PathEscape(identity.NormalizedName)
	return TargetConfig{
		Base: packageregistry.TargetConfig{
			Provider:     "npm",
			Ecosystem:    packageregistry.EcosystemNPM,
			Registry:     "https://registry.npmjs.org",
			ScopeID:      identity.PackageID,
			Namespace:    identity.Namespace,
			Packages:     []string{identity.NormalizedName},
			PackageLimit: config.PackageLimit,
			VersionLimit: config.VersionLimit,
			Visibility:   packageregistry.VisibilityUnknown,
			SourceURI:    metadataURL,
		},
		MetadataURL: metadataURL,
	}, true
}

func derivedPyPITarget(scopeID string, config DerivedTargetConfig) (TargetConfig, bool) {
	if !derivedTargetEcosystemEnabled(config, packageregistry.EcosystemPyPI) {
		return TargetConfig{}, false
	}
	identity, ok := normalizedDerivedIdentity(scopeID, packageregistry.EcosystemPyPI)
	if !ok || identity.Registry != "pypi.org/pypi" {
		return TargetConfig{}, false
	}
	metadataURL := "https://pypi.org/pypi/" + url.PathEscape(identity.NormalizedName) + "/json"
	return TargetConfig{
		Base: packageregistry.TargetConfig{
			Provider:     "pypi",
			Ecosystem:    packageregistry.EcosystemPyPI,
			Registry:     "https://pypi.org/pypi",
			ScopeID:      identity.PackageID,
			Packages:     []string{identity.NormalizedName},
			PackageLimit: config.PackageLimit,
			VersionLimit: config.VersionLimit,
			Visibility:   packageregistry.VisibilityUnknown,
			SourceURI:    metadataURL,
		},
		MetadataURL: metadataURL,
	}, true
}

func derivedUnsupportedTarget(scopeID string, config DerivedTargetConfig) (TargetConfig, bool) {
	for _, ecosystem := range []packageregistry.Ecosystem{
		packageregistry.EcosystemGoModule,
		packageregistry.EcosystemMaven,
		packageregistry.EcosystemNuGet,
		packageregistry.EcosystemComposer,
		packageregistry.EcosystemRubyGems,
		packageregistry.EcosystemCargo,
	} {
		if !derivedTargetEcosystemEnabled(config, ecosystem) {
			continue
		}
		identity, ok := normalizedDerivedIdentity(scopeID, ecosystem)
		if !ok {
			continue
		}
		return TargetConfig{
			Base: packageregistry.TargetConfig{
				Provider:     string(ecosystem),
				Ecosystem:    ecosystem,
				Registry:     identity.Registry,
				ScopeID:      identity.PackageID,
				Namespace:    identity.Namespace,
				Packages:     []string{identity.NormalizedName},
				PackageLimit: config.PackageLimit,
				VersionLimit: config.VersionLimit,
				Visibility:   packageregistry.VisibilityUnknown,
				SourceURI:    identity.PackageID,
			},
		}, true
	}
	return TargetConfig{}, false
}

func normalizedDerivedIdentity(scopeID string, ecosystem packageregistry.Ecosystem) (packageregistry.NormalizedPackageIdentity, bool) {
	parsed, err := url.Parse(scopeID)
	if err != nil || parsed.Scheme != string(ecosystem) {
		return packageregistry.NormalizedPackageIdentity{}, false
	}
	registry, rawPath, ok := derivedRegistryAndRawPath(scopeID, ecosystem)
	if !ok {
		return packageregistry.NormalizedPackageIdentity{}, false
	}
	rawName, namespace, ok := rawNameAndNamespaceFromDerivedPath(rawPath, ecosystem)
	if !ok {
		return packageregistry.NormalizedPackageIdentity{}, false
	}
	identity, err := packageregistry.NormalizePackageIdentity(packageregistry.PackageIdentity{
		Ecosystem: ecosystem,
		Registry:  registry,
		RawName:   rawName,
		Namespace: namespace,
	})
	if err != nil || identity.PackageID != scopeID {
		return packageregistry.NormalizedPackageIdentity{}, false
	}
	return identity, true
}

func derivedRegistryAndRawPath(scopeID string, ecosystem packageregistry.Ecosystem) (string, string, bool) {
	for _, candidate := range []struct {
		ecosystem packageregistry.Ecosystem
		prefix    string
		registry  string
	}{
		{packageregistry.EcosystemPyPI, "pypi://pypi.org/pypi/", "https://pypi.org/pypi"},
		{packageregistry.EcosystemGoModule, "gomod://proxy.golang.org/", "https://proxy.golang.org"},
		{packageregistry.EcosystemMaven, "maven://repo.maven.apache.org/maven2/", "https://repo.maven.apache.org/maven2"},
		{packageregistry.EcosystemNuGet, "nuget://api.nuget.org/v3/index.json/", "https://api.nuget.org/v3/index.json"},
		{packageregistry.EcosystemComposer, "composer://repo.packagist.org/", "https://repo.packagist.org"},
		{packageregistry.EcosystemRubyGems, "rubygems://rubygems.org/", "https://rubygems.org"},
		{packageregistry.EcosystemCargo, "cargo://crates.io/", "https://crates.io"},
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

func rawNameAndNamespaceFromDerivedPath(pathValue string, ecosystem packageregistry.Ecosystem) (string, string, bool) {
	if strings.TrimSpace(pathValue) == "" {
		return "", "", false
	}
	if ecosystem != packageregistry.EcosystemMaven {
		return pathValue, "", true
	}
	namespace, rawName, ok := strings.Cut(pathValue, ":")
	if !ok {
		return "", "", false
	}
	return strings.TrimSpace(rawName), strings.TrimSpace(namespace), true
}

func derivedTargetEcosystems(raw []packageregistry.Ecosystem) []packageregistry.Ecosystem {
	if len(raw) == 0 {
		return []packageregistry.Ecosystem{packageregistry.EcosystemNPM}
	}
	out := make([]packageregistry.Ecosystem, 0, len(raw))
	for _, ecosystem := range raw {
		out = append(out, packageidentity.NormalizeEcosystem(ecosystem))
	}
	return out
}

func derivedTargetEcosystemEnabled(config DerivedTargetConfig, ecosystem packageregistry.Ecosystem) bool {
	for _, candidate := range config.Ecosystems {
		if candidate == ecosystem {
			return true
		}
	}
	return false
}
