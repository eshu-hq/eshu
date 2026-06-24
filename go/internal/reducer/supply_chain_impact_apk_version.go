// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strconv"
	"strings"
	"unicode"
)

func validAPKVersion(value string) bool {
	_, ok := parseAPKVersion(value)
	return ok
}

type apkVersion struct {
	main     string
	suffixes []apkSuffix
	release  int64
}

type apkSuffix struct {
	name   string
	number int64
}

func parseAPKVersion(raw string) (apkVersion, bool) {
	value := strings.TrimSpace(raw)
	if value == "" || strings.ContainsAny(value, " \t\r\n") {
		return apkVersion{}, false
	}
	release := int64(0)
	base := value
	if before, after, ok := strings.Cut(value, "-r"); ok {
		if after == "" || !isDecimalString(after) {
			return apkVersion{}, false
		}
		parsed, err := strconv.ParseInt(after, 10, 64)
		if err != nil {
			return apkVersion{}, false
		}
		base = before
		release = parsed
	} else if strings.Contains(value, "-") {
		return apkVersion{}, false
	}
	parts := strings.Split(base, "_")
	if len(parts) == 0 || parts[0] == "" || !apkMainVersionValid(parts[0]) {
		return apkVersion{}, false
	}
	suffixes := make([]apkSuffix, 0, len(parts)-1)
	for _, part := range parts[1:] {
		suffix, ok := parseAPKSuffix(part)
		if !ok {
			return apkVersion{}, false
		}
		suffixes = append(suffixes, suffix)
	}
	return apkVersion{main: parts[0], suffixes: suffixes, release: release}, true
}

func compareAPKVersion(a string, b string) (int, bool) {
	left, ok := parseAPKVersion(a)
	if !ok {
		return 0, false
	}
	right, ok := parseAPKVersion(b)
	if !ok {
		return 0, false
	}
	if cmp := compareRPMSegments(left.main, right.main); cmp != 0 {
		return cmp, true
	}
	if cmp := compareAPKSuffixes(left.suffixes, right.suffixes); cmp != 0 {
		return cmp, true
	}
	switch {
	case left.release < right.release:
		return -1, true
	case left.release > right.release:
		return 1, true
	default:
		return 0, true
	}
}

func compareAPKSuffixes(left []apkSuffix, right []apkSuffix) int {
	maxLen := len(left)
	if len(right) > maxLen {
		maxLen = len(right)
	}
	for i := 0; i < maxLen; i++ {
		l := apkSuffix{name: ""}
		r := apkSuffix{name: ""}
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		lr := apkSuffixRank(l.name)
		rr := apkSuffixRank(r.name)
		if lr != rr {
			if lr < rr {
				return -1
			}
			return 1
		}
		if l.number != r.number {
			if l.number < r.number {
				return -1
			}
			return 1
		}
	}
	return 0
}

func parseAPKSuffix(value string) (apkSuffix, bool) {
	if value == "" {
		return apkSuffix{}, false
	}
	i := 0
	for i < len(value) && isASCIIAlpha(value[i]) {
		i++
	}
	if i == 0 {
		return apkSuffix{}, false
	}
	name := value[:i]
	if apkSuffixRank(name) == apkUnknownSuffixRank {
		return apkSuffix{}, false
	}
	number := int64(0)
	if i < len(value) {
		raw := value[i:]
		if !isDecimalString(raw) {
			return apkSuffix{}, false
		}
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return apkSuffix{}, false
		}
		number = parsed
	}
	return apkSuffix{name: name, number: number}, true
}

const apkUnknownSuffixRank = 99

func apkSuffixRank(value string) int {
	switch value {
	case "alpha":
		return -40
	case "beta":
		return -30
	case "pre":
		return -20
	case "rc":
		return -10
	case "":
		return 0
	case "cvs":
		return 10
	case "svn":
		return 20
	case "git":
		return 30
	case "hg":
		return 40
	case "p":
		return 50
	default:
		return apkUnknownSuffixRank
	}
}

func apkMainVersionValid(value string) bool {
	if value == "" || !unicode.IsDigit(rune(value[0])) {
		return false
	}
	for _, r := range value {
		switch {
		case unicode.IsDigit(r), unicode.IsLetter(r):
			continue
		case r == '.':
			continue
		default:
			return false
		}
	}
	return true
}
