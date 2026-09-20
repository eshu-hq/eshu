// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package codedivergence assembles code-divergence findings from fingerprint
// equality groups: parallel_implementation.exact (identical token streams)
// and parallel_implementation.renamed (identical alpha-renamed streams).
//
// A finding's score is members × token count, decomposed without remainder
// into reasons[]: one identical/renamed-stream reason plus one additional-copy
// reason per extra member, with zero-weight signal reasons (package span,
// large body) listed for judgment. score == sum(reasons) always; no hidden
// terms. Truth level is always derived, never exact.
//
// Suppression is counted, never silent: every rule is a named function with
// its own regression test, and assembly reports per-rule counts. The
// grouping SQL lives with ContentReader (package query); this package owns
// the finding contract both the HTTP handler and the MCP tools serve.
package codedivergence
