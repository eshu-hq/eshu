// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/packageidentity"
)

const (
	supplyChainVersionReasonNPMSemverAffectedRange      = "npm_semver_affected_range"
	supplyChainVersionReasonNPMSemverKnownFixed         = "npm_semver_known_fixed"
	supplyChainVersionReasonNuGetSemverAffectedRange    = "nuget_semver_affected_range"
	supplyChainVersionReasonNuGetSemverKnownFixed       = "nuget_semver_known_fixed"
	supplyChainVersionReasonCargoSemverAffectedRange    = "cargo_semver_affected_range"
	supplyChainVersionReasonCargoSemverKnownFixed       = "cargo_semver_known_fixed"
	supplyChainVersionReasonHexSemverAffectedRange      = "hex_semver_affected_range"
	supplyChainVersionReasonHexSemverKnownFixed         = "hex_semver_known_fixed"
	supplyChainVersionReasonGoSemverAffectedRange       = "go_semver_affected_range"
	supplyChainVersionReasonGoSemverKnownFixed          = "go_semver_known_fixed"
	supplyChainVersionReasonPyPIPep440AffectedRange     = "pypi_pep440_affected_range"
	supplyChainVersionReasonPyPIPep440KnownFixed        = "pypi_pep440_known_fixed"
	supplyChainVersionReasonSwiftSemverAffectedRange    = "swift_semver_affected_range"
	supplyChainVersionReasonSwiftSemverKnownFixed       = "swift_semver_known_fixed"
	supplyChainVersionReasonPubSemverAffectedRange      = "pub_semver_affected_range"
	supplyChainVersionReasonPubSemverKnownFixed         = "pub_semver_known_fixed"
	supplyChainVersionReasonComposerSemverAffectedRange = "composer_semver_affected_range"
	supplyChainVersionReasonComposerSemverKnownFixed    = "composer_semver_known_fixed"
	supplyChainVersionReasonMavenRangeMatch             = "maven_range_match"
	supplyChainVersionReasonMavenKnownFixed             = "maven_known_fixed"
	supplyChainVersionReasonRPMExactAffected            = "rpm_exact_affected_version"
	supplyChainVersionReasonRPMKnownFixed               = "rpm_known_fixed"
	supplyChainVersionReasonRangeOnlyManifest           = "range_only_manifest"
	supplyChainVersionReasonUnsupportedEcosystem        = "unsupported_ecosystem"
	supplyChainVersionReasonMalformedRange              = "malformed_advisory_range"
	supplyChainVersionReasonMalformedInstalled          = "installed_version_malformed"
	supplyChainVersionReasonNoAffectedMatch             = "version_not_in_advisory_range"
	supplyChainVersionReasonMissingInstalled            = "installed_version_missing"

	supplyChainMissingUnsupportedMatcher = "ecosystem version matcher unsupported"
	supplyChainMissingMalformedRange     = "advisory version range malformed"
	supplyChainMissingMalformedInstalled = "installed package version malformed"
	supplyChainMissingInstalledVersion   = "installed package version missing"
)

type supplyChainVersionMatchDecision struct {
	Status              SupplyChainImpactStatus
	Confidence          string
	RuntimeReachability string
	Reason              string
	MissingEvidence     []string
	FailClosed          bool
}

func evaluateSupplyChainVersionMatch(
	ecosystem string,
	observed string,
	requestedRange string,
	fixedVersion string,
	pkgs []supplyChainAffectedPackage,
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

	normalized := normalizedSupplyChainVersionEcosystem(ecosystem)
	if normalized == "" {
		normalized = strings.ToLower(strings.TrimSpace(ecosystem))
	} else if normalized == string(packageidentity.EcosystemOS) {
		normalized = osPackageVersionFamily(pkgs)
		if normalized == "" {
			normalized = strings.ToLower(strings.TrimSpace(ecosystem))
		}
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

func osPackageVersionFamily(pkgs []supplyChainAffectedPackage) string {
	for _, pkg := range pkgs {
		if family := osPackageFamilyFromPURL(pkg.purl); family != "" {
			return family
		}
	}
	return ""
}

func normalizedSupplyChainVersionEcosystem(ecosystem string) string {
	return string(packageidentity.NormalizeEcosystem(packageidentity.Ecosystem(ecosystem)))
}

func evaluateNPMSemverMatch(
	observed string,
	fixedVersion string,
	pkgs []supplyChainAffectedPackage,
) supplyChainVersionMatchDecision {
	if !validSupplyChainSemver(observed) {
		return malformedInstalledVersionDecision()
	}
	if affected, malformed := npmAffectedByAnyPackage(observed, pkgs); affected {
		return affectedVersionDecision(supplyChainVersionReasonNPMSemverAffectedRange)
	} else if malformed {
		return malformedVersionDecision()
	}
	if fixedVersion != "" {
		fixed, valid := semverAtLeast(observed, fixedVersion)
		if !valid {
			return malformedVersionDecision()
		}
		if fixed {
			return knownFixedDecision(supplyChainVersionReasonNPMSemverKnownFixed)
		}
	}
	return possiblyAffectedDecision(supplyChainVersionReasonNoAffectedMatch, nil)
}

func npmAffectedByAnyPackage(observed string, pkgs []supplyChainAffectedPackage) (bool, bool) {
	malformed := false
	for _, pkg := range pkgs {
		if affected, valid := npmAffectedByPackage(observed, pkg); affected {
			return true, false
		} else if !valid {
			malformed = true
		}
	}
	return false, malformed
}

func npmAffectedByPackage(observed string, pkg supplyChainAffectedPackage) (bool, bool) {
	valid := true
	for _, candidate := range pkg.affectedVersions {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if equal, ok := semverEqual(observed, candidate); ok && equal {
			return true, true
		} else if !ok {
			valid = false
		}
	}
	for _, affectedRange := range pkg.affectedRanges {
		if !strings.EqualFold(affectedRange.kind, "SEMVER") {
			continue
		}
		if affected, ok := semverRangeContainsDecision(affectedRange, observed); affected {
			return true, true
		} else if !ok {
			valid = false
		}
	}
	if raw := strings.TrimSpace(pkg.affectedRangeRaw); raw != "" {
		if affected, ok := comparatorRangeContains(raw, observed, compareOSVSemver); affected {
			return true, true
		} else if !ok {
			valid = false
		}
	}
	return false, valid
}

func evaluateNuGetSemverMatch(
	observed string,
	fixedVersion string,
	pkgs []supplyChainAffectedPackage,
) supplyChainVersionMatchDecision {
	if !validNuGetVersion(observed) {
		return malformedInstalledVersionDecision()
	}
	if affected, malformed := nugetAffectedByAnyPackage(observed, pkgs); affected {
		return affectedVersionDecision(supplyChainVersionReasonNuGetSemverAffectedRange)
	} else if malformed {
		return malformedVersionDecision()
	}
	if fixedVersion != "" {
		cmp, valid := compareNuGetVersion(observed, fixedVersion)
		if !valid {
			return malformedVersionDecision()
		}
		if cmp >= 0 {
			return knownFixedDecision(supplyChainVersionReasonNuGetSemverKnownFixed)
		}
	}
	return possiblyAffectedDecision(supplyChainVersionReasonNoAffectedMatch, nil)
}

func nugetAffectedByAnyPackage(observed string, pkgs []supplyChainAffectedPackage) (bool, bool) {
	malformed := false
	for _, pkg := range pkgs {
		if affected, valid := nugetAffectedByPackage(observed, pkg); affected {
			return true, false
		} else if !valid {
			malformed = true
		}
	}
	return false, malformed
}

func nugetAffectedByPackage(observed string, pkg supplyChainAffectedPackage) (bool, bool) {
	valid := true
	for _, candidate := range pkg.affectedVersions {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if cmp, ok := compareNuGetVersion(observed, candidate); ok && cmp == 0 {
			return true, true
		} else if !ok {
			valid = false
		}
	}
	for _, affectedRange := range pkg.affectedRanges {
		if !strings.EqualFold(affectedRange.kind, "SEMVER") {
			continue
		}
		if affected, ok := nugetSemverRangeContainsDecision(affectedRange, observed); affected {
			return true, true
		} else if !ok {
			valid = false
		}
	}
	if raw := strings.TrimSpace(pkg.affectedRangeRaw); raw != "" {
		if affected, ok := nugetAffectedRangeRawContains(raw, observed); affected {
			return true, true
		} else if !ok {
			valid = false
		}
	}
	return false, valid
}

func evaluateCargoSemverMatch(
	observed string,
	fixedVersion string,
	pkgs []supplyChainAffectedPackage,
) supplyChainVersionMatchDecision {
	decision := evaluateNPMSemverMatch(observed, fixedVersion, pkgs)
	switch decision.Reason {
	case supplyChainVersionReasonNPMSemverAffectedRange:
		decision.Reason = supplyChainVersionReasonCargoSemverAffectedRange
	case supplyChainVersionReasonNPMSemverKnownFixed:
		decision.Reason = supplyChainVersionReasonCargoSemverKnownFixed
	}
	return decision
}

func evaluateGoSemverMatch(
	observed string,
	fixedVersion string,
	pkgs []supplyChainAffectedPackage,
) supplyChainVersionMatchDecision {
	if !validSupplyChainSemver(observed) {
		return malformedInstalledVersionDecision()
	}
	if affected, malformed := goModuleAffectedByAnyPackage(observed, pkgs); affected {
		return affectedVersionDecision(supplyChainVersionReasonGoSemverAffectedRange)
	} else if malformed {
		return malformedVersionDecision()
	}
	if fixedVersion != "" {
		cmp, valid := compareOSVSemver(observed, fixedVersion)
		if !valid {
			return malformedVersionDecision()
		}
		if cmp >= 0 {
			return knownFixedDecision(supplyChainVersionReasonGoSemverKnownFixed)
		}
	}
	return possiblyAffectedDecision(supplyChainVersionReasonNoAffectedMatch, nil)
}

func evaluateHexSemverMatch(
	observed string,
	fixedVersion string,
	pkgs []supplyChainAffectedPackage,
) supplyChainVersionMatchDecision {
	decision := evaluateNPMSemverMatch(observed, fixedVersion, pkgs)
	switch decision.Reason {
	case supplyChainVersionReasonNPMSemverAffectedRange:
		decision.Reason = supplyChainVersionReasonHexSemverAffectedRange
	case supplyChainVersionReasonNPMSemverKnownFixed:
		decision.Reason = supplyChainVersionReasonHexSemverKnownFixed
	}
	return decision
}

func evaluateSwiftSemverMatch(
	observed string,
	fixedVersion string,
	pkgs []supplyChainAffectedPackage,
) supplyChainVersionMatchDecision {
	decision := evaluateNPMSemverMatch(observed, fixedVersion, pkgs)
	switch decision.Reason {
	case supplyChainVersionReasonNPMSemverAffectedRange:
		decision.Reason = supplyChainVersionReasonSwiftSemverAffectedRange
	case supplyChainVersionReasonNPMSemverKnownFixed:
		decision.Reason = supplyChainVersionReasonSwiftSemverKnownFixed
	}
	return decision
}

func evaluatePubSemverMatch(
	observed string,
	fixedVersion string,
	pkgs []supplyChainAffectedPackage,
) supplyChainVersionMatchDecision {
	decision := evaluateNPMSemverMatch(observed, fixedVersion, pkgs)
	switch decision.Reason {
	case supplyChainVersionReasonNPMSemverAffectedRange:
		decision.Reason = supplyChainVersionReasonPubSemverAffectedRange
	case supplyChainVersionReasonNPMSemverKnownFixed:
		decision.Reason = supplyChainVersionReasonPubSemverKnownFixed
	}
	return decision
}

func evaluateMavenVersionMatch(
	observed string,
	fixedVersion string,
	pkgs []supplyChainAffectedPackage,
) supplyChainVersionMatchDecision {
	if !validMavenVersion(observed) {
		return malformedInstalledVersionDecision()
	}
	if affected, malformed := mavenAffectedByAnyPackage(observed, pkgs); affected {
		return affectedVersionDecision(supplyChainVersionReasonMavenRangeMatch)
	} else if malformed {
		return malformedVersionDecision()
	}
	if fixedVersion != "" {
		cmp, valid := compareMavenVersion(observed, fixedVersion)
		if !valid {
			return malformedVersionDecision()
		}
		if cmp >= 0 {
			return knownFixedDecision(supplyChainVersionReasonMavenKnownFixed)
		}
	}
	return possiblyAffectedDecision(supplyChainVersionReasonNoAffectedMatch, nil)
}

func mavenAffectedByAnyPackage(observed string, pkgs []supplyChainAffectedPackage) (bool, bool) {
	malformed := false
	for _, pkg := range pkgs {
		if affected, valid := mavenAffectedByPackage(observed, pkg); affected {
			return true, false
		} else if !valid {
			malformed = true
		}
	}
	return false, malformed
}

func mavenAffectedByPackage(observed string, pkg supplyChainAffectedPackage) (bool, bool) {
	valid := true
	for _, candidate := range pkg.affectedVersions {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if cmp, ok := compareMavenVersion(observed, candidate); ok && cmp == 0 {
			return true, true
		} else if !ok {
			valid = false
		}
	}
	if raw := strings.TrimSpace(pkg.affectedRangeRaw); raw != "" {
		if affected, ok := mavenRangeContains(raw, observed); affected {
			return true, true
		} else if !ok {
			valid = false
		}
	}
	return false, valid
}

func semverEqual(left string, right string) (bool, bool) {
	cmp, ok := compareOSVSemver(left, right)
	return cmp == 0, ok
}

func validSupplyChainSemver(raw string) bool {
	_, ok := normalizeOSVSemver(raw)
	return ok
}
