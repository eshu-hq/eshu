// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "strings"

func affectedOSPackageLookupKeys(pkg supplyChainAffectedPackage) []string {
	var keys []string
	if key := strings.TrimSpace(pkg.packageID); key != "" {
		keys = append(keys, key)
	}
	if key := packageIDFromPURL(pkg.purl); key != "" {
		keys = append(keys, key)
	}
	if key := osPackageIdentityFromAffectedPackage(pkg); key != "" {
		keys = append(keys, key)
	}
	return uniqueSortedStrings(keys)
}

func classifyAffectedPackageAdvisorySource(pkg supplyChainAffectedPackage) string {
	if pkg.factID == "" && pkg.packageID == "" && pkg.purl == "" {
		return ""
	}
	source := classifyAdvisorySource(pkg.source, pkg.advisoryID)
	if source != "osv" && source != "" {
		return source
	}
	if strings.ToLower(strings.TrimSpace(pkg.ecosystem)) != "os" {
		return source
	}
	return osPackageVendorFromPURL(pkg.purl)
}

func osPackageIdentityFromAffectedPackage(pkg supplyChainAffectedPackage) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(pkg.packageID)), "os://") {
		return strings.ToLower(strings.TrimSpace(pkg.packageID))
	}
	vendor := osPackageVendorFromPURL(pkg.purl)
	name := strings.ToLower(strings.TrimSpace(pkg.name))
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
	trimmed := strings.ToLower(strings.TrimSpace(purl))
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
	switch strings.ToLower(strings.TrimSpace(packageManager)) {
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
