// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vulnsource

import (
	"net/url"
	"strconv"
	"strings"
	"time"
)

// nvdWindowScopePrefix is the coordinator-minted per-window NVD scope family
// (eshu-hq/eshu#9971): one scope per rolling [start, end) modified window,
// with the UTC-millisecond bounds rendered as Unix microseconds. Same bounds
// mint the same scope id on every run, so a retry or re-plan of one window
// reuses its scope and the commit path fences it as a same-scope successor
// instead of superseding an older window. The micros match the coverage
// descriptor's ModifiedStartUnixUS/ModifiedEndUnixUS bit-for-bit, because
// both normalize through NormalizeNVDWindowTime.
const nvdWindowScopePrefix = scopeIDPrefix + SourceNVD + "/_window/"

// NormalizeNVDWindowTime renders a window time the way the NVD client sends it
// and the coverage descriptor persists it: UTC truncated to the millisecond, so
// the planned, sent, and stored values are bit-identical.
func NormalizeNVDWindowTime(t time.Time) time.Time {
	return t.UTC().Truncate(time.Millisecond)
}

// NVDWindowScopeID mints the deterministic scope id of one rolling NVD
// window. It returns "" for degenerate bounds (a zero start or end, or an
// end at or before the start), so the coordinator can never plan a
// degenerate window scope.
func NVDWindowScopeID(start, end time.Time) string {
	start = NormalizeNVDWindowTime(start)
	end = NormalizeNVDWindowTime(end)
	if start.IsZero() || end.IsZero() || !end.After(start) {
		return ""
	}
	return nvdWindowScopePrefix +
		strconv.FormatInt(start.UnixMicro(), 10) +
		"-" +
		strconv.FormatInt(end.UnixMicro(), 10)
}

// ParseNVDWindowScope validates a coordinator-minted per-window NVD scope id
// and returns its exact bounds. It reports false for every other scope id,
// including configured NVD targets and derived OSV and EPSS scopes.
func ParseNVDWindowScope(scopeID string) (start, end time.Time, ok bool) {
	parsed, err := url.Parse(scopeID)
	if err != nil || parsed.Scheme != "vuln-intel" || parsed.Host != SourceNVD {
		return time.Time{}, time.Time{}, false
	}
	if parsed.RawQuery != "" {
		return time.Time{}, time.Time{}, false
	}
	rest, found := strings.CutPrefix(parsed.EscapedPath(), "/_window/")
	if !found {
		return time.Time{}, time.Time{}, false
	}
	bounds, err := url.PathUnescape(rest)
	if err != nil {
		return time.Time{}, time.Time{}, false
	}
	before, after, found := strings.Cut(bounds, "-")
	if !found {
		return time.Time{}, time.Time{}, false
	}
	startMicros, err := strconv.ParseInt(strings.TrimSpace(before), 10, 64)
	if err != nil || startMicros <= 0 {
		return time.Time{}, time.Time{}, false
	}
	endMicros, err := strconv.ParseInt(strings.TrimSpace(after), 10, 64)
	if err != nil || endMicros <= startMicros {
		return time.Time{}, time.Time{}, false
	}
	return time.UnixMicro(startMicros).UTC(), time.UnixMicro(endMicros).UTC(), true
}

// IsNVDWindowScope reports whether scopeID has the per-window NVD scope
// shape.
func IsNVDWindowScope(scopeID string) bool {
	_, _, ok := ParseNVDWindowScope(scopeID)
	return ok
}
