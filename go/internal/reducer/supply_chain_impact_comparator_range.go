// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

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
