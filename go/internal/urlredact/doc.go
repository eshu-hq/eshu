// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package urlredact owns where one "key=value" pair begins and ends, and
// removes the value of every pair whose name collector.IsSensitiveKeyName
// flags.
//
// It does that in the two shapes a credential arrives in. Query walks a parsed
// query string and keeps the URL readable, replacing only the value half.
// FreeText walks PROSE line by line, finding a pair written as "key=value" or as
// a header "key: value", and replaces the whole pair with FreeTextMarker.
//
// Both walks live here because they have to agree on that boundary and did not.
// The endpoint walk split a query string on "&" alone; the free-text scan
// already ended a value at "?", "&" or ";". A comment claimed the two "cannot
// disagree" because they share collector.IsSensitiveKeyName — true about NAMES,
// false about BOUNDARIES, and the gap shipped three credentials verbatim into an
// operator artifact: "?a=1;token=…", "?next=/v0/y?api_key=…", and
// "?redirect_uri=/cb?access_token=…".
//
// So PairSeparators lives here once and both walks read it, and BoundaryCases
// is the shared corpus both walks are driven through. A row that one walk
// handles and the other cannot is recorded in the row itself, with the reason,
// rather than left for a reader to discover.
//
// The consumers keep only what is specific to them. internal/reportbundle keeps
// the DOMAIN question — which bundle fields are free text — and passes its own
// inline-content key name in as an ADDITION to the shared predicate, never a
// replacement, so a caller can widen the name set and can never narrow it.
// internal/cli/evidredact keeps URL assembly: the userinfo rule, the fragment
// rule, and the mcpsetup.RedactToken fallback for a value that does not parse as
// a URL, which would drag a CLI dependency in here for no gain.
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
// Two refinements, each learned from a leak the depth rule alone still allowed.
// The depth question asks about the NAME and the "=" and nothing else: when the
// escape is the first byte of the value, the pending text is a bare "token=",
// and a walk that also asked "is there a value here yet?" answered no and
// returned "?token=%26<credential>" untouched. And one layer down, only
// PairSeparators is structure — a walk over PROSE also ends a value at
// whitespace or a quote, and counting THOSE in escaped form cut a credential in
// half, because an encoder writes "%20" precisely because the space is inside a
// value. IndexBoundaryBySpelling is where a caller says which set it means in
// which spelling.
//
// DifferentialCases is the answer to how both of those reached review: two
// walks decided depth independently, both passed every row of the shared
// corpus, and they disagreed on 72 of 594 generated inputs. That table crosses
// the axes rather than enumerating the cases somebody already thought of.
//
// It asserts two different things, and conflating them cost it 36 rows of real
// coverage. CheckRemoval says both walks removed every fragment the depth model
// puts inside a credential value — 378 of the 594 rows carry one. CheckAgreement
// says the two decided every fragment the same way, which is all that can be
// asked of the rest, because over-removal is this package's accepted cost.
// Agreement on its own is silent when both walks stop removing together.
//
// The package makes no judgement about what a VALUE looks like. There is no
// entropy check and no secret-pattern list. It asks the shared name predicate
// about the left half of a pair, exactly as every other redaction walk here
// does.
package urlredact
