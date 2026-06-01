package reducer

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/packageidentity"
)

const (
	supplyChainVersionReasonNPMSemverAffectedRange   = "npm_semver_affected_range"
	supplyChainVersionReasonNPMSemverKnownFixed      = "npm_semver_known_fixed"
	supplyChainVersionReasonNuGetSemverAffectedRange = "nuget_semver_affected_range"
	supplyChainVersionReasonNuGetSemverKnownFixed    = "nuget_semver_known_fixed"
	supplyChainVersionReasonCargoSemverAffectedRange = "cargo_semver_affected_range"
	supplyChainVersionReasonCargoSemverKnownFixed    = "cargo_semver_known_fixed"
	supplyChainVersionReasonPyPIPep440AffectedRange  = "pypi_pep440_affected_range"
	supplyChainVersionReasonPyPIPep440KnownFixed     = "pypi_pep440_known_fixed"
	supplyChainVersionReasonMavenRangeMatch          = "maven_range_match"
	supplyChainVersionReasonMavenKnownFixed          = "maven_known_fixed"
	supplyChainVersionReasonRPMExactAffected         = "rpm_exact_affected_version"
	supplyChainVersionReasonRPMKnownFixed            = "rpm_known_fixed"
	supplyChainVersionReasonRangeOnlyManifest        = "range_only_manifest"
	supplyChainVersionReasonUnsupportedEcosystem     = "unsupported_ecosystem"
	supplyChainVersionReasonMalformedRange           = "malformed_advisory_range"
	supplyChainVersionReasonMalformedInstalled       = "installed_version_malformed"
	supplyChainVersionReasonNoAffectedMatch          = "version_not_in_advisory_range"
	supplyChainVersionReasonMissingInstalled         = "installed_version_missing"

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
	}
	switch normalized {
	case string(packageidentity.EcosystemNPM):
		return evaluateNPMSemverMatch(observed, fixedVersion, pkgs)
	case string(packageidentity.EcosystemNuGet):
		return evaluateNuGetSemverMatch(observed, fixedVersion, pkgs)
	case string(packageidentity.EcosystemCargo):
		return evaluateCargoSemverMatch(observed, fixedVersion, pkgs)
	case string(packageidentity.EcosystemPyPI):
		return evaluatePyPIPep440Match(observed, fixedVersion, pkgs)
	case string(packageidentity.EcosystemMaven):
		return evaluateMavenVersionMatch(observed, fixedVersion, pkgs)
	case "redhat", "fedora", "centos", "rocky", "alma", "amazonlinux", "rpm":
		return evaluateRPMVersionMatch(observed, fixedVersion, pkgs)
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

func affectedVersionDecision(reason string) supplyChainVersionMatchDecision {
	return supplyChainVersionMatchDecision{
		Status:              SupplyChainImpactAffectedExact,
		Confidence:          "exact",
		RuntimeReachability: "package_manifest",
		Reason:              reason,
	}
}

func knownFixedDecision(reason string) supplyChainVersionMatchDecision {
	return supplyChainVersionMatchDecision{
		Status:              SupplyChainImpactNotAffectedKnownFixed,
		Confidence:          "exact",
		RuntimeReachability: "known_fixed",
		Reason:              reason,
	}
}

func malformedVersionDecision() supplyChainVersionMatchDecision {
	return possiblyAffectedDecision(supplyChainVersionReasonMalformedRange, []string{supplyChainMissingMalformedRange})
}

func malformedInstalledVersionDecision() supplyChainVersionMatchDecision {
	return possiblyAffectedDecision(
		supplyChainVersionReasonMalformedInstalled,
		[]string{supplyChainMissingMalformedInstalled},
	)
}

func possiblyAffectedDecision(reason string, missing []string) supplyChainVersionMatchDecision {
	return supplyChainVersionMatchDecision{
		Status:          SupplyChainImpactPossiblyAffected,
		Confidence:      "partial",
		Reason:          reason,
		MissingEvidence: missing,
		FailClosed: reason == supplyChainVersionReasonMalformedRange ||
			reason == supplyChainVersionReasonMalformedInstalled,
	}
}

func semverEqual(left string, right string) (bool, bool) {
	cmp, ok := compareOSVSemver(left, right)
	return cmp == 0, ok
}

func validSupplyChainSemver(raw string) bool {
	_, ok := normalizeOSVSemver(raw)
	return ok
}

type versionCompareFunc func(string, string) (int, bool)

func comparatorRangeContains(raw string, observed string, compare versionCompareFunc) (bool, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false, true
	}
	malformed := false
	for _, branch := range strings.Split(raw, "||") {
		branch = strings.TrimSpace(branch)
		if branch == "" {
			malformed = true
			continue
		}
		ok, valid := comparatorBranchContains(branch, observed, compare)
		if ok {
			return true, true
		}
		if !valid {
			malformed = true
		}
	}
	return false, !malformed
}

func comparatorBranchContains(branch string, observed string, compare versionCompareFunc) (bool, bool) {
	fields := strings.Fields(branch)
	if len(fields) == 0 {
		return false, false
	}
	for _, field := range fields {
		ok, valid := comparatorConstraintContains(field, observed, compare)
		if !valid || !ok {
			return false, valid
		}
	}
	return true, true
}

func comparatorConstraintContains(token string, observed string, compare versionCompareFunc) (bool, bool) {
	operator, version := splitVersionComparator(token)
	if version == "" {
		return false, false
	}
	cmp, valid := compare(observed, version)
	if !valid {
		return false, false
	}
	switch operator {
	case "", "=", "==":
		return cmp == 0, true
	case "<":
		return cmp < 0, true
	case "<=":
		return cmp <= 0, true
	case ">":
		return cmp > 0, true
	case ">=":
		return cmp >= 0, true
	case "!=":
		return cmp != 0, true
	default:
		return false, false
	}
}

func splitVersionComparator(token string) (string, string) {
	for _, operator := range []string{">=", "<=", "==", "!=", ">", "<", "="} {
		if strings.HasPrefix(token, operator) {
			return operator, strings.TrimSpace(strings.TrimPrefix(token, operator))
		}
	}
	if strings.HasPrefix(token, "^") || strings.HasPrefix(token, "~") {
		return token[:1], ""
	}
	return "", strings.TrimSpace(token)
}
