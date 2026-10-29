// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dockerfile

import (
	"strings"
	"unicode"
)

// sensitiveQueryTokens matches whole dot/dash/underscore-delimited or
// camelCase query-key tokens. Substring matching overreaches (it drops safe
// keys such as keyboard, author, or monkey), while token equality keeps glued
// camelCase credential keys such as AWSAccessKeyId dropping.
var sensitiveQueryTokens = map[string]bool{
	"sig": true, "signature": true, "signatures": true,
	"credential": true, "credentials": true,
	"token": true, "tokens": true,
	"key": true, "keys": true,
	"secret": true, "secrets": true,
	"auth": true, "authorization": true,
	"password": true, "passwords": true, "passwd": true, "pass": true, "pwd": true,
}

// compoundGlueTokens are non-credential words that only join credential
// compounds: api, access, and id never drop a key alone, but api+key,
// access+token, and access+key+id are credential-bearing. The set stays
// minimal on purpose: keyboard (key+board), author (auth+or), and monkey
// (mon+key) have no full segmentation because board, or, and mon are in
// neither set, so the safe-key regression tests stay green.
var compoundGlueTokens = map[string]bool{
	"api": true, "access": true, "id": true,
}

// maxCompoundTokenRunes bounds compound segmentation attempts. Longer tokens
// fail closed (drop), the same direction as malformed escapes in
// hasTokenQuery: an unbounded DP over an adversarial key is not worth the
// risk for evidence parsed from Dockerfiles.
const maxCompoundTokenRunes = 256

// isCompoundCredentialToken reports whether a lowered alphanumeric token
// fully segments into two or more dictionary or glue pieces with at least
// one sensitive piece. Whole-token equality stays the primary match; this
// only catches glued lowercase compounds such as apikey or accesstoken that
// carry no delimiter or case boundary to split on.
func isCompoundCredentialToken(lowered string) bool {
	runes := []rune(lowered)
	if len(runes) > maxCompoundTokenRunes {
		return true
	}
	n := len(runes)
	// reach[i][t][c]: prefix runes[:i] segments into pieces, t marks a
	// sensitive piece seen, c caps the piece count at 2.
	var reach [maxCompoundTokenRunes + 1][2][3]bool
	reach[0][0][0] = true
	for i := 0; i < n; i++ {
		for t := 0; t < 2; t++ {
			for c := 0; c < 3; c++ {
				if !reach[i][t][c] {
					continue
				}
				for j := i + 1; j <= n; j++ {
					piece := string(runes[i:j])
					inDict := sensitiveQueryTokens[piece]
					if !inDict && !compoundGlueTokens[piece] {
						continue
					}
					nt := t
					if inDict {
						nt = 1
					}
					nc := c + 1
					if nc > 2 {
						nc = 2
					}
					reach[j][nt][nc] = true
				}
			}
		}
	}
	return reach[n][1][2]
}

// queryKeyTokens splits a decoded query key on non-alphanumeric runes and
// camelCase boundaries so each token can be compared whole. It unions the
// case-sensitive camel split (which catches glued keys such as AWSAccessKeyId)
// with a plain delimiter split of the lowercased key (which keeps mixed-case
// spellings such as x-AmZ-sIgNaTuRe whole enough to match).
func queryKeyTokens(key string) []string {
	tokens := make([]string, 0, 8)
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			tokens = append(tokens, cur.String())
			cur.Reset()
		}
	}
	runes := []rune(key)
	for i, r := range runes {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			flush()
			continue
		}
		if unicode.IsUpper(r) {
			prevLower := i > 0 && unicode.IsLower(runes[i-1])
			prevUpper := i > 0 && unicode.IsUpper(runes[i-1])
			nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			if prevLower || (prevUpper && nextLower && cur.Len() > 0) {
				flush()
			}
		}
		cur.WriteRune(unicode.ToLower(r))
	}
	flush()
	return append(tokens, strings.FieldsFunc(strings.ToLower(key), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})...)
}
