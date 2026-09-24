// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package statement reduces a Cypher statement to its literal-free shape so
// the text can be logged or hashed without carrying any value a caller typed
// into the query.
//
// Redact is a single-pass lexical scanner, not a parser and not a regular
// expression over the whole text. It replaces every integer, float
// (including exponent, suffix, hex, octal and Neo4j 5 digit-separator forms),
// single-quoted string and double-quoted string with Placeholder, drops
// comments, and collapses whitespace (ASCII and Unicode). Booleans and null are
// kept. Identifiers, labels, relationship types, property keys,
// $parameters and backtick-quoted identifiers are kept verbatim, because none
// of them is a value: parameter values bind outside the statement text.
//
// The scanner never fails and never rejects input. Malformed text (an
// unterminated string, comment or backtick identifier) redacts to the end of
// the statement, so a half-typed literal cannot leak. The result is the shared
// input for the graph-read statement fingerprint and the bounded log head in
// package query (#7035).
package statement
