// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package codedivergence assembles code-divergence findings from fingerprint
// equality groups: parallel_implementation.exact (identical token streams)
// and parallel_implementation.renamed (identical alpha-renamed streams),
// plus parallel_implementation.wrapper_bypass (one thin wrapper fronting a
// target with cross-package direct callers, qualified over one-hop graph
// rows and carrying weakest-edge confidence) and
// parallel_implementation.convention_outlier (cohort members missing a call
// the cohort majority makes, selected over cohort-bounded graph rows and
// carrying weakest-edge confidence with the cohort evidence).
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
