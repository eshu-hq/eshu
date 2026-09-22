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
//
// [Handler] does NOT run the global value-flow fixpoint itself (issue
// #6923): summaries are fixpoint inputs by definition, so Handle always
// reports the refresh_affected_repos sub-signal and CanonicalWrites counts a
// full-snapshot replace's removed rows alongside written ones (and never
// less than one for a full-snapshot replace), so even a replace that
// empties a repo, or a retry of one, still triggers a solve. That lets this
// handler's ACK become the fifth producer of the code/value/refresh
// singleton, which fences the actual solve until every writer of the
// cloud-sink chain has drained — collapsing what used to be up to one inline
// solve per repo generation onto one fenced global run.
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
