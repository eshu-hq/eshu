// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package urlredact

import (
	"net/url"
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
	for i := 0; i <= len(rawQuery); i++ {
		if i < len(rawQuery) && strings.IndexByte(PairSeparators, rawQuery[i]) < 0 {
			continue
		}
		out.WriteString(redactPair(rawQuery[start:i], marker))
		if i < len(rawQuery) {
			// Write the separator that was actually there, not a canonical one.
			out.WriteByte(rawQuery[i])
		}
		start = i + 1
	}
	return out.String()
}

// redactPair rewrites one "name=value" pair when the name is credential-shaped,
// and returns it untouched otherwise.
//
// An empty value is the whole no-value test. strings.Cut returns an empty
// after-half for "token=" AND for a bare "token", so a separate !found check
// would be a clause no input can decide on its own — break it and every test
// still passes.
func redactPair(pair, marker string) string {
	name, value, _ := strings.Cut(pair, "=")
	if value == "" {
		return pair
	}
	decoded, err := url.QueryUnescape(name)
	if err != nil {
		decoded = name
	}
	if !collector.IsSensitiveKeyName(decoded) {
		return pair
	}
	return name + "=" + marker
}
