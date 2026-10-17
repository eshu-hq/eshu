// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packages

import (
	"net/url"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/registry/package"
	"github.com/eshu-hq/eshu/go/internal/coordinator/schedule"
	"github.com/eshu-hq/eshu/go/internal/packageidentity"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// DerivationEcosystems normalizes the configured ecosystem filter for
// package-registry target derivation into a set, dropping any value that is
// not a supported owned-dependency ecosystem. An empty filter defaults to npm
// rather than to every ecosystem, so an unconfigured instance derives a
// bounded set.
func DerivationEcosystems(values []string) map[string]struct{} {
	out := map[string]struct{}{}
	source := values
	if len(source) == 0 {
		source = []string{"npm"}
	}
	for _, value := range source {
		ecosystem := packageRegistryOwnedDependencyEcosystem(value)
		if ecosystem == "" {
			continue
		}
		out[ecosystem] = struct{}{}
	}
	return out
}

func packageRegistryTargetForOwnedPackage(
	target workflow.OwnedPackageDependencyTarget,
	packageLimit int,
	versionLimit int,
) (packageRegistryTargetConfiguration, bool) {
	spec, ok := packageRegistryEcosystemSpec(target.Ecosystem)
	if !ok {
		return packageRegistryTargetConfiguration{}, false
	}
	rawName, namespace := packageRegistryRawNameAndNamespace(spec.ecosystem, target.PackageName)
	identity, err := packages.NormalizePackageIdentity(packages.PackageIdentity{
		Ecosystem: spec.ecosystem,
		Registry:  spec.registry,
		RawName:   rawName,
		Namespace: namespace,
	})
	if err != nil {
		return packageRegistryTargetConfiguration{}, false
	}
	metadataURL := ""
	if spec.metadataURL != nil {
		metadataURL = spec.metadataURL(identity)
	}
	return packageRegistryTargetConfiguration{
		Provider:     spec.provider,
		Ecosystem:    string(spec.ecosystem),
		Registry:     spec.registry,
		ScopeID:      identity.PackageID,
		Namespace:    identity.Namespace,
		Packages:     []string{identity.NormalizedName},
		PackageLimit: packageLimit,
		VersionLimit: versionLimit,
		Visibility:   string(packages.VisibilityUnknown),
		SourceURI:    schedule.FirstNonBlank(metadataURL, identity.PackageID),
		MetadataURL:  metadataURL,
		Derived:      true,
		PackageName:  identity.NormalizedName,
		TargetClass:  targetClassOwnedPackage,
	}, true
}

func npmPackageRegistryTarget(
	target workflow.OwnedPackageDependencyTarget,
	packageLimit int,
	versionLimit int,
) (packageRegistryTargetConfiguration, bool) {
	target.Ecosystem = string(packages.EcosystemNPM)
	return packageRegistryTargetForOwnedPackage(target, packageLimit, versionLimit)
}

type packageRegistryEcosystemTargetSpec struct {
	ecosystem   packages.Ecosystem
	provider    string
	registry    string
	metadataURL func(packages.NormalizedPackageIdentity) string
}

func packageRegistryEcosystemSpec(raw string) (packageRegistryEcosystemTargetSpec, bool) {
	ecosystem := packageidentity.NormalizeEcosystem(packageidentity.Ecosystem(raw))
	switch ecosystem {
	case packages.EcosystemNPM:
		return packageRegistryEcosystemTargetSpec{
			ecosystem: packages.EcosystemNPM,
			provider:  "npm",
			registry:  "https://registry.npmjs.org",
			metadataURL: func(identity packages.NormalizedPackageIdentity) string {
				return "https://registry.npmjs.org/" + url.PathEscape(identity.NormalizedName)
			},
		}, true
	case packages.EcosystemPyPI:
		return packageRegistryEcosystemTargetSpec{
			ecosystem: packages.EcosystemPyPI,
			provider:  "pypi",
			registry:  "https://pypi.org/pypi",
			metadataURL: func(identity packages.NormalizedPackageIdentity) string {
				return "https://pypi.org/pypi/" + url.PathEscape(identity.NormalizedName) + "/json"
			},
		}, true
	case packages.EcosystemGoModule:
		return packageRegistryEcosystemTargetSpec{ecosystem: ecosystem, provider: "go", registry: "https://proxy.golang.org"}, true
	case packages.EcosystemMaven:
		return packageRegistryEcosystemTargetSpec{ecosystem: ecosystem, provider: "maven", registry: "https://repo.maven.apache.org/maven2"}, true
	case packages.EcosystemNuGet:
		return packageRegistryEcosystemTargetSpec{ecosystem: ecosystem, provider: "nuget", registry: "https://api.nuget.org/v3/index.json"}, true
	case packages.EcosystemComposer:
		return packageRegistryEcosystemTargetSpec{ecosystem: ecosystem, provider: "packagist", registry: "https://repo.packagist.org"}, true
	case packages.EcosystemRubyGems:
		return packageRegistryEcosystemTargetSpec{ecosystem: ecosystem, provider: "rubygems", registry: "https://rubygems.org"}, true
	case packages.EcosystemCargo:
		return packageRegistryEcosystemTargetSpec{ecosystem: ecosystem, provider: "crates.io", registry: "https://crates.io"}, true
	default:
		return packageRegistryEcosystemTargetSpec{}, false
	}
}

func packageRegistryOwnedDependencyEcosystem(raw string) string {
	ecosystem := packageidentity.NormalizeEcosystem(packageidentity.Ecosystem(raw))
	if ecosystem == "" {
		return ""
	}
	if ecosystem == packageidentity.EcosystemGoModule {
		return "go"
	}
	return string(ecosystem)
}

func packageRegistryRawNameAndNamespace(ecosystem packages.Ecosystem, packageName string) (string, string) {
	packageName = strings.TrimSpace(packageName)
	if ecosystem != packages.EcosystemMaven {
		return packageName, ""
	}
	namespace, name, ok := strings.Cut(packageName, ":")
	if !ok {
		return packageName, ""
	}
	return strings.TrimSpace(name), strings.TrimSpace(namespace)
}
