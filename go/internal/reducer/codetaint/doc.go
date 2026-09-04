// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package codetaint materializes the code_taint_evidence and
// code_interproc_evidence reducer domains: value-flow taint findings
// attached to their Function as CodeTaintEvidence graph nodes, and
// cross-function value-flow findings projected as TAINT_FLOWS_TO edges
// between Function nodes.
//
// The two families share one package rather than splitting into
// codetaint/codeinterproc siblings (issue #6061) because they are genuinely
// coupled, not just co-located: [CodeTaintEvidenceMaterializationHandler]
// decodes through the same typed-contracts seam
// (DecodeCodeTaintEvidenceInput/DecodeCodeInterprocEvidenceInput,
// ExtractCodeTaintEvidenceRowsWithQuarantine/
// ExtractCodeInterprocEvidenceRowsWithQuarantine) as
// [CodeInterprocEvidenceMaterializationHandler], and the sibling valueflow
// package's value-flow fixpoint solver (value_flow_fixpoint_evidence_loader.go)
// composes [CodeInterprocEvidenceInput] and
// [ExtractCodeInterprocFixpointEvidenceRows] directly. Moving either family
// alone would just relocate a two-package import cycle back into the
// reducer root.
//
// [CodeTaintEvidenceMaterializationHandler] and
// [CodeInterprocEvidenceMaterializationHandler] each: load raw fact
// envelopes for one scope generation, decode them through the typed
// contracts seam (a malformed required field — function_uid, or
// source_function_uid/sink_function_uid — dead-letters as an input_invalid
// quarantine rather than being silently dropped, Contract System v1 Wave 4f
// S2, issue #4754), retract the prior generation's rows unless this is the
// scope's first generation, and write the newly projected rows. When a
// [CodeTaintEvidenceProjectedNodeLedger] or [CodeInterprocProjectedEdgeLedger]
// is wired, retraction enumerates uids from the ledger and calls the
// anchored-delete writer method instead of a broad scope-stamped retract;
// the ledger record always happens before the graph write so it stays a
// superset of graph state (issue #4893).
//
// [CodeTaintEvidenceProjectedNodeBackfiller] and
// [CodeInterprocProjectedEdgeBackfiller] are one-time, idempotent startup
// backfills that seed those ledgers from existing graph nodes/edges for
// deployments that predate the ledger, count-guarded so a zero-taint graph
// backfills for free.
package codetaint
