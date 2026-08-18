// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package pgarray encodes and decodes the Postgres text-array literal for the
// two element types Eshu stores -- text[] and double precision[] -- and quotes
// SQL identifiers. It replaces the four symbols the repository used from
// github.com/lib/pq (Array, StringArray, Float64Array, QuoteIdentifier), which
// was dropped when govulncheck reported unfixable advisories against it.
//
// The wire behaviour is deliberately identical to what lib/pq v1.10.9
// produced, because Eshu already talks to Postgres through the pgx stdlib
// driver and passes these values as text parameters. Every StringArray element
// is double-quoted with `"` and `\` backslash-escaped, a nil slice is SQL
// NULL, an empty slice is "{}", and Float64Array renders shortest-round-trip
// 'f' notation. That equivalence was proven byte-for-byte by a differential
// test run against lib/pq before it was removed; the expectations that test
// froze live on in pgarray_test.go.
//
// Only one-dimensional arrays are supported. Scan rejects a NULL element for
// both types rather than dropping it, and Array rejects any element type other
// than []string, *[]string, []float64 and *[]float64 with a typed error at
// Value/Scan time. This package depends on the standard library alone and does
// no reflection.
package pgarray
