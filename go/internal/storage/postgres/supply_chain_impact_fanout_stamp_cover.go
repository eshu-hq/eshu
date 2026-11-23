// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"encoding/json"
	"fmt"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// The #10972 cover rule: does a queued holder row's next pass already derive a
// fan-out's change? The queued-holder prefilter drops a holder only when every
// matched row is covered, and the reopen widens the stamp of a queued or
// flagged row that is not. Both read these fragments, so they cannot disagree.
//
// The fragments mirror the Go pass rules in reducer/supplychain/core:
// fanOutOneHop (stamped, claim attempt at most 1, not owed) and
// scopedPassCVEIDs (a bounded stamp set scopes the pass; no set, an empty set,
// or the sentinel runs it full). Every disagreement falls on the side of
// keeping a holder: a cves array with no usable string id runs full in Go but
// is not "unbounded" here, so it is kept and widened. Each fragment is written
// so it can never evaluate to NULL, because bool_and ignores NULL rows and a
// NULL arm would drop a holder whose other row is covered. jsonb_array_length
// is only reached inside a CASE branch that has already proved an array.

// fanOutStampSQL is the stored stamp of the row the statement reads.
const fanOutStampSQL = `work.payload->'` + supplychaincore.PayloadKeyFanOutOrigin + `'`

// fanOutStampUnboundedSQL is TRUE when the stamp expression names no bounded
// set: no stamp object (missing, JSON null, or another JSON type), no cves
// array, an empty array, or the sentinel. A pass with such a stamp derives
// every CVE, so it covers any change. Never NULL.
func fanOutStampUnboundedSQL(stamp string) string {
	cves := stamp + `->'` + supplychaincore.FanOutStampCVEKey + `'`
	return `(CASE
            WHEN jsonb_typeof(` + stamp + `) IS DISTINCT FROM 'object' THEN TRUE
            WHEN jsonb_typeof(` + cves + `) IS DISTINCT FROM 'array' THEN TRUE
            WHEN jsonb_array_length(` + cves + `) = 0 THEN TRUE
            ELSE ` + cves + ` @> '["` + supplychaincore.FanOutStampSentinel + `"]'::jsonb
        END)`
}

// fanOutStampCoversSQL is TRUE when the stamp expression's set already names
// the incoming change: the stamp is unbounded, or the change is bounded
// (boundedArg) and the stored set contains every id of cvesArg. An unbounded
// change is covered only by an unbounded stamp. Never NULL.
func fanOutStampCoversSQL(stamp, cvesArg, boundedArg string) string {
	cves := stamp + `->'` + supplychaincore.FanOutStampCVEKey + `'`
	return `COALESCE(` + fanOutStampUnboundedSQL(stamp) + `
            OR (` + boundedArg + ` AND ` + cves + ` @> ` + cvesArg + `), FALSE)`
}

// fanOutQueuedRowCoversSQL is TRUE for a pending or retrying row whose next
// pass covers the incoming change: it runs full because the item owes a
// fan-out, because its claim attempt will exceed 1 (the claim's own
// arithmetic: a retrying row in a non-counting class keeps its count, from
// reducerNonCountingFailureClassPredicateSQL, the predicate the claim uses),
// or because its stamp is unbounded; or its scoped pass already names every
// id of the change. FALSE for every other status. Never NULL.
func fanOutQueuedRowCoversSQL(cvesArg, boundedArg string) string {
	return `COALESCE(work.status IN ('pending', 'retrying') AND (
            (work.payload->'` + supplychaincore.PayloadKeyFanOutOwed + `' = 'true'::jsonb) IS TRUE
         OR work.attempt_count + CASE WHEN work.status = 'retrying' AND ` +
		reducerNonCountingFailureClassPredicateSQL("work") + ` THEN 0 ELSE 1 END > 1
         OR ` + fanOutStampCoversSQL(fanOutStampSQL, cvesArg, boundedArg) + `
        ), FALSE)`
}

// fanOutChangeArgs returns the two bind values the cover rule reads for a
// fan-out's stamp set: the ids as a jsonb array and whether they are bounded.
// stampOut is already canonical (CapFanOutStampCVEIDs). An empty set or the
// sentinel means the change is unbounded, so only an unbounded stamp or a
// full next pass covers it, and the array is bound as [] so no arm reads NULL.
func fanOutChangeArgs(stampOut []string) (string, bool, error) {
	bounded := len(stampOut) > 0
	for _, id := range stampOut {
		if id == supplychaincore.FanOutStampSentinel {
			bounded = false
		}
	}
	if !bounded {
		return "[]", false, nil
	}
	encoded, err := json.Marshal(stampOut)
	if err != nil {
		return "", false, fmt.Errorf("marshal supply chain impact fan-out change set: %w", err)
	}
	return string(encoded), true, nil
}
