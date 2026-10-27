// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// componentMatchesAffectedPackage reports whether an SBOM component and a
// vulnerability affected_package describe the same package. It bridges the two
// identity schemes the ingester emits: the affected_package carries the
// canonical package_id and OSV's versionless purl, while the SBOM component
// carries a version-qualified purl (and, after the collector fix, the canonical
// package_id). Matching the version-stripped purl keeps version-qualified
// components correlating with versionless advisory purls without widening to
// name-only coincidence.
func componentMatchesAffectedPackage(component supplychainmodel.SBOMComponent, pkg supplychainmodel.AffectedPackage) bool {
	if pkg.PURL != "" && component.PURL == pkg.PURL {
		return true
	}
	if pkg.PackageID != "" && component.PackageID == pkg.PackageID {
		return true
	}
	if pkg.PURL != "" && component.PURL != "" &&
		packageIDFromPURL(component.PURL) == packageIDFromPURL(pkg.PURL) {
		return true
	}
	return false
}

func componentMatchesAffectedProduct(component supplychainmodel.SBOMComponent, product supplychainmodel.AffectedProduct) bool {
	if product.Criteria == "" || component.CPE == "" {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(product.Criteria), strings.TrimSpace(component.CPE))
}

// packageIDFromPURL returns the purl with any version qualifier removed, giving
// a stable version-independent package identity for correlation.
func packageIDFromPURL(purl string) string {
	identity, _ := splitPackagePURLVersion(purl)
	return identity
}

func versionFromPURL(purl string) string {
	_, version := splitPackagePURLVersion(purl)
	return version
}

// splitPackagePURLVersion separates the optional version delimiter after the
// package name. A scoped npm name can contain @ before its final slash.
func splitPackagePURLVersion(purl string) (string, string) {
	purl = strings.TrimSpace(purl)
	base := purl
	if cut := strings.IndexAny(base, "?#"); cut >= 0 {
		base = base[:cut]
	}
	at := strings.LastIndex(base, "@")
	if at <= strings.LastIndex(base, "/") {
		return purl, ""
	}
	return base[:at], base[at+1:]
}

func versionFromCPE23Criteria(criteria string) string {
	criteria = strings.TrimSpace(criteria)
	if !strings.HasPrefix(criteria, "cpe:2.3:") {
		return ""
	}
	parts := strings.Split(criteria, ":")
	if len(parts) < 6 {
		return ""
	}
	version := strings.TrimSpace(parts[5])
	if version == "*" || version == "-" {
		return ""
	}
	return version
}
