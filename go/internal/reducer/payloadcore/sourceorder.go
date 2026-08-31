// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package payloadcore

import "github.com/eshu-hq/eshu/go/internal/facts"

// SourceOrderKeyField is the node-row map key every #5007 Stage 1 row builder
// stamps with SourceOrderKey's output, and the graphowner gate reads as
// row.source_order_key to resolve cross-scope ownership.
const SourceOrderKeyField = "source_order_key"

// SourceOrderKeySeparator joins the fixed-width timestamp segment to the
// tie-break source_fact_id segment. Its exact value does not affect correctness
// (the timestamp segment's fixed width already guarantees two keys' timestamp
// segments are compared in full before either string reaches the separator),
// but a value outside the timestamp/fact-id alphabet keeps the two segments
// visually distinct.
const SourceOrderKeySeparator = "|"

// SourceOrderKeyTimestampLayout formats an envelope's ObservedAt as a
// fixed-width, UTC, nanosecond-precision timestamp. Every value produced by
// this layout has exactly the same length (unlike time.RFC3339Nano, which
// trims trailing zero fractional digits), so lexicographic string comparison
// of two SourceOrderKey values always agrees with chronological comparison of
// the ObservedAt values they were built from.
const SourceOrderKeyTimestampLayout = "2006-01-02T15:04:05.000000000Z"

// SourceOrderKey computes the #5007 Stage 1 deterministic order key for one
// contributing fact: max (observed_at, source_fact_id), i.e. the
// lexicographically comparable concatenation of a fixed-width UTC timestamp and
// the fact's stable id. Two different facts about the same node uid almost never
// share an identical (observed_at, source_fact_id) pair (fact ids are unique per
// fact), so this key gives a total order over contributors: the owner ledger
// keeps a shared node's scope-derived properties on whichever contributor has
// the greatest SourceOrderKey, independent of commit order or worker count, and
// PreferMaxSourceOrderKey applies the identical rule to within-scope
// duplicate-uid resolution during extraction. See
// docs/internal/design/5007-cross-scope-node-ownership.md.
func SourceOrderKey(env facts.Envelope) string {
	return env.ObservedAt.UTC().Format(SourceOrderKeyTimestampLayout) + SourceOrderKeySeparator + env.FactID
}

// PreferMaxSourceOrderKey reports whether candidate should replace existing in a
// byUID deduplication map: true when candidate carries a strictly greater
// SourceOrderKeyField value, or when existing is nil (no prior contributor for
// this uid yet). This is the single within-scope duplicate-uid tie-break rule
// #5007 Stage 1 requires every Extract*NodeRows function to share with the owner
// ledger's cross-scope resolution, so "which contributor wins" is one rule, not
// two. Both rows are produced by the reducer's own row builders, which always
// stamp SourceOrderKeyField as a string; a row missing or mistyping that field
// is a programmer error, and this function fails safe by preferring the
// candidate rather than silently keeping a stale row forever.
func PreferMaxSourceOrderKey(existing, candidate map[string]any) bool {
	if existing == nil {
		return true
	}
	candidateKey, candidateOK := candidate[SourceOrderKeyField].(string)
	existingKey, existingOK := existing[SourceOrderKeyField].(string)
	if !candidateOK || !existingOK {
		return true
	}
	return candidateKey > existingKey
}
