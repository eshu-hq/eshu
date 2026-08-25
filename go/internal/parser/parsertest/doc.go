// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package parsertest provides shared assertions and fixture helpers for
// external parser test packages.
//
// The helpers exercise the parent parser's public Engine contract and preserve
// its concrete map-slice and string-slice payload assertions. They fail through
// testing.T so call sites report the external test line, and production
// packages must not import this package.
package parsertest
