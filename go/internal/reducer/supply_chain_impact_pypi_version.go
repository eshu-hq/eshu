// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

var pypiVersionPattern = regexp.MustCompile(`^(?:(\d+)!)?(\d+(?:\.\d+)*)(?:(a|b|rc)(\d+))?(?:\.post(\d+))?(?:\.dev(\d+))?$`)

type pypiVersion struct {
	epoch     int
	release   []int
	stageRank int
	stageNum  int
}

func evaluatePyPIPep440Match(
	observed string,
	fixedVersion string,
	pkgs []supplychainmodel.AffectedPackage,
) supplyChainVersionMatchDecision {
	if !validPyPIVersion(observed) {
		return malformedInstalledVersionDecision()
	}
	if affected, malformed := pypiAffectedByAnyPackage(observed, pkgs); affected {
		return affectedVersionDecision(supplyChainVersionReasonPyPIPep440AffectedRange)
	} else if malformed {
		return malformedVersionDecision()
	}
	if fixedVersion != "" {
		cmp, valid := comparePyPIVersion(observed, fixedVersion)
		if !valid {
			return malformedVersionDecision()
		}
		if cmp >= 0 {
			return knownFixedDecision(supplyChainVersionReasonPyPIPep440KnownFixed)
		}
	}
	return possiblyAffectedDecision(supplyChainVersionReasonNoAffectedMatch, nil)
}

func pypiAffectedByAnyPackage(observed string, pkgs []supplychainmodel.AffectedPackage) (bool, bool) {
	malformed := false
	for _, pkg := range pkgs {
		if affected, valid := pypiAffectedByPackage(observed, pkg); affected {
			return true, false
		} else if !valid {
			malformed = true
		}
	}
	return false, malformed
}

func pypiAffectedByPackage(observed string, pkg supplychainmodel.AffectedPackage) (bool, bool) {
	valid := true
	for _, candidate := range pkg.AffectedVersions {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if cmp, ok := comparePyPIVersion(observed, candidate); ok && cmp == 0 {
			return true, true
		} else if !ok {
			valid = false
		}
	}
	for _, affectedRange := range pkg.AffectedRanges {
		if !strings.EqualFold(affectedRange.Kind, "ECOSYSTEM") &&
			!strings.EqualFold(affectedRange.Kind, "SEMVER") {
			continue
		}
		if affected, ok := pypiRangeContainsDecision(affectedRange, observed); affected {
			return true, true
		} else if !ok {
			valid = false
		}
	}
	if raw := strings.TrimSpace(pkg.AffectedRangeRaw); raw != "" {
		if affected, ok := pypiSpecifierSetContains(raw, observed); affected {
			return true, true
		} else if !ok {
			valid = false
		}
	}
	return false, valid
}

func pypiRangeContainsDecision(
	affectedRange supplychainmodel.AffectedRange,
	observed string,
) (bool, bool) {
	if ok, valid := pypiBeforeLimitsDecision(observed, affectedRange.Events); !valid {
		return false, false
	} else if !ok {
		return false, true
	}
	vulnerable := false
	for _, event := range affectedRange.Events {
		switch {
		case event.Introduced != "":
			if ok, valid := pypiAtLeast(observed, event.Introduced); !valid {
				return false, false
			} else if ok {
				vulnerable = true
			}
		case event.Fixed != "":
			if ok, valid := pypiAtLeast(observed, event.Fixed); !valid {
				return false, false
			} else if ok {
				vulnerable = false
			}
		case event.LastAffected != "":
			if ok, valid := pypiGreaterThan(observed, event.LastAffected); !valid {
				return false, false
			} else if ok {
				vulnerable = false
			}
		}
	}
	return vulnerable, true
}

func pypiBeforeLimitsDecision(observed string, events []supplychainmodel.AffectedRangeEvent) (bool, bool) {
	hasLimit := false
	for _, event := range events {
		limit := strings.TrimSpace(event.Limit)
		if limit == "" {
			continue
		}
		hasLimit = true
		if limit == "*" {
			return true, true
		}
		if ok, valid := pypiLessThan(observed, limit); !valid {
			return false, false
		} else if ok {
			return true, true
		}
	}
	return !hasLimit, true
}

func pypiAtLeast(left string, right string) (bool, bool) {
	cmp, ok := comparePyPIVersion(left, right)
	return cmp >= 0, ok
}

func pypiGreaterThan(left string, right string) (bool, bool) {
	cmp, ok := comparePyPIVersion(left, right)
	return cmp > 0, ok
}

func pypiLessThan(left string, right string) (bool, bool) {
	cmp, ok := comparePyPIVersion(left, right)
	return cmp < 0, ok
}

func comparePyPIVersion(left string, right string) (int, bool) {
	leftVersion, ok := parsePyPIVersion(left)
	if !ok {
		return 0, false
	}
	rightVersion, ok := parsePyPIVersion(right)
	if !ok {
		return 0, false
	}
	if leftVersion.epoch != rightVersion.epoch {
		if leftVersion.epoch < rightVersion.epoch {
			return -1, true
		}
		return 1, true
	}
	maxRelease := maxInt(len(leftVersion.release), len(rightVersion.release))
	for i := 0; i < maxRelease; i++ {
		leftPart := releasePart(leftVersion.release, i)
		rightPart := releasePart(rightVersion.release, i)
		if leftPart < rightPart {
			return -1, true
		}
		if leftPart > rightPart {
			return 1, true
		}
	}
	if leftVersion.stageRank != rightVersion.stageRank {
		if leftVersion.stageRank < rightVersion.stageRank {
			return -1, true
		}
		return 1, true
	}
	if leftVersion.stageNum < rightVersion.stageNum {
		return -1, true
	}
	if leftVersion.stageNum > rightVersion.stageNum {
		return 1, true
	}
	return 0, true
}

func validPyPIVersion(raw string) bool {
	_, ok := parsePyPIVersion(raw)
	return ok
}

func parsePyPIVersion(raw string) (pypiVersion, bool) {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	if normalized == "" {
		return pypiVersion{}, false
	}
	normalized = strings.TrimPrefix(normalized, "v")
	if localAt := strings.Index(normalized, "+"); localAt >= 0 {
		normalized = normalized[:localAt]
	}
	matches := pypiVersionPattern.FindStringSubmatch(normalized)
	if matches == nil {
		return pypiVersion{}, false
	}
	epoch := 0
	if matches[1] != "" {
		parsed, err := strconv.Atoi(matches[1])
		if err != nil {
			return pypiVersion{}, false
		}
		epoch = parsed
	}
	releaseParts := strings.Split(matches[2], ".")
	release := make([]int, 0, len(releaseParts))
	for _, part := range releaseParts {
		parsed, err := strconv.Atoi(part)
		if err != nil {
			return pypiVersion{}, false
		}
		release = append(release, parsed)
	}
	stageRank := 0
	stageNum := 0
	if matches[5] != "" {
		stageRank = 1
		stageNum = atoiDefault(matches[5])
	} else if matches[6] != "" {
		stageRank = -4
		stageNum = atoiDefault(matches[6])
	} else if matches[3] != "" {
		switch matches[3] {
		case "a":
			stageRank = -3
		case "b":
			stageRank = -2
		case "rc":
			stageRank = -1
		default:
			return pypiVersion{}, false
		}
		stageNum = atoiDefault(matches[4])
	}
	return pypiVersion{
		epoch:     epoch,
		release:   release,
		stageRank: stageRank,
		stageNum:  stageNum,
	}, true
}

func releasePart(parts []int, index int) int {
	if index >= len(parts) {
		return 0
	}
	return parts[index]
}

func atoiDefault(raw string) int {
	value, _ := strconv.Atoi(raw)
	return value
}

func maxInt(left int, right int) int {
	if left > right {
		return left
	}
	return right
}
