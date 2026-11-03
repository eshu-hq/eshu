// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import "strings"

type versionCompareFunc func(string, string) (int, bool)

func comparatorRangeContains(raw string, observed string, compare versionCompareFunc) (bool, bool) {
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

func comparatorRangeBranches(raw string) []string {
	raw = strings.ReplaceAll(raw, "||", "|")
	return strings.Split(raw, "|")
}

// comparatorBranchContains reads one AND-branch of a comparator range. Every
// field is read before the branch answers: a field that does not match must not
// hide a later field that is malformed, or the branch would read as a valid
// "not contained" (#9865). The npm hyphen form "A - B" reads as ">=A <=B".
func comparatorBranchContains(branch string, observed string, compare versionCompareFunc) (bool, bool) {
	fields := strings.Fields(branch)
	if len(fields) == 0 {
		return false, false
	}
	contained, valid := true, true
	for i := 0; i < len(fields); i++ {
		if i+1 < len(fields) && fields[i+1] == "-" {
			ok, wellFormed, consumed := hyphenRangeContains(fields[i:], observed, compare)
			if !wellFormed {
				return false, false
			}
			contained = contained && ok
			i += consumed - 1
			continue
		}
		ok, wellFormed := comparatorConstraintContains(fields[i], observed, compare)
		if !wellFormed {
			valid = false
			continue
		}
		contained = contained && ok
	}
	if !valid {
		return false, false
	}
	return contained, true
}

// hyphenRangeContains reads the npm hyphen range "A - B" at the head of fields
// as ">=A <=B" and reports how many fields it used. It is well formed only for
// two full versions: npm widens a partial bound ("1.2 - 2.3" is
// ">=1.2.0 <2.4.0"), which the matcher cannot expand, so a partial bound, a
// missing bound, or a bound that carries an operator fails closed.
func hyphenRangeContains(fields []string, observed string, compare versionCompareFunc) (bool, bool, int) {
	if len(fields) < 3 || fields[1] != "-" || !fullVersionBound(fields[0]) || !fullVersionBound(fields[2]) {
		return false, false, 0
	}
	lower, validLower := compare(observed, fields[0])
	upper, validUpper := compare(observed, fields[2])
	if !validLower || !validUpper {
		return false, false, 0
	}
	return lower >= 0 && upper <= 0, true, 3
}

// fullVersionBound reports whether token is a bare version with a major, minor,
// and patch part, with no operator and no wildcard.
func fullVersionBound(token string) bool {
	core := token
	if i := strings.IndexAny(core, "-+"); i >= 0 {
		core = core[:i]
	}
	core = strings.TrimPrefix(core, "v")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" || strings.Trim(part, "0123456789") != "" {
			return false
		}
	}
	return true
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
	for _, operator := range []string{">=", "<=", "==", "!=", "~=", ">", "<", "="} {
		if strings.HasPrefix(token, operator) {
			return operator, strings.TrimSpace(strings.TrimPrefix(token, operator))
		}
	}
	if strings.HasPrefix(token, "^") || strings.HasPrefix(token, "~") {
		return token[:1], ""
	}
	return "", strings.TrimSpace(token)
}
