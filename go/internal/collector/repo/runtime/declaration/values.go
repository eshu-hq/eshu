// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import "strings"

// The pin-file value rule (#8969 review). A whitespace-split pin file line is
// a list of independent versions: .tool-versions lists fallbacks, the other
// single-runtime pins hold one. Splitting on whitespace alone would cut one
// declared expression into fragments, so "18 - 20" became partial(18) +
// alias("-") + partial(20). Two rules keep an expression whole:
//
//  1. A token that is only a comparator ("=", "==", "<", "<=", ">", ">=",
//     "~", "~>", "~=", "^") is joined to the version token after it. ">= 18"
//     is one declaration, raw_spec as written, with the same spec as ">=18".
//  2. If an operator-only token is still left (a hyphen range's "-", a "||",
//     a comparator with no version after it), or a token holds a "||" or
//     starts or ends with a hyphen ("18||20", "18 -20"), the value is not a list of
//     versions. This package does not read hyphen-range or disjunction
//     syntax in a pin file and fails closed: the whole value is one
//     declaration at order 0, classified alias (or unresolved when it holds
//     a placeholder), never a guessed version.
//
// The same check applies to a .sdkmanrc value so one text gets one spec in
// every pin file. Classify keeps its hyphen-range and disjunction grammar for
// source kinds whose tools define it (package.json engines, which the manifest
// readers pass to Classify whole).

// operatorBytes holds every byte a comparator or operator-only token is made
// of. A line without any of them needs no value-shape scan.
const operatorBytes = "<>=~^|!-"

// isComparator reports whether token is a comparator operator written apart
// from its version.
func isComparator(token string) bool {
	switch token {
	case "=", "==", "<", "<=", ">", ">=", "~", "~>", "~=", "^":
		return true
	}
	return false
}

// isOperatorOnly reports whether token holds only comparator or range
// punctuation, so it cannot name a version by itself.
func isOperatorOnly(token string) bool {
	if token == "" {
		return false
	}
	for i := 0; i < len(token); i++ {
		if strings.IndexByte(operatorBytes, token[i]) < 0 {
			return false
		}
	}
	return true
}

// joinsExpression reports whether token carries range or disjunction syntax
// inside it: a "||" (as in "18||20" or "18|| 20"), or a leading or trailing
// hyphen (as in "-20" or "18-" from "18 -20" and "18- 20"). A version never
// starts or ends with a hyphen, so such a token means a hyphen range or
// disjunction written without the spaces the operator-only rule needs.
func joinsExpression(token string) bool {
	return strings.Contains(token, "||") || token[0] == '-' || token[len(token)-1] == '-'
}

// startsVersion reports whether token starts like a numeric version: a digit,
// or a "v" before a digit.
func startsVersion(token string) bool {
	if token == "" {
		return false
	}
	if token[0] == 'v' || token[0] == 'V' {
		token = token[1:]
	}
	return token != "" && token[0] >= '0' && token[0] <= '9'
}

// valueShape scans the tokens of a value. first is the byte offset of the
// first token, or -1 when there is none. whole is true when the value must be
// one declaration (rule 2 above).
func valueShape(value string) (first int, whole bool) {
	scanner := tokenScanner{line: value}
	first = -1
	pending := false
	for {
		start, end, ok := scanner.next()
		if !ok {
			return first, pending
		}
		if first < 0 {
			first = start
		}
		token := value[start:end]
		switch {
		case joinsExpression(token):
			return first, true
		case pending:
			if !startsVersion(token) {
				return first, true
			}
			pending = false
		case isComparator(token):
			pending = true
		case isOperatorOnly(token):
			return first, true
		}
	}
}

// eachValue calls fn with the order and raw text of every declaration in a
// whitespace-split value, until fn returns false. A lone comparator is joined
// to the version after it, and raw is the span of the value as written. When
// the value is not a list of versions, fn is called once with order 0, the
// whole value text and whole set. Like eachToken it allocates nothing.
func eachValue(value string, fn func(order int, raw string, whole bool) bool) {
	if !strings.ContainsAny(value, operatorBytes) {
		// No token can be a comparator or operator-only, so every token is
		// its own declaration and one scan is enough.
		eachToken(value, func(order int, token string) bool {
			return fn(order, token, false)
		})
		return
	}
	first, whole := valueShape(value)
	if first < 0 {
		return
	}
	if whole {
		fn(0, value[first:], true)
		return
	}
	scanner := tokenScanner{line: value}
	order, operatorStart := 0, -1
	for {
		start, end, ok := scanner.next()
		if !ok {
			return
		}
		if operatorStart < 0 && isComparator(value[start:end]) {
			operatorStart = start
			continue
		}
		if operatorStart >= 0 {
			start, operatorStart = operatorStart, -1
		}
		if !fn(order, value[start:end], false) {
			return
		}
		order++
	}
}
