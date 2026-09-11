// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package summary persists one generation's durable value-flow function
// summaries: it loads the raw Effects, recomputes their content versions
// through a flow.Store, and upserts the resulting snapshot (issue #6061).
// The upsert is idempotent on FunctionID, so re-running a generation
// converges rather than duplicating. When the optional source and graph-id
// loader/writers are wired, [Handler.Handle] also persists
// that generation's param-level taint sources and the FunctionID->uid map,
// which the cross-repo value-flow fixpoint needs alongside the summaries.
// When the optional fixpoint projector is wired it runs after those durable
// writes complete, so graph projection cannot race ahead of persistence.
//
// This package's own name collides with its central dependency,
// internal/parser/summary (FunctionID, Effects, Snapshot, Store): every file
// aliases that import as parsed, so parsed.FunctionID/parsed.Effects/
// parsed.Snapshot read without a self-referential "summary.summary".
//
// The reducer root imports this package as summary. It wires
// [Definition] and [Handler] in
// defaults_additive_domains_incident_code.go and keeps the exported
// CodeFunctionSummary*/CodeFunctionSource*/CodeFunctionGraphID* spellings
// through the code-function-summary stanza of compat_decode.go and
// compat_correlation.go.
package summary
