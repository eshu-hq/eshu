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
//	vuln-intel://nvd/_baseline/<run-id>/<year>/<start_unix_us>/<from_cve_id>
//
// The resume position is the pair (start_unix_us, from_cve_id), an
// inclusive lower bound in (lastModified, CVE ID) order, so a chunk can
// resume inside one instant and a same-stamp group larger than a chunk
// drains across chunks (eshu-hq/eshu#10809). One position has exactly one
// scope id: an empty from_cve_id is never written as a trailing segment, so
// the three-segment form is the position whose id is empty and the
// four-segment form carries a canonical id (see IsCanonicalNVDBaselineCVEID).
// Same (run, year, position) mints the same scope id on every run, so a
// refusal, skip, or crash re-plans the same scope and the commit path
// fences it as a same-scope successor instead of superseding it. The chunk
// end is satellite-determined at the chunk budget, so it is not part of the
// identity: the committed checkpoint records the position the next plan
// starts from. The family sits outside the nvd/_window/ prefix,
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

// nvdBaselineCVEIDMaxLength bounds a canonical from_cve_id segment.
const nvdBaselineCVEIDMaxLength = 64

// IsCanonicalNVDBaselineCVEID reports whether id is the canonical form of a
// baseline resume CVE ID: already trimmed and upper-cased, one to 64
// characters of [A-Z0-9-]. The empty id is not canonical here; it is the
// absent trailing segment. Core, the checkpoint writer, and the collector
// share this one rule, so two spellings of one position cannot mint two
// scope ids.
func IsCanonicalNVDBaselineCVEID(id string) bool {
	if id == "" || len(id) > nvdBaselineCVEIDMaxLength {
		return false
	}
	for i := 0; i < len(id); i++ {
		switch c := id[i]; {
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		case c == '-':
		default:
			return false
		}
	}
	return true
}

// NVDBaselineScopeID mints the deterministic scope id of one NVD baseline
// chunk: the resume position (stamp and CVE ID) a run replays a year file
// from. fromCVEID is empty for a position at the start of an instant, and a
// canonical id otherwise. It returns "" for degenerate input (a blank or
// unsafe run id, a pre-feed year, a zero start, or a non-canonical id), so
// the coordinator can never plan a degenerate baseline chunk. The start
// normalizes through NormalizeNVDWindowTime (UTC millisecond, the NVD wire
// form), so the planned, fetched, and stored values agree.
func NVDBaselineScopeID(runID string, year int, start time.Time, fromCVEID string) string {
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
	id := nvdBaselineScopePrefix +
		runID + "/" +
		strconv.Itoa(year) + "/" +
		strconv.FormatInt(start.UnixMicro(), 10)
	if fromCVEID == "" {
		return id
	}
	if !IsCanonicalNVDBaselineCVEID(fromCVEID) {
		return ""
	}
	return id + "/" + fromCVEID
}

// ParseNVDBaselineScope validates a coordinator-minted NVD baseline scope
// id and returns its run id, year, resume stamp, and resume CVE ID (empty
// for the three-segment form). It reports false for every other scope id,
// including window scopes, static NVD scopes, and GHSA scopes: a wrong
// family is a fail-closed shape error, never a baseline plan. A fourth
// segment must be a canonical CVE ID: a lowercase, padded, empty, or
// over-long id, a fifth segment, a query, or a fragment all reject, and so does
// any non-canonical year or start spelling (signed, zero-padded, space-padded,
// or a start that is not a whole UTC millisecond), so one position never has
// two scope ids.
func ParseNVDBaselineScope(scopeID string) (runID string, year int, start time.Time, fromCVEID string, ok bool) {
	// The literal prefix check rejects an upper-case scheme or host that
	// url.Parse would fold, so the parser accepts exactly the spelling the
	// mint writes and IsNVDBaselineScope agrees with it.
	if !strings.HasPrefix(scopeID, nvdBaselineScopePrefix) {
		return "", 0, time.Time{}, "", false
	}
	parsed, err := url.Parse(scopeID)
	if err != nil || parsed.Scheme != "vuln-intel" || parsed.Host != SourceNVD {
		return "", 0, time.Time{}, "", false
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" || strings.Contains(scopeID, "#") {
		return "", 0, time.Time{}, "", false
	}
	rest, found := strings.CutPrefix(parsed.EscapedPath(), "/_baseline/")
	if !found {
		return "", 0, time.Time{}, "", false
	}
	segments := strings.Split(rest, "/")
	if len(segments) != 3 && len(segments) != 4 {
		return "", 0, time.Time{}, "", false
	}
	runID, yearRaw, startRaw := segments[0], segments[1], segments[2]
	if !isNVDBaselineRunID(runID) {
		return "", 0, time.Time{}, "", false
	}
	// Year and start parse only in the spelling the mint writes: no sign, no
	// padding, a four-digit year, and a whole UTC millisecond start.
	year, err = strconv.Atoi(yearRaw)
	if err != nil || len(yearRaw) != 4 || strconv.Itoa(year) != yearRaw || year < nvdBaselineFirstYear {
		return "", 0, time.Time{}, "", false
	}
	startMicros, err := strconv.ParseInt(startRaw, 10, 64)
	if err != nil || startMicros <= 0 || strconv.FormatInt(startMicros, 10) != startRaw || startMicros%1000 != 0 {
		return "", 0, time.Time{}, "", false
	}
	if len(segments) == 4 {
		fromCVEID = segments[3]
		if !IsCanonicalNVDBaselineCVEID(fromCVEID) {
			return "", 0, time.Time{}, "", false
		}
	}
	return runID, year, time.UnixMicro(startMicros).UTC(), fromCVEID, true
}

// IsNVDBaselineScope reports whether scopeID has the NVD baseline scope
// shape. The prefix check keeps the hot non-baseline path to one string
// comparison; only a candidate baseline scope pays for the full parse.
func IsNVDBaselineScope(scopeID string) bool {
	if !strings.HasPrefix(scopeID, nvdBaselineScopePrefix) {
		return false
	}
	_, _, _, _, ok := ParseNVDBaselineScope(scopeID)
	return ok
}
