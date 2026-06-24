// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strconv"
	"strings"
	"unicode"
)

func evaluateRPMVersionMatch(
	observed string,
	fixedVersion string,
	pkgs []supplyChainAffectedPackage,
) supplyChainVersionMatchDecision {
	if !validRPMEVR(observed) {
		return malformedInstalledVersionDecision()
	}
	for _, pkg := range pkgs {
		for _, affected := range pkg.affectedVersions {
			cmp, ok := compareRPMEVR(observed, affected)
			if ok && cmp == 0 {
				return affectedVersionDecision(supplyChainVersionReasonRPMExactAffected)
			}
			if !ok {
				return malformedVersionDecision()
			}
		}
	}
	if fixedVersion != "" {
		cmp, ok := compareRPMEVR(observed, fixedVersion)
		if !ok {
			return malformedVersionDecision()
		}
		if cmp >= 0 {
			return knownFixedDecision(supplyChainVersionReasonRPMKnownFixed)
		}
	}
	return possiblyAffectedDecision(supplyChainVersionReasonNoAffectedMatch, nil)
}

func validRPMEVR(value string) bool {
	evr := strings.TrimSpace(value)
	if evr == "" || strings.ContainsAny(evr, " \t\r\n") {
		return false
	}
	parsed := parseRPMEVR(evr)
	return parsed.version != ""
}

type rpmEVR struct {
	epoch   string
	version string
	release string
}

func parseRPMEVR(raw string) rpmEVR {
	rest := strings.TrimSpace(raw)
	epoch := ""
	if before, after, ok := strings.Cut(rest, ":"); ok && isDecimalString(before) {
		epoch = before
		rest = after
	}
	version := rest
	release := ""
	if before, after, ok := strings.Cut(rest, "-"); ok {
		version = before
		release = after
	}
	return rpmEVR{epoch: epoch, version: version, release: release}
}

func compareRPMEVR(a string, b string) (int, bool) {
	left := parseRPMEVR(a)
	right := parseRPMEVR(b)
	if left.version == "" || right.version == "" {
		return 0, false
	}
	if cmp := compareRPMEpoch(left.epoch, right.epoch); cmp != 0 {
		return cmp, true
	}
	if cmp := compareRPMSegments(left.version, right.version); cmp != 0 {
		return cmp, true
	}
	return compareRPMSegments(left.release, right.release), true
}

func compareRPMEpoch(a string, b string) int {
	ai := parseRPMEpoch(a)
	bi := parseRPMEpoch(b)
	switch {
	case ai < bi:
		return -1
	case ai > bi:
		return 1
	default:
		return 0
	}
}

func parseRPMEpoch(value string) int64 {
	if value == "" {
		return 0
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0
	}
	return parsed
}

func compareRPMSegments(a string, b string) int {
	left := rpmSegmentScanner{value: a}
	right := rpmSegmentScanner{value: b}
	for {
		ls := left.next()
		rs := right.next()
		if ls.value == "" && rs.value == "" {
			return 0
		}
		if ls.value == "" {
			return -1
		}
		if rs.value == "" {
			return 1
		}
		if ls.numeric != rs.numeric {
			if ls.numeric {
				return 1
			}
			return -1
		}
		if cmp := compareRPMSegmentValue(ls, rs); cmp != 0 {
			return cmp
		}
	}
}

type rpmSegment struct {
	value   string
	numeric bool
}

type rpmSegmentScanner struct {
	value string
	pos   int
}

func (s *rpmSegmentScanner) next() rpmSegment {
	for s.pos < len(s.value) && !isRPMSegmentByte(s.value[s.pos]) {
		s.pos++
	}
	if s.pos >= len(s.value) {
		return rpmSegment{}
	}
	start := s.pos
	numeric := unicode.IsDigit(rune(s.value[s.pos]))
	for s.pos < len(s.value) {
		r := rune(s.value[s.pos])
		if numeric != unicode.IsDigit(r) || !isRPMSegmentByte(s.value[s.pos]) {
			break
		}
		s.pos++
	}
	return rpmSegment{value: s.value[start:s.pos], numeric: numeric}
}

func compareRPMSegmentValue(a rpmSegment, b rpmSegment) int {
	left := a.value
	right := b.value
	if a.numeric {
		left = strings.TrimLeft(left, "0")
		right = strings.TrimLeft(right, "0")
		if len(left) != len(right) {
			if len(left) < len(right) {
				return -1
			}
			return 1
		}
	}
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func isRPMSegmentByte(value byte) bool {
	r := rune(value)
	return unicode.IsDigit(r) || unicode.IsLetter(r)
}

func isDecimalString(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}
