// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import "slices"

// Closed NVD rolling-window planner codes (eshu-hq/eshu#9877, item 2.2,
// arbiter decision 11). They ride span attributes and validator errors, never
// metric labels: labels stay source-only so a window value cannot explode
// cardinality.
const (
	// NVDWindowEndAfter marks an end past the allowable head: the clamp plus
	// a skew signal for a planner-generated window, and a hard reject only
	// for an operator-configured static window at target validation.
	NVDWindowEndAfter = "window_end_after"
	// NVDWindowExceedsMaxRange marks a window wider than the admission cap:
	// the planner chunks it, and a static config carrying one is rejected.
	NVDWindowExceedsMaxRange = "window_exceeds_max_range"
	// NVDWindowOlderThanMinimum marks a cursor more than the cap behind the
	// head. There is no minimum: the claim chunks forward sequentially, and
	// a reject there would strand catch-up permanently.
	NVDWindowOlderThanMinimum = "window_older_than_minimum"
	// NVDWindowOther counts a record with a reason outside this set.
	NVDWindowOther = "other"
)

// nvdWindowCodes is the closed, ordered reason set.
var nvdWindowCodes = [...]string{
	NVDWindowEndAfter,
	NVDWindowExceedsMaxRange,
	NVDWindowOlderThanMinimum,
	NVDWindowOther,
}

// NVDWindowCodes returns the closed, ordered set of NVD window codes.
func NVDWindowCodes() []string {
	return slices.Clone(nvdWindowCodes[:])
}

// NVDWindowCode maps code onto the closed set.
func NVDWindowCode(code string) string {
	if slices.Contains(nvdWindowCodes[:], code) {
		return code
	}
	return NVDWindowOther
}

// Closed NVD transports (eshu-hq/eshu#10716). The satellite stamps the
// transport that served a window into its source snapshot, and the host
// folds it onto this set before it becomes a metric label: api is the
// rate-limited NVD API walk, feed is the NVD 2.0 JSON data feed path, and
// unknown is everything else, including a satellite that stamps nothing.
const (
	NVDTransportAPI     = "api"
	NVDTransportFeed    = "feed"
	NVDTransportUnknown = "unknown"
)

// nvdTransports is the closed, ordered transport set.
var nvdTransports = [...]string{
	NVDTransportAPI,
	NVDTransportFeed,
	NVDTransportUnknown,
}

// NVDTransports returns the closed, ordered transport set.
func NVDTransports() []string {
	return slices.Clone(nvdTransports[:])
}

// NVDTransport maps a satellite-stamped transport onto the closed set.
// Matching is exact: a case variant or padded value is unknown, because the
// label must stay a bounded set, never a pass-through of satellite text.
func NVDTransport(value string) string {
	if slices.Contains(nvdTransports[:], value) {
		return value
	}
	return NVDTransportUnknown
}

// Closed NVD window claim outcomes, labeled with the transport on the window
// claim counter and duration histogram. committed means the generation is
// durable, refused means the committer refused it (write-once or successor
// guard), and the failed outcomes are the claim plane's retryable and
// terminal verdicts.
const (
	NVDWindowClaimCommitted = "committed"
	NVDWindowClaimRefused   = "refused"
	NVDWindowClaimRetryable = "failed_retryable"
	NVDWindowClaimTerminal  = "failed_terminal"
)

// Closed NVD window cursor advance outcomes. advanced means the commit
// transaction armed the cursor compare-and-advance, held means the facts
// committed and the satellite's verdict held the cursor.
const (
	NVDWindowAdvanceAdvanced = "advanced"
	NVDWindowAdvanceHeld     = "held"
)

// Closed NVD feed guard classes for the guard-trips counter. These are the
// satellite's selector classes that routed a window to the API walk because a
// feed guard tripped: a year file older than a record the Modified feed
// carries, a first lastModified group too large to commit whole, and a probe
// that failed before the feed could be trusted (the design names a timeout and
// a digest mismatch while only probing).
const (
	NVDFeedGuardYearStale     = "nvd_feed_year_stale"
	NVDFeedGuardGroupOversize = "nvd_feed_group_oversize"
	NVDFeedGuardProbeFailed   = "nvd_feed_probe_failed"
	NVDFeedGuardOther         = "other"
)

// NVDFeedFailureSHA256Mismatch is the retryable failure class the satellite
// names when the sha256 of a feed body does not match the digest in its meta
// file, once the feed path was selected. The host counts it on the sha256
// mismatch counter.
const NVDFeedFailureSHA256Mismatch = "nvd_feed_sha256_mismatch"

// NVDFeedGuardClass maps a guard class onto the closed set.
func NVDFeedGuardClass(class string) string {
	switch class {
	case NVDFeedGuardYearStale, NVDFeedGuardGroupOversize, NVDFeedGuardProbeFailed:
		return class
	default:
		return NVDFeedGuardOther
	}
}

// Closed NVD feed selector outcomes: which source served a rolling window
// claim and why the API ran instead of the seed tail. before_seed is a
// window older than the tail's low bound, covered_seed a window served from
// the tail overlaid by the Modified feed, beyond_seed a window past the
// tail's horizon, and seed_unavailable a tail that could not serve. The set
// is registered here so the transport instruments can select on it once the
// satellite stamps its choice; nothing emits it yet.
const (
	NVDFeedSelectorBeforeSeed  = "nvd_feed_before_seed"
	NVDFeedSelectorCoveredSeed = "nvd_feed_covered_seed"
	NVDFeedSelectorBeyondSeed  = "nvd_feed_beyond_seed"
	NVDFeedSelectorUnavailable = "nvd_feed_seed_unavailable"
	NVDFeedSelectorOther       = "other"
)

// nvdFeedSelectors is the closed, ordered selector set.
var nvdFeedSelectors = [...]string{
	NVDFeedSelectorBeforeSeed,
	NVDFeedSelectorCoveredSeed,
	NVDFeedSelectorBeyondSeed,
	NVDFeedSelectorUnavailable,
	NVDFeedSelectorOther,
}

// NVDWindowFeedSelectors returns the closed, ordered set of NVD feed
// selector outcomes.
func NVDWindowFeedSelectors() []string {
	return slices.Clone(nvdFeedSelectors[:])
}

// NVDWindowFeedSelector maps a satellite-stamped selector outcome onto the
// closed set. Matching is exact: anything outside the set reads other, so
// the label stays bounded whatever a satellite stamps.
func NVDWindowFeedSelector(value string) string {
	if slices.Contains(nvdFeedSelectors[:], value) {
		return value
	}
	return NVDFeedSelectorOther
}
