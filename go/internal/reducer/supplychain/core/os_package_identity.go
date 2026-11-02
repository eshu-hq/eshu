// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/packageidentity"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

func affectedOSPackageLookupKeys(pkg supplychainmodel.AffectedPackage) []string {
	var keys []string
	if key := strings.TrimSpace(pkg.PackageID); key != "" {
		keys = append(keys, key)
	}
	if key := purlPackageKey(pkg.PURL); key != "" {
		keys = append(keys, key)
	}
	if key := osPackageIdentityFromAffectedPackage(pkg); key != "" {
		keys = append(keys, key)
	}
	return payloadcore.UniqueSortedStrings(keys)
}

// purlPackageKey returns pkg:<type>/<namespace>/<name> for an advisory purl,
// with qualifiers, subpath, and any version removed. It is the advisory-side
// form of the installed key: an installed os_package row is keyed by
// packageIDFromPURL of its versioned purl (pkg:deb/debian/curl@7.88.1-10?...
// gives pkg:deb/debian/curl), the same string migration 153's
// split_part(purl, '@', 1) index serves. OSV publishes OS advisory purls with
// qualifiers and no version (pkg:deb/debian/curl?arch=source&distro=bookworm),
// which packageIDFromPURL returns unchanged, so they never met the installed
// key (#9848). Only the advisory side uses this: changing packageIDFromPURL
// would move the installed key away from its SQL mirror.
func purlPackageKey(purl string) string {
	purl = strings.TrimSpace(purl)
	if cut := strings.IndexAny(purl, "?#"); cut >= 0 {
		purl = purl[:cut]
	}
	if at := strings.LastIndexByte(purl, '@'); at > strings.LastIndexByte(purl, '/') {
		purl = purl[:at]
	}
	return purl
}

func classifyAffectedPackageAdvisorySource(pkg supplychainmodel.AffectedPackage) string {
	if pkg.FactID == "" && pkg.PackageID == "" && pkg.PURL == "" {
		return ""
	}
	source := classifyAdvisorySource(pkg.Source, pkg.AdvisoryID)
	if source != "osv" && source != "" {
		return source
	}
	if packageidentity.LowerASCII(strings.TrimSpace(pkg.Ecosystem)) != "os" {
		return source
	}
	return osPackageVendorFromPURL(pkg.PURL)
}

func osPackageIdentityFromAffectedPackage(pkg supplychainmodel.AffectedPackage) string {
	if strings.HasPrefix(packageidentity.LowerASCII(strings.TrimSpace(pkg.PackageID)), "os://") {
		return packageidentity.LowerASCII(strings.TrimSpace(pkg.PackageID))
	}
	vendor := osPackageVendorFromPURL(pkg.PURL)
	name := packageidentity.LowerASCII(strings.TrimSpace(pkg.Name))
	if vendor == "" || name == "" {
		return ""
	}
	return "os://" + vendor + "/" + name
}

func osPackageVendorFromPURL(purl string) string {
	purlType, namespace := osPackagePURLTypeAndNamespace(purl)
	switch purlType {
	case "deb":
		if namespace == "debian" || namespace == "ubuntu" {
			return namespace
		}
	case "apk":
		if namespace == "alpine" {
			return namespace
		}
	}
	return ""
}

func osPackageFamilyFromPURL(purl string) string {
	purlType, _ := osPackagePURLTypeAndNamespace(purl)
	switch purlType {
	case "deb":
		return "dpkg"
	case "apk":
		return "apk"
	case "rpm":
		return "rpm"
	default:
		return ""
	}
}

func osPackagePURLTypeAndNamespace(purl string) (string, string) {
	trimmed := packageidentity.LowerASCII(strings.TrimSpace(purl))
	if !strings.HasPrefix(trimmed, "pkg:") {
		return "", ""
	}
	withoutPrefix := strings.TrimPrefix(trimmed, "pkg:")
	withoutQualifiers, _, _ := strings.Cut(withoutPrefix, "?")
	withoutSubpath, _, _ := strings.Cut(withoutQualifiers, "#")
	if lastAt := strings.LastIndex(withoutSubpath, "@"); lastAt > strings.LastIndex(withoutSubpath, "/") {
		withoutSubpath = withoutSubpath[:lastAt]
	}
	purlType, path, ok := strings.Cut(withoutSubpath, "/")
	if !ok {
		return "", ""
	}
	namespace, _, ok := strings.Cut(path, "/")
	if !ok {
		return "", ""
	}
	return purlType, namespace
}

func osPackageFamilyFromPackageManager(packageManager string) string {
	switch packageidentity.LowerASCII(strings.TrimSpace(packageManager)) {
	case "deb", "dpkg":
		return "dpkg"
	case "apk":
		return "apk"
	case "rpm":
		return "rpm"
	default:
		return ""
	}
}

func osPackageVendorMatchesFamily(vendorSource string, packageManager string, distro string) bool {
	switch osPackageFamilyFromPackageManager(packageManager) {
	case "dpkg":
		return vendorSource == "debian" || vendorSource == "ubuntu" || distro == "debian" || distro == "ubuntu"
	case "apk":
		return vendorSource == "alpine" || distro == "alpine"
	case "rpm":
		return vendorSource == "redhat" || vendorSource == "fedora" || vendorSource == "centos" ||
			vendorSource == "rocky" || vendorSource == "alma" || vendorSource == "amazonlinux"
	default:
		return false
	}
}

// supplyChainImpactScopeGenerationPair is one distinct (ScopeID, GenerationID)
// pair collected from loaded os_package envelopes so
// loadSupplyChainImpactScannerAnalysisScopeFacts can load each scan target's
// sibling scanner_worker.analysis fact exactly once.
type supplyChainImpactScopeGenerationPair struct {
	scopeID      string
	generationID string
}

// supplyChainImpactOSPackageScopeGenerationPairs returns the distinct,
// non-empty (ScopeID, GenerationID) pairs carried by every loaded
// vulnerability.os_package envelope, in stable first-seen order. It does not
// cap the result: a scope with many scan targets used to lose every pair past
// 256 and report truncation, so it never retracted (#7154). The number of
// envelopes those pairs can pull in is bounded by the per-intent evidence
// budget instead (supplyChainImpactEvidenceBudget).
func supplyChainImpactOSPackageScopeGenerationPairs(
	envelopes []facts.Envelope,
) []supplyChainImpactScopeGenerationPair {
	seen := make(map[string]struct{})
	var pairs []supplyChainImpactScopeGenerationPair
	for _, envelope := range envelopes {
		if envelope.FactKind != chain.VulnerabilityOSPackageFactKind {
			continue
		}
		scopeID := strings.TrimSpace(envelope.ScopeID)
		if scopeID == "" {
			continue
		}
		generationID := strings.TrimSpace(envelope.GenerationID)
		key := supplychainmodel.ScopeGenerationKey(scopeID, generationID)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		pairs = append(pairs, supplyChainImpactScopeGenerationPair{scopeID: scopeID, generationID: generationID})
	}
	return pairs
}
