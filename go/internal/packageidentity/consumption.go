// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packageidentity

import (
	"sort"
	"strings"
)

// ConsumptionKey is one normalized manifest dependency name under its package
// ecosystem. It is safe to persist in Postgres text columns because its fields
// do not use the reducer's legacy NUL-delimited representation.
type ConsumptionKey struct {
	Ecosystem   Ecosystem
	PackageName string
}

// ConsumptionKeys returns the normalized and source-lowercase forms that a
// manifest dependency can use to match package identity. Unknown ecosystems
// return no keys so callers fail closed rather than guessing a normalization.
func ConsumptionKeys(ecosystem string, packageNames ...string) []ConsumptionKey {
	normalizedEcosystem := NormalizeEcosystem(Ecosystem(ecosystem))
	if normalizedEcosystem == "" {
		return nil
	}

	keys := make([]ConsumptionKey, 0, len(packageNames)*2)
	for _, packageName := range packageNames {
		for _, candidate := range ConsumptionNameCandidates(normalizedEcosystem, packageName) {
			keys = append(keys, ConsumptionKey{
				Ecosystem:   normalizedEcosystem,
				PackageName: candidate,
			})
		}
	}
	return uniqueSortedConsumptionKeys(keys)
}

// ConsumptionNameCandidates returns the normalized plus lower-cased name forms
// a manifest dependency can use to match one package ecosystem.
func ConsumptionNameCandidates(ecosystem Ecosystem, packageName string) []string {
	packageName = strings.TrimSpace(packageName)
	if packageName == "" {
		return nil
	}

	candidates := []string{strings.ToLower(packageName)}
	if normalizedName, ok := normalizedConsumptionName(ecosystem, packageName); ok {
		candidates = append(candidates, normalizedName)
	}
	return uniqueSortedStrings(candidates)
}

// ConsumptionKeysFromPURL returns manifest-consumption keys for a Package URL.
// It returns no keys for an invalid or unsupported PURL.
func ConsumptionKeysFromPURL(purl string) []ConsumptionKey {
	raw, ok := parsePURLToRawIdentity(purl)
	if !ok {
		return nil
	}
	names := []string{raw.RawName}
	if raw.Namespace != "" {
		names = append(names, strings.TrimRight(raw.Namespace, "/")+"/"+strings.TrimLeft(raw.RawName, "/"))
	}
	return ConsumptionKeys(string(raw.Ecosystem), names...)
}

// ConsumptionKeysForRegistryIdentity returns the source and normalized forms a
// package-registry identity can use to match manifest dependencies.
func ConsumptionKeysForRegistryIdentity(ecosystem, rawName, normalizedName, namespace string) []ConsumptionKey {
	normalizedEcosystem := NormalizeEcosystem(Ecosystem(ecosystem))
	if normalizedEcosystem == "" {
		return nil
	}
	names := []string{rawName, normalizedName}
	namespace = strings.TrimSpace(namespace)
	normalizedName = strings.TrimSpace(normalizedName)
	if namespace != "" && normalizedName != "" {
		names = append(names, strings.TrimRight(namespace, "/")+"/"+strings.TrimLeft(normalizedName, "/"))
		if normalizedEcosystem == EcosystemMaven {
			names = append(names, strings.TrimRight(namespace, ":")+":"+strings.TrimLeft(normalizedName, ":"))
		}
	}
	return ConsumptionKeys(string(normalizedEcosystem), names...)
}

// ConsumptionKeysFromPackageID returns keys for an Eshu package ID only when
// it uses the canonical default registry. Custom registries require their
// active package-registry metadata and deliberately return no keys here.
func ConsumptionKeysFromPackageID(packageID string) []ConsumptionKey {
	trimmed := strings.TrimSpace(packageID)
	for _, ecosystem := range []Ecosystem{EcosystemNPM, EcosystemPyPI, EcosystemGoModule, EcosystemMaven, EcosystemNuGet, EcosystemComposer, EcosystemRubyGems, EcosystemCargo, EcosystemSwift, EcosystemHex, EcosystemPub, EcosystemOS} {
		prefix := string(ecosystem) + "://" + DefaultRegistry(ecosystem) + "/"
		if !strings.HasPrefix(trimmed, prefix) {
			continue
		}
		name := strings.TrimPrefix(trimmed, prefix)
		if name == "" {
			return nil
		}
		return ConsumptionKeys(string(ecosystem), name)
	}
	return nil
}

func normalizedConsumptionName(ecosystem Ecosystem, packageName string) (string, bool) {
	rawName, namespace := consumptionRawNameAndNamespace(ecosystem, packageName)
	identity, err := Normalize(RawIdentity{
		Ecosystem:      ecosystem,
		Registry:       "manifest.local",
		RawName:        rawName,
		Namespace:      namespace,
		PackageManager: string(ecosystem),
	})
	if err != nil {
		return "", false
	}
	if namespace != "" {
		return strings.TrimRight(namespace, "/") + "/" + strings.TrimLeft(identity.NormalizedName, "/"), true
	}
	return identity.NormalizedName, true
}

func consumptionRawNameAndNamespace(ecosystem Ecosystem, packageName string) (string, string) {
	packageName = strings.TrimSpace(packageName)
	if ecosystem != EcosystemMaven && ecosystem != EcosystemHex {
		return packageName, ""
	}
	namespace, name, ok := strings.Cut(packageName, ":")
	if !ok {
		namespace, name, ok = strings.Cut(packageName, "/")
	}
	if !ok {
		return packageName, ""
	}
	return strings.TrimSpace(name), strings.TrimSpace(namespace)
}

func uniqueSortedConsumptionKeys(keys []ConsumptionKey) []ConsumptionKey {
	if len(keys) == 0 {
		return nil
	}
	seen := make(map[ConsumptionKey]struct{}, len(keys))
	for _, key := range keys {
		if key.Ecosystem == "" || key.PackageName == "" {
			continue
		}
		seen[key] = struct{}{}
	}
	if len(seen) == 0 {
		return nil
	}
	result := make([]ConsumptionKey, 0, len(seen))
	for key := range seen {
		result = append(result, key)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Ecosystem != result[j].Ecosystem {
			return result[i].Ecosystem < result[j].Ecosystem
		}
		return result[i].PackageName < result[j].PackageName
	})
	return result
}

func uniqueSortedStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			seen[value] = struct{}{}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	result := make([]string, 0, len(seen))
	for value := range seen {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
