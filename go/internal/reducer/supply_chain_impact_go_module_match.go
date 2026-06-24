// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "strings"

func goModuleAffectedByAnyPackage(observed string, pkgs []supplyChainAffectedPackage) (bool, bool) {
	malformed := false
	for _, pkg := range pkgs {
		if affected, valid := goModuleAffectedByPackage(observed, pkg); affected {
			return true, false
		} else if !valid {
			malformed = true
		}
	}
	return false, malformed
}

func goModuleAffectedByPackage(observed string, pkg supplyChainAffectedPackage) (bool, bool) {
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
		if !strings.EqualFold(affectedRange.kind, "SEMVER") &&
			!strings.EqualFold(affectedRange.kind, "ECOSYSTEM") {
			continue
		}
		if affected, ok := goModuleRangeContainsDecision(affectedRange, observed); affected {
			return true, true
		} else if !ok {
			valid = false
		}
	}
	if raw := strings.TrimSpace(pkg.affectedRangeRaw); raw != "" {
		if affected, ok := goModuleComparatorRangeContains(raw, observed); affected {
			return true, true
		} else if !ok {
			valid = false
		}
	}
	return false, valid
}

func goModuleRangeContainsDecision(
	affectedRange supplyChainAffectedRange,
	observed string,
) (bool, bool) {
	if ok, valid := versionBeforeLimitsDecision(observed, affectedRange.events, compareOSVSemver); !valid {
		return false, false
	} else if !ok {
		return false, true
	}
	vulnerable := false
	for _, event := range affectedRange.events {
		switch {
		case event.introduced != "":
			if goModuleVersionFloor(event.introduced) {
				vulnerable = true
				continue
			}
			if ok, valid := semverAtLeast(observed, event.introduced); !valid {
				return false, false
			} else if ok {
				vulnerable = true
			}
		case event.fixed != "":
			if ok, valid := semverAtLeast(observed, event.fixed); !valid {
				return false, false
			} else if ok {
				vulnerable = false
			}
		case event.lastAffected != "":
			if ok, valid := versionGreaterThan(observed, event.lastAffected, compareOSVSemver); !valid {
				return false, false
			} else if ok {
				vulnerable = false
			}
		}
	}
	return vulnerable, true
}

func goModuleComparatorRangeContains(raw string, observed string) (bool, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false, true
	}
	malformed := false
	for _, branch := range comparatorRangeBranches(raw) {
		branch = strings.TrimSpace(branch)
		if branch == "" {
			malformed = true
			continue
		}
		ok, valid := goModuleComparatorBranchContains(branch, observed)
		if ok {
			return true, true
		}
		if !valid {
			malformed = true
		}
	}
	return false, !malformed
}

func goModuleComparatorBranchContains(branch string, observed string) (bool, bool) {
	fields := strings.Fields(branch)
	if len(fields) == 0 {
		return false, false
	}
	for _, field := range fields {
		ok, valid := goModuleComparatorConstraintContains(field, observed)
		if !valid || !ok {
			return false, valid
		}
	}
	return true, true
}

func goModuleComparatorConstraintContains(token string, observed string) (bool, bool) {
	operator, version := splitVersionComparator(token)
	if version == "" {
		return false, false
	}
	if operator == ">=" && goModuleVersionFloor(version) {
		return validSupplyChainSemver(observed), true
	}
	return comparatorConstraintContains(token, observed, compareOSVSemver)
}

func goModuleVersionFloor(raw string) bool {
	normalized, ok := normalizeOSVSemver(raw)
	return ok && normalized == "v0.0.0"
}
