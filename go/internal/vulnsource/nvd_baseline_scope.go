// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vulnsource

import (
	"net/url"
	"strconv"
	"strings"
	"time"
)

// nvdBaselineScopePrefix is the coordinator-minted per-chunk NVD baseline
// scope family (eshu-hq/eshu#10728): one scope per baseline chunk, carrying
// the baseline run id, the year file under replay, and the resume stamp the
// chunk starts from:
//
//	vuln-intel://nvd/_baseline/<run-id>/<year>/<start_unix_us>
//
// Same (run, year, start) mints the same scope id on every run, so a
// refusal, skip, or crash re-plans the same scope and the commit path
// fences it as a same-scope successor instead of superseding it. The chunk
// end is satellite-determined at the per-claim fact/byte caps, so it is
// not part of the identity: the committed checkpoint records the end the
// next plan starts from. The family sits outside the nvd/_window/ prefix,
// so the window cursor arming check (IsNVDWindowScope) returns false for
// every baseline scope by construction, and the cursor CAS never sees a
// baseline candidate.
const nvdBaselineScopePrefix = scopeIDPrefix + SourceNVD + "/_baseline/"

// NVDScopePrefix is the prefix every NVD scope id carries, the window and the
// baseline families alike (vuln-intel://nvd/...). The NVD baseline trip line
// uses it to recognize the scope rows an NVD pointer flip appends to the
// winners dirty log, which carry no advisory id (eshu-hq/eshu#10767).
const NVDScopePrefix = scopeIDPrefix + SourceNVD + "/"

// nvdBaselineFirstYear is the first NVD year feed: CVE ids below 2002 have
// no year file, so the planner can never mint a chunk for them.
const nvdBaselineFirstYear = 2002

// isNVDBaselineRunID reports whether runID is safe to mint into a scope id:
// non-blank, URL-segment-safe, and free of query or path separators.
func isNVDBaselineRunID(runID string) bool {
	if runID == "" || len(runID) > 64 {
		return false
	}
	for _, r := range runID {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_':
		default:
			return false
		}
	}
	return true
}

// NVDBaselineScopeID mints the deterministic scope id of one NVD baseline
// chunk: the resume stamp a run replays a year file from. It returns "" for
// degenerate input (a blank or unsafe run id, a pre-feed year, or a zero
// start), so the coordinator can never plan a degenerate baseline chunk.
// The start normalizes through NormalizeNVDWindowTime (UTC millisecond, the
// NVD wire form), so the planned, fetched, and stored values agree.
func NVDBaselineScopeID(runID string, year int, start time.Time) string {
	if !isNVDBaselineRunID(runID) {
		return ""
	}
	if year < nvdBaselineFirstYear {
		return ""
	}
	start = NormalizeNVDWindowTime(start)
	if start.IsZero() {
		return ""
	}
	return nvdBaselineScopePrefix +
		runID + "/" +
		strconv.Itoa(year) + "/" +
		strconv.FormatInt(start.UnixMicro(), 10)
}

// ParseNVDBaselineScope validates a coordinator-minted NVD baseline scope
// id and returns its run id, year, and resume stamp. It reports false for
// every other scope id, including window scopes, static NVD scopes, and
// GHSA scopes: a wrong family is a fail-closed shape error, never a
// baseline plan.
func ParseNVDBaselineScope(scopeID string) (runID string, year int, start time.Time, ok bool) {
	parsed, err := url.Parse(scopeID)
	if err != nil || parsed.Scheme != "vuln-intel" || parsed.Host != SourceNVD {
		return "", 0, time.Time{}, false
	}
	if parsed.RawQuery != "" {
		return "", 0, time.Time{}, false
	}
	rest, found := strings.CutPrefix(parsed.EscapedPath(), "/_baseline/")
	if !found {
		return "", 0, time.Time{}, false
	}
	segments := strings.Split(rest, "/")
	if len(segments) != 3 {
		return "", 0, time.Time{}, false
	}
	runID, yearRaw, startRaw := segments[0], segments[1], segments[2]
	if !isNVDBaselineRunID(runID) {
		return "", 0, time.Time{}, false
	}
	year, err = strconv.Atoi(strings.TrimSpace(yearRaw))
	if err != nil || year < nvdBaselineFirstYear {
		return "", 0, time.Time{}, false
	}
	startMicros, err := strconv.ParseInt(strings.TrimSpace(startRaw), 10, 64)
	if err != nil || startMicros <= 0 {
		return "", 0, time.Time{}, false
	}
	return runID, year, time.UnixMicro(startMicros).UTC(), true
}

// IsNVDBaselineScope reports whether scopeID has the NVD baseline scope
// shape. The prefix check keeps the hot non-baseline path to one string
// comparison; only a candidate baseline scope pays for the full parse.
func IsNVDBaselineScope(scopeID string) bool {
	if !strings.HasPrefix(scopeID, nvdBaselineScopePrefix) {
		return false
	}
	_, _, _, ok := ParseNVDBaselineScope(scopeID)
	return ok
}
