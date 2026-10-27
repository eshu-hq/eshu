// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// sbomVersionEvidenceConflicts checks independent versions before an SBOM
// attachment can contribute to a precise package-advisory decision.
func sbomVersionEvidenceConflicts(
	ecosystem string,
	component supplychainmodel.SBOMComponent,
	consumption supplychainmodel.PackageConsumption,
) bool {
	if !equivalentSupplyChainVersions(ecosystem, component.PayloadVersion, component.PURLVersion) {
		return true
	}
	consumed := strings.TrimSpace(consumption.ObservedVersion)
	if consumed == "" {
		if exact, ok := exactConsumptionDependencyVersion(ecosystem, consumption); ok {
			consumed = exact
		}
	}
	if consumed == "" || component.Version == "" {
		return false
	}
	return !equivalentSupplyChainVersions(ecosystem, consumed, component.Version)
}

func equivalentSupplyChainVersions(ecosystem, left, right string) bool {
	left, right = strings.TrimSpace(left), strings.TrimSpace(right)
	if left == "" || right == "" || left == right {
		return true
	}
	var compare func(string, string) (int, bool)
	switch normalizedSupplyChainVersionEcosystem(ecosystem) {
	case "npm", "gomod", "cargo", "hex", "pub", "swift", "composer":
		compare = compareOSVSemver
	case "nuget":
		compare = compareNuGetVersion
	case "pypi":
		compare = comparePyPIVersion
	case "maven":
		compare = compareMavenVersion
	case "rubygems":
		compare = compareRubyGemsVersion
	}
	if compare == nil {
		return false
	}
	ordering, valid := compare(left, right)
	return valid && ordering == 0
}

// consumptionProvesObservedVersion prevents a range-only manifest from
// borrowing an exact installed version supplied by an attached SBOM.
func consumptionProvesObservedVersion(
	ecosystem, observed string,
	consumption supplychainmodel.PackageConsumption,
) bool {
	consumed := strings.TrimSpace(consumption.ObservedVersion)
	if consumed == "" {
		var ok bool
		consumed, ok = exactConsumptionDependencyVersion(ecosystem, consumption)
		if !ok {
			return false
		}
	}
	return equivalentSupplyChainVersions(ecosystem, consumed, observed)
}
