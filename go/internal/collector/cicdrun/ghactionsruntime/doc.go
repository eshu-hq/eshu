// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package ghactionsruntime collects bounded GitHub Actions run metadata for
// the CI/CD run collector family.
//
// The package owns hosted provider polling and claim resolution. It delegates
// fact envelope construction to the fixture-backed cicdrun normalizer so live
// provider rows and offline fixtures share one schema. HTTP responses are
// closed by the client after each bounded provider read. The package does not
// read artifact contents, logs, secrets, graph state, or query state.
//
// Every claim fetches a bounded, stateless window of the most recent runs
// (max_runs); re-fetching the same window on a later cycle is an idempotent
// upsert at projection, not a resume operation. An optional
// SourceConfig.Watermarks (runwatermark.Store) tracks the newest run ID a
// claim observed so a LATER claim can detect -- not resume -- a cross-cycle
// gap: when more than max_runs runs land between two cycles, runs between
// the previous watermark and the new window's floor were never fetched by
// either cycle. See run_watermark.go.
package ghactionsruntime
