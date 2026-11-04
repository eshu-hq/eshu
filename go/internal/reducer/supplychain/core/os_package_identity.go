// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"net/url"
	"strings"
	"unicode"

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
	if key := canonicalAdvisoryOSPackageKey(pkg); key != "" {
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

// osAdvisoryPURLReleaseLine returns the "<vendor>:<line>" release key an
// advisory purl names through its distro qualifier, or "" when the purl names
// none the reducer can read (#9945). The qualifier value is "<vendor>-<release>"
// (pkg:deb/debian/curl?distro=debian-12, ubuntu-22.04, alpine-3.18) and is read
// through osHostReleaseLine, the same closed vector table that keys the installed
// row, so a fact line and a host line compare as equal strings.
//
// It fails closed to "": no purl, no qualifier, an empty value, a value the
// table cannot map (a codename such as "bookworm", a vendor without a table
// row), or several distro qualifiers that do not agree. Repeated qualifiers that
// name the same line count once. Qualifier keys and the vendor spelling are
// case-insensitive; the version, subpath and qualifiers after the first "#"
// are ignored as in purlPackageKey.
func osAdvisoryPURLReleaseLine(purl string) string {
	purl = strings.TrimSpace(purl)
	if cut := strings.IndexByte(purl, '#'); cut >= 0 {
		purl = purl[:cut]
	}
	_, query, ok := strings.Cut(purl, "?")
	if !ok {
		return ""
	}
	line := ""
	for query != "" {
		var pair string
		pair, query, _ = strings.Cut(query, "&")
		key, value, _ := strings.Cut(pair, "=")
		if !strings.EqualFold(key, "distro") {
			continue
		}
		if strings.IndexByte(value, '%') >= 0 {
			decoded, err := url.QueryUnescape(value)
			if err != nil {
				return ""
			}
			value = decoded
		}
		vendor, release, found := strings.Cut(value, "-")
		if !found {
			return ""
		}
		qualifierLine := osHostReleaseLine(vendor, release)
		if qualifierLine == "" || (line != "" && line != qualifierLine) {
			return ""
		}
		line = qualifierLine
	}
	return line
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
	case "rpm":
		// Upstream distro namespaces vary (almalinux vs alma, rocky-linux
		// vs rocky); the vendor is the canonical name the installed rows
		// and the narrow read key on (#9844).
		switch namespace {
		case "alma", "almalinux":
			return "alma"
		case "rocky", "rockylinux", "rocky-linux":
			return "rocky"
		}
	}
	return ""
}

// canonicalAdvisoryOSPackageKey derives the advisory-side
// pkg:rpm/<vendor>/<name> key from the canonical vendor
// (osPackageVendorFromPURL) and the lowercased package name. OSV publishes
// rpm purls under the upstream distro namespace (almalinux, rocky-linux)
// while installed rows key on the canonical vendor (alma, rocky), so the
// raw purl key never meets the installed key. Both affectedOSPackageLookupKeys
// and the osPackageMatchesAffectedPackage re-verification use this one
// helper so the narrow read and the join agree. Advisory-side only: the
// installed key, the purl splitter, the scanner contract, and the OSV purls
// stay untouched.
func canonicalAdvisoryOSPackageKey(pkg supplychainmodel.AffectedPackage) string {
	purlType, _ := osPackagePURLTypeAndNamespace(pkg.PURL)
	if purlType != "rpm" {
		return ""
	}
	vendor := osPackageVendorFromPURL(pkg.PURL)
	name := packageidentity.LowerASCII(strings.TrimSpace(pkg.Name))
	if vendor == "" || name == "" {
		return ""
	}
	return "pkg:rpm/" + vendor + "/" + name
}

// InstalledOSPackageSourceKey returns the advisory-side key form of an
// installed row's source package name (#9859): the installed purl's
// version-stripped prefix with its last segment (the binary name) replaced
// by the source name. An installed libssl3 row with source "openssl" keys
// as pkg:deb/debian/openssl, the same string purlPackageKey derives from
// the advisory purl pkg:deb/debian/openssl?arch=source, so the two meet in
// the index without any per-row fallback scan.
//
// The comparison is verbatim on both sides: no case folding, because the
// SQL mirror (osPackageSourcePURLPrefixExpression) must derive the identical
// string and LOWER() is Unicode-lower while Go would fold ASCII-only. The
// rejection set is exactly what the SQL CASE mirrors: empty, any '/', '@',
// '?', '#', or any unicode space, and a prefix with no '/' to replace into.
// A rejected source keys as "" and the row is indexed on its binary key
// alone, exactly as rows written before source_package existed.
//
// Exported because it is a join contract: the reducer index and matcher read
// it, and the Postgres live differential harness
// (TestListOSPackageAdvisoryFactEnvelopesPURLFormsMatchTheGoMatcherLive)
// oracles it directly so the SQL mirror can never drift from this function
// without failing loudly.
func InstalledOSPackageSourceKey(purl, source string) string {
	source = strings.TrimSpace(source)
	if source == "" || strings.ContainsAny(source, "/@?#") {
		return ""
	}
	for _, r := range source {
		if unicode.IsSpace(r) {
			return ""
		}
	}
	prefix := packageIDFromPURL(purl)
	slash := strings.LastIndexByte(prefix, '/')
	if slash < 0 {
		return ""
	}
	return prefix[:slash+1] + source
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
