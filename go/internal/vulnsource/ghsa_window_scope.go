// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vulnsource

import (
	"net/url"
	"strconv"
	"strings"
	"time"
)

// GHSA advisory types admitted by the window grammar (eshu-hq/eshu#10037).
// Unreviewed GitHub advisories are out of scope and have no scope id.
const (
	// GHSAAdvisoryTypeReviewed names the reviewed GitHub advisory stream.
	GHSAAdvisoryTypeReviewed = "reviewed"
	// GHSAAdvisoryTypeMalware names the malware GitHub advisory stream.
	GHSAAdvisoryTypeMalware = "malware"
)

// ghsaWindowScopeInfix separates the advisory-type segment from the window
// bounds in a coordinator-minted per-window GHSA scope id:
//
//	vuln-intel://ghsa/<reviewed|malware>/_window/<start_unix_us>-<end_unix_us>
//
// The grammar is core-owned; the satellite's copy is pinned by the shared
// golden vectors in testdata/ghsa_window_golden.json. Same bounds mint the
// same scope id on every run, and a window scope is write-once, so no GHSA
// generation ever supersedes another.
const ghsaWindowScopeInfix = "/_window/"

// IsGHSAAdvisoryType reports whether value is one of the two admitted GHSA
// advisory types, matched exactly and case-sensitively.
func IsGHSAAdvisoryType(value string) bool {
	return value == GHSAAdvisoryTypeReviewed || value == GHSAAdvisoryTypeMalware
}

// NormalizeGHSAWindowTime renders a window bound the way the GitHub
// modified feed reports it: UTC truncated to the second, so the planned,
// fetched, and stored values are bit-identical. GitHub's updated_at has
// second granularity and the modified range is inclusive at both ends.
func NormalizeGHSAWindowTime(t time.Time) time.Time {
	return t.UTC().Truncate(time.Second)
}

// GHSAStreamScopeIDForType returns the anchor (stream) scope id of one
// advisory type, or "" for a type outside the grammar.
func GHSAStreamScopeIDForType(advisoryType string) string {
	if !IsGHSAAdvisoryType(advisoryType) {
		return ""
	}
	return scopeIDPrefix + SourceGHSA + "/" + advisoryType
}

// GHSAWindowScopeID mints the deterministic scope id of one rolling GHSA
// window. It returns "" for an unadmitted advisory type or degenerate
// bounds (a zero start or end, or an end at or before the start after
// normalization), so the coordinator can never plan a degenerate window.
func GHSAWindowScopeID(advisoryType string, start, end time.Time) string {
	stream := GHSAStreamScopeIDForType(advisoryType)
	if stream == "" {
		return ""
	}
	start = NormalizeGHSAWindowTime(start)
	end = NormalizeGHSAWindowTime(end)
	if start.IsZero() || end.IsZero() || !end.After(start) {
		return ""
	}
	return stream + ghsaWindowScopeInfix +
		strconv.FormatInt(start.UnixMicro(), 10) +
		"-" +
		strconv.FormatInt(end.UnixMicro(), 10)
}

// ParseGHSAWindowScope validates a coordinator-minted per-window GHSA
// scope id and returns its advisory type and exact bounds. It reports
// false for every other scope id, including the per-type anchors: digits
// only, no query, no extra segment, end strictly after a positive start.
func ParseGHSAWindowScope(scopeID string) (advisoryType string, start, end time.Time, ok bool) {
	parsed, err := url.Parse(scopeID)
	if err != nil || parsed.Scheme != "vuln-intel" || parsed.Host != SourceGHSA {
		return "", time.Time{}, time.Time{}, false
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", time.Time{}, time.Time{}, false
	}
	rest, found := strings.CutPrefix(parsed.EscapedPath(), "/")
	if !found {
		return "", time.Time{}, time.Time{}, false
	}
	advisoryType, tail, found := strings.Cut(rest, ghsaWindowScopeInfix)
	if !found || !IsGHSAAdvisoryType(advisoryType) {
		return "", time.Time{}, time.Time{}, false
	}
	bounds, err := url.PathUnescape(tail)
	if err != nil || bounds != tail || strings.Contains(bounds, "/") {
		return "", time.Time{}, time.Time{}, false
	}
	before, after, found := strings.Cut(bounds, "-")
	if !found {
		return "", time.Time{}, time.Time{}, false
	}
	startMicros, ok := parseWindowBoundDigits(before)
	if !ok || startMicros <= 0 {
		return "", time.Time{}, time.Time{}, false
	}
	endMicros, ok := parseWindowBoundDigits(after)
	if !ok || endMicros <= startMicros {
		return "", time.Time{}, time.Time{}, false
	}
	return advisoryType, time.UnixMicro(startMicros).UTC(), time.UnixMicro(endMicros).UTC(), true
}

// parseWindowBoundDigits parses one window bound, accepting ASCII digits
// only: no sign, no padding, no spaces. The grammar is refuse-everything-
// else, so a bound a lenient parser would accept still fails here.
func parseWindowBoundDigits(value string) (int64, bool) {
	if value == "" {
		return 0, false
	}
	for i := 0; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return 0, false
		}
	}
	micros, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, false
	}
	return micros, true
}

// IsGHSAWindowScope reports whether scopeID has the per-window GHSA scope
// shape. The infix check keeps the hot non-window path to one string
// search; only a candidate window scope pays for the full parse.
func IsGHSAWindowScope(scopeID string) bool {
	if !strings.Contains(scopeID, ghsaWindowScopeInfix) {
		return false
	}
	_, _, _, ok := ParseGHSAWindowScope(scopeID)
	return ok
}

// GHSAStreamScopeID maps a per-window GHSA scope id to its stream (anchor)
// scope id, the key source-state rows are recorded under so two windows of
// one stream share one row. ok is false for every non-window scope id,
// including the anchor itself.
func GHSAStreamScopeID(scopeID string) (string, bool) {
	advisoryType, _, _, ok := ParseGHSAWindowScope(scopeID)
	if !ok {
		return "", false
	}
	return GHSAStreamScopeIDForType(advisoryType), true
}

// GHSAWindowBounds returns the bounds a per-window GHSA scope id encodes,
// for the stream source-state row's window columns. ok is false for every
// non-window scope id.
func GHSAWindowBounds(scopeID string) (start, end time.Time, ok bool) {
	_, start, end, ok = ParseGHSAWindowScope(scopeID)
	return start, end, ok
}
