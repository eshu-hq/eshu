// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package group

import (
	"strings"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// PayloadKeyFirstUnderlyingDataAt is the optional intent/queue payload key
// carrying the row's first-underlying-data time (Decision-3 Q1 option a,
// eshu-hq/eshu#11080). The projection producer stamps the touching
// supply_chain_impact pass's cycle start (its enqueue when the cycle start
// is zero) as an RFC3339Nano string; rows enqueued before the wire carry
// no field and the handler falls back to the post-claim deferral clock.
// The key is payload-scoped, so old rows, the work-item id, the conflict
// key, and the claim path need no migration: the claim restores the whole
// payload onto the intent.
const PayloadKeyFirstUnderlyingDataAt = "first_underlying_data_at"

// Closed advisory-origin values for the per-verdict origin attribution on
// watermark_waits_total (#11080). The verdict fires before any winner row
// is read, so the origin derives from the group id with the #11078
// dirty-log proxy: a CVE-class id names NVD data, a GHSA-class id names
// GHSA data, anything else (distro advisories, blank) is other. The set
// stays closed: raw advisory ids never become a label.
const (
	// AdvisoryOriginNVD attributes the verdict to NVD-side (CVE) inflow.
	AdvisoryOriginNVD = "nvd"
	// AdvisoryOriginGHSA attributes the verdict to GHSA-side inflow.
	AdvisoryOriginGHSA = "ghsa"
	// AdvisoryOriginOther attributes the verdict to neither leg.
	AdvisoryOriginOther = "other"
)

// firstUnderlyingDataAt reads the Decision-3 wire field off the claimed
// intent: the producer-stamped first-underlying-data time. It accepts the
// RFC3339Nano string the queue JSON round-trip delivers and a time.Time a
// hand-built intent may carry. It reports false when the field is absent
// (a row enqueued before the wire), unparsable, zero, or future-dated, so
// the handler falls back to the post-claim deferral clock on every input
// the producer cannot vouch for.
func firstUnderlyingDataAt(intent reducercontract.Intent, now time.Time) (time.Time, bool) {
	raw, ok := intent.Payload[PayloadKeyFirstUnderlyingDataAt]
	if !ok {
		return time.Time{}, false
	}
	var stamped time.Time
	switch value := raw.(type) {
	case string:
		if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
			stamped = parsed
		} else if parsed, err := time.Parse(time.RFC3339, value); err == nil {
			stamped = parsed
		} else {
			return time.Time{}, false
		}
	case time.Time:
		stamped = value
	default:
		return time.Time{}, false
	}
	if stamped.IsZero() || stamped.After(now) {
		return time.Time{}, false
	}
	return stamped.UTC(), true
}

// advisoryOrigin classifies one advisory group id into the closed
// per-verdict origin: CVE-class ids (matching canonicalCVEKey) are NVD
// data, GHSA-prefixed ids are GHSA data, everything else is other.
func advisoryOrigin(groupID string) string {
	if _, ok := canonicalCVEKey(groupID); ok {
		return AdvisoryOriginNVD
	}
	if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(groupID)), "GHSA-") {
		return AdvisoryOriginGHSA
	}
	return AdvisoryOriginOther
}

// verdictOrigin attributes one watermark-wait verdict to a closed origin
// from the intent's group ids. Production intents carry exactly one; a
// hand-built multi-key intent attributes to the first in sort order, and
// an empty set attributes to other. The origin is per-verdict input only:
// it never becomes an entity key, a conflict key, or a bound.
func verdictOrigin(groupIDs []string) string {
	if len(groupIDs) == 0 {
		return AdvisoryOriginOther
	}
	return advisoryOrigin(groupIDs[0])
}
