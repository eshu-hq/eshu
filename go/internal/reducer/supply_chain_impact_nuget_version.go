// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strconv"
	"strings"
)

type nugetVersionParts struct {
	numeric    [4]int
	prerelease []string
}

func validNuGetVersion(raw string) bool {
	_, ok := parseNuGetVersion(raw)
	return ok
}

func compareNuGetVersion(left string, right string) (int, bool) {
	leftVersion, ok := parseNuGetVersion(left)
	if !ok {
		return 0, false
	}
	rightVersion, ok := parseNuGetVersion(right)
	if !ok {
		return 0, false
	}
	return compareNuGetVersionParts(leftVersion, rightVersion), true
}

func parseNuGetVersion(raw string) (nugetVersionParts, bool) {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		return nugetVersionParts{}, false
	}
	value = strings.TrimPrefix(value, "v")
	value, _, _ = strings.Cut(value, "+")
	mainVersion, prerelease, hasPrerelease := strings.Cut(value, "-")
	segments := strings.Split(mainVersion, ".")
	if len(segments) == 0 || len(segments) > 4 {
		return nugetVersionParts{}, false
	}

	version := nugetVersionParts{}
	for index, segment := range segments {
		if segment == "" {
			return nugetVersionParts{}, false
		}
		number, err := strconv.Atoi(segment)
		if err != nil || number < 0 {
			return nugetVersionParts{}, false
		}
		version.numeric[index] = number
	}
	if hasPrerelease {
		prerelease = strings.TrimSpace(prerelease)
		if prerelease == "" {
			return nugetVersionParts{}, false
		}
		version.prerelease = strings.Split(prerelease, ".")
		for _, identifier := range version.prerelease {
			if strings.TrimSpace(identifier) == "" {
				return nugetVersionParts{}, false
			}
		}
	}
	return version, true
}

func compareNuGetVersionParts(left nugetVersionParts, right nugetVersionParts) int {
	for index, leftNumber := range left.numeric {
		if leftNumber < right.numeric[index] {
			return -1
		}
		if leftNumber > right.numeric[index] {
			return 1
		}
	}
	return compareNuGetPrerelease(left.prerelease, right.prerelease)
}

func compareNuGetPrerelease(left []string, right []string) int {
	if len(left) == 0 && len(right) == 0 {
		return 0
	}
	if len(left) == 0 {
		return 1
	}
	if len(right) == 0 {
		return -1
	}
	for index, leftIdentifier := range left {
		if index >= len(right) {
			return 1
		}
		rightIdentifier := right[index]
		if leftNumber, leftIsNumber := nugetPrereleaseNumber(leftIdentifier); leftIsNumber {
			rightNumber, rightIsNumber := nugetPrereleaseNumber(rightIdentifier)
			if rightIsNumber {
				if leftNumber < rightNumber {
					return -1
				}
				if leftNumber > rightNumber {
					return 1
				}
				continue
			}
			return -1
		} else if _, rightIsNumber := nugetPrereleaseNumber(rightIdentifier); rightIsNumber {
			return 1
		}
		if cmp := strings.Compare(leftIdentifier, rightIdentifier); cmp != 0 {
			return cmp
		}
	}
	if len(left) < len(right) {
		return -1
	}
	return 0
}

func nugetPrereleaseNumber(value string) (int, bool) {
	if value == "" {
		return 0, false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return 0, false
		}
	}
	number, err := strconv.Atoi(value)
	if err != nil || number < 0 {
		return 0, false
	}
	return number, true
}

func nugetSemverRangeContainsDecision(
	affectedRange supplyChainAffectedRange,
	observed string,
) (bool, bool) {
	if ok, valid := nugetVersionBeforeLimitsDecision(observed, affectedRange.events); !valid {
		return false, false
	} else if !ok {
		return false, true
	}
	vulnerable := false
	for _, event := range affectedRange.events {
		switch {
		case event.introduced != "":
			if ok, valid := nugetVersionAtLeast(observed, event.introduced); !valid {
				return false, false
			} else if ok {
				vulnerable = true
			}
		case event.fixed != "":
			if ok, valid := nugetVersionAtLeast(observed, event.fixed); !valid {
				return false, false
			} else if ok {
				vulnerable = false
			}
		case event.lastAffected != "":
			if ok, valid := nugetVersionGreaterThan(observed, event.lastAffected); !valid {
				return false, false
			} else if ok {
				vulnerable = false
			}
		}
	}
	return vulnerable, true
}

func nugetAffectedRangeRawContains(raw string, observed string) (bool, bool) {
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
		var (
			affected bool
			valid    bool
		)
		if nugetRangeBranchUsesBounds(branch) {
			affected, valid = nugetRangeBranchContains(branch, observed)
		} else {
			affected, valid = comparatorRangeContains(branch, observed, compareNuGetVersion)
		}
		if affected {
			return true, true
		}
		if !valid {
			malformed = true
		}
	}
	return false, !malformed
}

func nugetRangeBranchUsesBounds(raw string) bool {
	if raw == "" {
		return false
	}
	first := raw[0]
	return first == '[' || first == '('
}

func nugetRangeBranchContains(raw string, observed string) (bool, bool) {
	raw = strings.TrimSpace(raw)
	if len(raw) < 2 {
		return false, false
	}
	lowerInclusive := raw[0] == '['
	lowerExclusive := raw[0] == '('
	upperInclusive := raw[len(raw)-1] == ']'
	upperExclusive := raw[len(raw)-1] == ')'
	if (!lowerInclusive && !lowerExclusive) || (!upperInclusive && !upperExclusive) {
		return false, false
	}
	body := strings.TrimSpace(raw[1 : len(raw)-1])
	if body == "" {
		return false, false
	}
	if !strings.Contains(body, ",") {
		if !lowerInclusive || !upperInclusive {
			return false, false
		}
		cmp, ok := compareNuGetVersion(observed, body)
		return cmp == 0, ok
	}
	lower, upper, ok := strings.Cut(body, ",")
	if !ok {
		return false, false
	}
	lower = strings.TrimSpace(lower)
	upper = strings.TrimSpace(upper)
	if lower == "" && upper == "" {
		return false, false
	}
	if lower != "" {
		cmp, valid := compareNuGetVersion(observed, lower)
		if !valid {
			return false, false
		}
		if lowerInclusive && cmp < 0 {
			return false, true
		}
		if lowerExclusive && cmp <= 0 {
			return false, true
		}
	}
	if upper != "" {
		cmp, valid := compareNuGetVersion(observed, upper)
		if !valid {
			return false, false
		}
		if upperInclusive && cmp > 0 {
			return false, true
		}
		if upperExclusive && cmp >= 0 {
			return false, true
		}
	}
	return true, true
}

func nugetVersionBeforeLimitsDecision(observed string, events []supplyChainAffectedRangeEvent) (bool, bool) {
	hasLimit := false
	for _, event := range events {
		limit := event.limit
		if limit == "" {
			continue
		}
		hasLimit = true
		if limit == "*" {
			return true, true
		}
		if ok, valid := nugetVersionLessThan(observed, limit); !valid {
			return false, false
		} else if ok {
			return true, true
		}
	}
	return !hasLimit, true
}

func nugetVersionAtLeast(left string, right string) (bool, bool) {
	cmp, ok := compareNuGetVersion(left, right)
	return cmp >= 0, ok
}

func nugetVersionGreaterThan(left string, right string) (bool, bool) {
	cmp, ok := compareNuGetVersion(left, right)
	return cmp > 0, ok
}

func nugetVersionLessThan(left string, right string) (bool, bool) {
	cmp, ok := compareNuGetVersion(left, right)
	return cmp < 0, ok
}
