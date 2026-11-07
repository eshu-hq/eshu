// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/packageidentity"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// evaluateSupplyChainVersionMatchForRow is evaluateSupplyChainVersionMatch
// for a finding joined to an installed row: a blank or "os" ecosystem reads
// its version family from the row's package manager (#10034 item 2), which is
// host truth, instead of borrowing the first OS purl in fact order from a
// fact that may not join the row at all. A caller with no joined row passes a
// blank manager and keeps the purl fallback.
func evaluateSupplyChainVersionMatchForRow(
	ecosystem string,
	observed string,
	requestedRange string,
	fixedVersion string,
	pkgs []supplychainmodel.AffectedPackage,
	installedPackageManager string,
) supplyChainVersionMatchDecision {
	observed = strings.TrimSpace(observed)
	requestedRange = strings.TrimSpace(requestedRange)
	fixedVersion = strings.TrimSpace(fixedVersion)
	if observed == "" {
		if requestedRange != "" {
			return supplyChainVersionMatchDecision{
				Reason:          supplyChainVersionReasonRangeOnlyManifest,
				MissingEvidence: []string{supplyChainMissingInstalledVersion},
			}
		}
		return supplyChainVersionMatchDecision{
			Reason:          supplyChainVersionReasonMissingInstalled,
			MissingEvidence: []string{supplyChainMissingInstalledVersion},
		}
	}

	// A blank ecosystem reads the family from the joined row's package manager
	// when there is one, as "os" does (#10034 item 2): the same fact earns the
	// dpkg verdict under either, however the facts arrive. Without a joined
	// row it falls back to the group's OS purls, which must agree: a group
	// whose purls name several families names no family, so the answer stays
	// unsupported instead of depending on fact order. A purl that is not an OS
	// purl names no family, so a language fact is untouched. A spelling the
	// identity table does not know, such as dpkg, keeps the matcher it names.
	raw := packageidentity.LowerASCII(strings.TrimSpace(ecosystem))
	normalized := normalizedSupplyChainVersionEcosystem(ecosystem)
	switch {
	case raw == "" || normalized == string(packageidentity.EcosystemOS):
		normalized = osPackageVersionFamilyForRow(installedPackageManager, pkgs)
		if normalized == "" {
			normalized = raw
		}
	case normalized == "":
		normalized = raw
	}
	switch normalized {
	case string(packageidentity.EcosystemNPM):
		return evaluateNPMSemverMatch(observed, fixedVersion, pkgs)
	case string(packageidentity.EcosystemNuGet):
		return evaluateNuGetSemverMatch(observed, fixedVersion, pkgs)
	case string(packageidentity.EcosystemCargo):
		return evaluateCargoSemverMatch(observed, fixedVersion, pkgs)
	case string(packageidentity.EcosystemHex):
		return evaluateHexSemverMatch(observed, fixedVersion, pkgs)
	case string(packageidentity.EcosystemGoModule):
		return evaluateGoSemverMatch(observed, fixedVersion, pkgs)
	case string(packageidentity.EcosystemPyPI):
		return evaluatePyPIPep440Match(observed, fixedVersion, pkgs)
	case string(packageidentity.EcosystemSwift):
		return evaluateSwiftSemverMatch(observed, fixedVersion, pkgs)
	case string(packageidentity.EcosystemPub):
		return evaluatePubSemverMatch(observed, fixedVersion, pkgs)
	case string(packageidentity.EcosystemComposer):
		return evaluateComposerSemverMatch(observed, fixedVersion, pkgs)
	case string(packageidentity.EcosystemMaven):
		return evaluateMavenVersionMatch(observed, fixedVersion, pkgs)
	case "redhat", "fedora", "centos", "rocky", "alma", "amazonlinux", "rpm":
		return evaluateRPMVersionMatch(observed, fixedVersion, pkgs)
	case "debian", "ubuntu", "deb", "dpkg":
		return evaluateOSPackageVersionMatch(
			observed,
			fixedVersion,
			pkgs,
			supplyChainVersionReasonDPKGExactAffected,
			supplyChainVersionReasonDPKGExactKnownFixed,
			supplyChainVersionReasonDPKGAffectedRange,
			supplyChainVersionReasonDPKGKnownFixed,
			compareDPKGVersion,
		)
	case "alpine", "apk":
		return evaluateOSPackageVersionMatch(
			observed,
			fixedVersion,
			pkgs,
			supplyChainVersionReasonAPKExactAffected,
			supplyChainVersionReasonAPKExactKnownFixed,
			supplyChainVersionReasonAPKAffectedRange,
			supplyChainVersionReasonAPKKnownFixed,
			compareAPKVersion,
		)
	case string(packageidentity.EcosystemRubyGems):
		return evaluateRubyGemsVersionMatch(observed, fixedVersion, pkgs)
	default:
		return supplyChainVersionMatchDecision{
			Status:          SupplyChainImpactPossiblyAffected,
			Confidence:      "partial",
			Reason:          supplyChainVersionReasonUnsupportedEcosystem,
			MissingEvidence: []string{supplyChainMissingUnsupportedMatcher},
			FailClosed:      true,
		}
	}
}

// osPackageVersionFamilyForRow derives the version family for a blank or "os"
// ecosystem: the joined row's package manager first, because it is host truth
// (#10034 item 2). A fact with no purl of its own no longer borrows one from
// a fact that cannot join, and the row answers the same however the facts
// arrive.
func osPackageVersionFamilyForRow(installedPackageManager string, pkgs []supplychainmodel.AffectedPackage) string {
	if family := osPackageFamilyFromPackageManager(installedPackageManager); family != "" {
		return family
	}
	return osPackageVersionFamily(pkgs)
}

// osPackageVersionFamily derives the version family from the group's OS
// purls, which must agree: a group whose purls name several families names no
// family, so the answer stays unsupported instead of depending on fact order.
func osPackageVersionFamily(pkgs []supplychainmodel.AffectedPackage) string {
	family := ""
	for _, pkg := range pkgs {
		candidate := osPackageFamilyFromPURL(pkg.PURL)
		if candidate == "" {
			continue
		}
		if family == "" {
			family = candidate
			continue
		}
		if candidate != family {
			return ""
		}
	}
	return family
}
