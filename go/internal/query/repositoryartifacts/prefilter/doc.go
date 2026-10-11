// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package prefilter derives a sound necessary condition from a compiled
// regular expression, so a caller can skip running the expression on text
// that cannot contain a match.
//
// The condition is read from the expression's own syntax tree: the literal
// fragments every match must contain, joined with AND where they are all
// required and OR where the expression has alternatives. Case-insensitive
// fragments accept the whole Unicode simple-fold orbit (Go's (?i) folds "k"
// with U+212A KELVIN SIGN and "s" with U+017F LONG S), and a \b placed
// directly against a fragment is checked with Go's ASCII-only word rule.
//
// Soundness is the contract. When Set.Evaluate reports false for an
// expression, the expression has no match in the text, for every input
// including invalid UTF-8. A true value carries no claim. A small class (four
// runes or fewer) is a set of exact alternatives and a plus keeps its
// operand's requirement. Whatever the analysis cannot prove (a large class, a
// wildcard, an optional or starred part) contributes no requirement, and an
// expression with no requirement at all is reported ungated and always
// evaluates true. There is no hand-kept token list: nothing is gated that the
// pattern itself does not force.
//
// The package is pure: no state between calls, no I/O, no configuration.
package prefilter
