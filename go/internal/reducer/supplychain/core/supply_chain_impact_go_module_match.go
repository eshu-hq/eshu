// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

func goModuleAffectedByAnyPackage(observed string, pkgs []supplychainmodel.AffectedPackage) (bool, bool) {
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

func goModuleAffectedByPackage(observed string, pkg supplychainmodel.AffectedPackage) (bool, bool) {
	valid := true
	for _, candidate := range pkg.AffectedVersions {
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
	for _, affectedRange := range pkg.AffectedRanges {
		if !strings.EqualFold(affectedRange.Kind, "SEMVER") &&
			!strings.EqualFold(affectedRange.Kind, "ECOSYSTEM") {
			continue
		}
		if affected, ok := goModuleRangeContainsDecision(affectedRange, observed); affected {
			return true, true
		} else if !ok {
			valid = false
		}
	}
	if raw := strings.TrimSpace(pkg.AffectedRangeRaw); raw != "" {
		if affected, ok := goModuleComparatorRangeContains(raw, observed); affected {
			return true, true
		} else if !ok {
			valid = false
		}
	}
	return false, valid
}

func goModuleRangeContainsDecision(
	affectedRange supplychainmodel.AffectedRange,
	observed string,
) (bool, bool) {
	if ok, valid := versionBeforeLimitsDecision(observed, affectedRange.Events, compareOSVSemver); !valid {
		return false, false
	} else if !ok {
		return false, true
	}
	vulnerable := false
	for _, event := range affectedRange.Events {
		switch {
		case event.Introduced != "":
			if goModuleVersionFloor(event.Introduced) {
				vulnerable = true
				continue
			}
			if ok, valid := semverAtLeast(observed, event.Introduced); !valid {
				return false, false
			} else if ok {
				vulnerable = true
			}
		case event.Fixed != "":
			if ok, valid := semverAtLeast(observed, event.Fixed); !valid {
				return false, false
			} else if ok {
				vulnerable = false
			}
		case event.LastAffected != "":
			if ok, valid := versionGreaterThan(observed, event.LastAffected, compareOSVSemver); !valid {
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
