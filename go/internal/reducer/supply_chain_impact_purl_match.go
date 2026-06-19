package reducer

import "strings"

// componentMatchesAffectedPackage reports whether an SBOM component and a
// vulnerability affected_package describe the same package. It bridges the two
// identity schemes the ingester emits: the affected_package carries the
// canonical package_id and OSV's versionless purl, while the SBOM component
// carries a version-qualified purl (and, after the collector fix, the canonical
// package_id). Matching the version-stripped purl keeps version-qualified
// components correlating with versionless advisory purls without widening to
// name-only coincidence.
func componentMatchesAffectedPackage(component supplyChainSBOMComponent, pkg supplyChainAffectedPackage) bool {
	if pkg.purl != "" && component.purl == pkg.purl {
		return true
	}
	if pkg.packageID != "" && component.packageID == pkg.packageID {
		return true
	}
	if pkg.purl != "" && component.purl != "" &&
		packageIDFromPURL(component.purl) == packageIDFromPURL(pkg.purl) {
		return true
	}
	return false
}

func componentMatchesAffectedProduct(component supplyChainSBOMComponent, product supplyChainAffectedProduct) bool {
	if product.criteria == "" || component.cpe == "" {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(product.criteria), strings.TrimSpace(component.cpe))
}

// packageIDFromPURL returns the purl with any version qualifier removed, giving
// a stable version-independent package identity for correlation.
func packageIDFromPURL(purl string) string {
	purl = strings.TrimSpace(purl)
	if before, _, ok := strings.Cut(purl, "@"); ok {
		return before
	}
	return purl
}

func versionFromPURL(purl string) string {
	_, after, ok := strings.Cut(strings.TrimSpace(purl), "@")
	if !ok {
		return ""
	}
	if version, _, ok := strings.Cut(after, "?"); ok {
		return version
	}
	return after
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
