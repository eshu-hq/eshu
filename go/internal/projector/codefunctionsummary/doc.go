// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package codefunctionsummary builds the code_function_summary reducer intent
// from one immutable scope generation: it fires when the generation carries a
// code_function_summary finding, else when it carries only the
// code_dataflow_scanned full-scan marker, and fires on neither otherwise. A
// finding always outranks the marker as the intent's FactID/Reason
// provenance, regardless of which appears earlier in the generation — the two
// kinds are looked up independently, not merged by original order.
//
// The payload attaches a best-effort repo_id: derived from the winning
// trigger when it decodes cleanly (a code_function_summary fact's
// function_id prefix, or a code_dataflow_scanned marker's repo_id field),
// falling back to the marker's own repo_id when a summary trigger's decode
// fails to resolve one and the marker is also present. A repo_id that cannot
// be resolved from either fact is omitted from the payload rather than
// fabricated. The marker also sets full_snapshot true whenever it is present
// in the generation (regardless of which fact won provenance), which tells
// the reducer's DomainCodeFunctionSummary handler it may replace the
// repository's summary snapshot and prune summaries for functions deleted or
// renamed out of the latest complete value-flow scan — summary-only
// generations never carry that signal, since they observed only the
// functions that changed, not a complete repository scan.
//
// This package performs no typed-payload schema-version admission and holds
// no quarantine apparatus of its own; a malformed payload on either fact kind
// simply yields an unresolved repo_id, never a dropped intent or a
// projector-side dead letter. The reducer's DomainCodeFunctionSummary handler
// owns typed decode, quarantine, and durable summary/source/graph-id
// persistence. Root projector assembly owns lookup construction and
// lifetime, family invocation order, queue writes, retries, and telemetry.
package codefunctionsummary
