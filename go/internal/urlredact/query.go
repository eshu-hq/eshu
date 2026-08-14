// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package urlredact

import (
	"strings"

	"github.com/eshu-hq/eshu/sdk/go/collector"
)

// PairSeparators are the characters that end one key=value pair inside
// query-shaped text: "?" opens a nested query, "&" and ";" separate pairs
// within one.
//
// It lives here, in one place, because two walks that each defined their own
// copy drifted. internal/reportbundle had all three; cmd/eshu had only "&", so
// "?a=1;token=<credential>" reached an operator artifact whole.
const PairSeparators = "?&;"

// Query returns rawQuery with the value of every parameter whose name
// collector.IsSensitiveKeyName flags replaced by marker. Names, other
// parameters, their order, and the original separator bytes all survive, so the
// endpoint an operator has to match against their own config stays readable.
//
// It walks by index rather than Split/Join because the separators are not
// interchangeable: "?a=1;token=x" must come back with its ";" intact, and
// rebuilding on one chosen separator would rewrite an endpoint nobody asked to
// have rewritten. url.ParseQuery plus Encode is out for the same reason one
// level up — that pair sorts the parameters and re-encodes the ones it kept.
//
// A pair with no "=" ("?token") and a pair with an empty value ("?token=") are
// left exactly as they arrived. There is no value in either to remove, and
// writing "token=redacted" over them would report a credential the URL never
// carried.
//
// The name is percent-decoded before the predicate sees it, so "api%5Fkey" is
// recognized as "api_key". A name that will not decode is asked about as it
// arrived rather than skipped.
//
// A separator written as a percent-escape counts as that separator, one layer
// deep (see escape.go for why one). "?a=1&b=%3Ftoken%3D<credential>" is what an
// HTTP client produces for a nested URL, and reading only the literal spelling
// left it whole: there is no bare "?" or "=" in it for a split to find. The
// escape is copied through as it arrived, so the endpoint an operator has to
// match against their own config keeps its exact bytes.
//
// An escape only counts as a separator at the DEPTH its pair was written at,
// and getting that wrong is what made an encoding-aware walk truncate the very
// credentials it was added to remove. "?token=AAAA%26BBBB" joins its name to
// its value with a literal "=", so it is text at the surface and the "%26" is
// two characters the credential contains; splitting there left "BBBB" in the
// endpoint. "?a=1%26token%3D<credential>%26repo%3Ddemo" joins with "%3D", so
// its whole structure is one layer down and "%26" there is the separator it
// stands for. So an escaped separator is skipped when the pair it would end is
// a credential-named pair with a literal "=".
//
// The cost is over-removal: "?token=x%26repo=demo" loses the "repo=demo" an
// operator might have wanted, because it cannot be told apart from the tail of
// a credential containing an "&". Losing a benign parameter is the side to err
// on when the alternative is shipping half a credential.
//
// The result is a fixed point: marker is a non-empty value under a still-
// sensitive name, so a second pass rewrites it to itself. That matters because
// an evidence report can be re-rendered from a saved envelope.
func Query(rawQuery, marker string) string {
	if rawQuery == "" {
		return ""
	}
	var out strings.Builder
	out.Grow(len(rawQuery))
	start := 0
	for i := 0; i < len(rawQuery); {
		decoded, width := DecodedByteAt(rawQuery, i)
		if strings.IndexByte(PairSeparators, decoded) < 0 {
			i += width
			continue
		}
		if width != 1 && opensSurfaceCredential(rawQuery[start:i]) {
			// An escape cannot end a value that a literal "=" opened: it is
			// content of that value. Keep scanning so the whole tail goes.
			i += width
			continue
		}
		out.WriteString(redactPair(rawQuery[start:i], marker))
		// Write the separator that was actually there, in the spelling it
		// arrived in, not a canonical one.
		out.WriteString(rawQuery[i : i+width])
		i += width
		start = i
	}
	out.WriteString(redactPair(rawQuery[start:], marker))
	return out.String()
}

// redactPair rewrites one "name=value" pair when the name is credential-shaped,
// and returns it untouched otherwise.
//
// The "=" is found decode-aware, so "token%3D<credential>" splits where
// "token=<credential>" does. Everything up to and including the separator is
// copied through verbatim; only the value half is replaced. That keeps the
// original spelling of both the name and the separator in the output.
//
// Two shapes are left exactly as they arrived, and the test for them is one
// index check rather than two clauses: a bare "token" has no separator at all,
// and "token=" has nothing after it. Neither carries a value, and writing
// "token=redacted" over them would report a credential the URL never had.
func redactPair(pair, marker string) string {
	sep, width, ok := sensitivePairSeparator(pair)
	if !ok {
		return pair
	}
	return pair[:sep+width] + marker
}

// sensitivePairSeparator locates the "=" that joins a credential-named
// parameter to a value it actually carries, and reports how that "=" was
// spelled: width 1 for a literal one, EscapeWidth for "%3D".
//
// ok is false for a pair with no "=", a pair with nothing after it, and a pair
// whose name is not credential-shaped. Those are the three cases redactPair
// returns untouched, and the width is what tells Query whether an escaped
// separator ends this pair's value or belongs to it.
//
// The name is unwrapped through the package's own one-layer reader rather than
// url.QueryUnescape. The two differ on "+", and a second decoder beside the
// shared one is how the walks drifted in the first place — see escape.go.
// Nothing here regresses on "+": the name predicate matches on a substring, so
// "token+" is recognized whether or not the "+" became a space.
func sensitivePairSeparator(pair string) (int, int, bool) {
	sep, width := IndexBoundary(pair, "=", LiteralOrEscaped)
	if sep < 0 || sep+width >= len(pair) {
		return 0, 0, false
	}
	name := pair[:sep]
	if decoded, changed := Decode(name); changed {
		name = decoded
	}
	if !collector.IsSensitiveKeyName(name) {
		return 0, 0, false
	}
	return sep, width, true
}

// opensSurfaceCredential reports whether pair is a credential-named pair whose
// "=" was written literally, which is the case where an escaped separator after
// it is value content rather than the end of the value.
//
// It is asked about the text pending since the last pair boundary, so the
// answer is about the pair the escape would close.
func opensSurfaceCredential(pair string) bool {
	_, width, ok := sensitivePairSeparator(pair)
	return ok && width == 1
}
