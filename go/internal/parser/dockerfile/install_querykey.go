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
