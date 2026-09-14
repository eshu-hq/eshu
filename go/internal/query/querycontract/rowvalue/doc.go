// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package rowvalue converts a graph or SQL driver's untyped result row into Go
// values without panicking.
//
// A driver hands a row back as map[string]any, so every read path has to assert
// each column's type before using it. These helpers do that assertion once. A
// missing key or a nil always yields the zero value. An unexpected type yields
// the zero value too, except in [StringVal], which renders it with %v because a
// driver returning a number where a string was expected still carries the value
// the caller asked for. A graph read that lost one column should degrade that
// field, not fail the whole request.
//
// [IntVal] and [FloatVal] accept the several numeric shapes a driver actually
// returns -- int64 over Bolt, int from an in-process fake, float64 after a JSON
// round trip, plus float32 in [FloatVal] -- rather than only the one the schema
// nominally declares.
//
// The package is deliberately a leaf: it imports nothing from the query family
// and names no driver type, handler or store. That is what lets a handler-family
// subpackage decode rows without importing its parent, which it cannot do
// without an import cycle through the parent's compatibility aliases. Keep it
// that way -- adding a family dependency here would re-create the cycle for
// every package that depends on it.
//
// Callers that still name these helpers on querycontract or on package query
// reach them through forwarding wrappers in those packages; the behaviour is
// identical.
package rowvalue
