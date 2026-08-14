// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package urlredact owns where one "key=value" pair ends inside query-shaped
// text, and removes the value of every pair whose name
// collector.IsSensitiveKeyName flags.
//
// It exists because two redaction walks in this repository had to agree on that
// boundary and did not. cmd/eshu's redactEndpoint split a query string on "&"
// alone; internal/reportbundle's free-text scan already ended a value at "?",
// "&" or ";". A comment claimed the two "cannot disagree" because they share
// collector.IsSensitiveKeyName — true about NAMES, false about BOUNDARIES, and
// the gap shipped three credentials verbatim into an operator artifact:
// "?a=1;token=…", "?next=/v0/y?api_key=…", and
// "?redirect_uri=/cb?access_token=…".
//
// So PairSeparators lives here once and both walks read it, and BoundaryCases
// is the shared corpus both walks are driven through. A row that one walk
// handles and the other cannot is recorded in the row itself, with the reason,
// rather than left for a reader to discover.
//
// The package makes no judgement about what a VALUE looks like. There is no
// entropy check and no secret-pattern list. It asks the shared name predicate
// about the left half of a pair, exactly as every other redaction walk here
// does.
package urlredact
