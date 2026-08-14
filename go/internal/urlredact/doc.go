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
// Agreeing on WHICH bytes bound a pair is only half of it. An HTTP client
// building a nested URL writes the structure percent-encoded, so
// "?redirect_uri=%2Fcb%3Faccess_token%3D…" carries no bare "?" and no bare "="
// — the same credential as the literal spelling, past both walks. escape.go
// owns reading a separator through EXACTLY ONE layer of percent-encoding, in
// either hex case, and records why one layer is the right depth. The escape is
// copied through as it arrived, so nothing rewrites the operator's endpoint.
//
// Reading every escape as a separator is too much, though, and doing so
// introduced a partial leak: "?token=AAAA%26BBBB" joins its name to its value
// with a LITERAL "=", so the "%26" is two characters of a credential whose
// value is "AAAA&BBBB", and cutting there shipped "BBBB". An escape stands for
// a separator only at the depth its own pair was written at. BoundaryDepth
// names the two depths and IndexBoundary scans at either; both walks pick from
// how their pair's "=" was spelled.
//
// The package makes no judgement about what a VALUE looks like. There is no
// entropy check and no secret-pattern list. It asks the shared name predicate
// about the left half of a pair, exactly as every other redaction walk here
// does.
package urlredact
