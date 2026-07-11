// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// sourceOrderKeyTimestampLayout formats an envelope's ObservedAt as a
// fixed-width, UTC, nanosecond-precision timestamp. Every value produced by
// this layout has exactly the same length (unlike time.RFC3339Nano, which
// trims trailing zero fractional digits), so lexicographic string comparison
// of two sourceOrderKey values always agrees with chronological comparison of
// the ObservedAt values they were built from.
const sourceOrderKeyTimestampLayout = "2006-01-02T15:04:05.000000000Z"

// sourceOrderKeySeparator joins the fixed-width timestamp segment to the
// tie-break source_fact_id segment. Its exact value does not affect correctness
// (the timestamp segment's fixed width already guarantees two keys' timestamp
// segments are compared in full before either string reaches the separator),
// but a value outside the timestamp/fact-id alphabet keeps the two segments
// visually distinct.
const sourceOrderKeySeparator = "|"

// sourceOrderKeyField is the node-row map key every #5007 Stage 1 row builder
// stamps with sourceOrderKey's output, and the graphowner gate reads as
// row.source_order_key to resolve cross-scope ownership.
const sourceOrderKeyField = "source_order_key"

// sourceOrderKey computes the #5007 Stage 1 deterministic order key for one
// contributing fact: max (observed_at, source_fact_id), i.e. the
// lexicographically comparable concatenation of a fixed-width UTC timestamp and
// the fact's stable id. Two different facts about the same node uid almost never
// share an identical (observed_at, source_fact_id) pair (fact ids are unique per
// fact), so this key gives a total order over contributors: the owner ledger
// keeps a shared node's scope-derived properties on whichever contributor has
// the greatest sourceOrderKey, independent of commit order or worker count, and
// preferMaxSourceOrderKey applies the identical rule to within-scope
// duplicate-uid resolution during extraction. See
// docs/internal/design/5007-cross-scope-node-ownership.md.
func sourceOrderKey(env facts.Envelope) string {
	return env.ObservedAt.UTC().Format(sourceOrderKeyTimestampLayout) + sourceOrderKeySeparator + env.FactID
}

// preferMaxSourceOrderKey reports whether candidate should replace existing in a
// byUID deduplication map: true when candidate carries a strictly greater
// sourceOrderKeyField value, or when existing is nil (no prior contributor for
// this uid yet). This is the single within-scope duplicate-uid tie-break rule
// #5007 Stage 1 requires every Extract*NodeRows function to share with the owner
// ledger's cross-scope resolution, so "which contributor wins" is one rule, not
// two. Both rows are produced by this package's own row builders, which always
// stamp sourceOrderKeyField as a string; a row missing or mistyping that field
// is a programmer error, and this function fails safe by preferring the
// candidate rather than silently keeping a stale row forever.
func preferMaxSourceOrderKey(existing, candidate map[string]any) bool {
	if existing == nil {
		return true
	}
	candidateKey, candidateOK := candidate[sourceOrderKeyField].(string)
	existingKey, existingOK := existing[sourceOrderKeyField].(string)
	if !candidateOK || !existingOK {
		return true
	}
	return candidateKey > existingKey
}
