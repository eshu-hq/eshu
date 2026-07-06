// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package v1 defines the schema-version-1 typed payload structs for the git
// collector's value-flow ("dataflow") fact family (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md), decoded through the parent
// factschema package's kind-keyed seam (decode.go, decode_codedataflow.go).
//
// The family has six fact kinds, all emitted only when the value-flow gate
// (ESHU_EMIT_DATAFLOW) is on (go/internal/collector/git_snapshot_*.go,
// git_followup_facts.go dataflowScannedFactEnvelope):
//
//   - DataflowScanned ("code_dataflow_scanned"): a per-generation
//     reconciliation marker carrying no findings, emitted once per repository
//     regardless of whether the scan produced any findings, so the reducer
//     still reconciles (and retracts stale) value-flow evidence domains on a
//     generation with zero findings.
//   - DataflowFunction ("code_dataflow_function"): one parser-emitted
//     function-level CFG/reaching-definition/control-dependence record. Exact
//     parser evidence for the API/MCP code-flow read surface
//     (go/internal/query/code_flow_postgres.go); no reducer materialization
//     handler decodes this kind today.
//   - FunctionSummary ("code_function_summary"): one function's durable
//     value-flow Effects (structural summary), keyed by the
//     generation-independent FunctionID, that the reducer persists to the
//     function-summary store for cross-repo interprocedural composition.
//   - FunctionSource ("code_function_source"): one function parameter that is
//     a taint entry point, persisted as an interproc source port.
//   - TaintEvidence ("code_taint_evidence"): one resolved intraprocedural
//     taint finding the collector has already joined to its Function entity
//     uid, projected by the reducer as graph evidence attached to that
//     Function node.
//   - InterprocEvidence ("code_interproc_evidence"): one resolved
//     cross-function taint finding with both endpoints already joined to
//     their Function entity uids, projected by the reducer as a
//     TAINT_FLOWS_TO edge between the two Function nodes.
//
// Every struct in this package types the fields the reducer or its postgres
// loader actually reads for identity, join, or graph-edge construction — not
// the full wire shape. A field the collector unconditionally emits but no
// consumer reads is OPTIONAL, matching the codegraph family's precedent
// (sdk/go/factschema/codegraph/v1/doc.go): requiring an emit-only field would
// dead-letter usable graph truth, the wrong contract under Contract System
// v1's "don't drop right results" accuracy guarantee.
//
// Nested per-finding evidence shapes the parser emits (cfg_blocks, cfg_edges,
// def_use, control_dependencies, why_trail) keep their INNER keys UNTYPED
// (each element is a []map[string]any, not a nested struct), mirroring
// codegraphv1.File's ParsedFileData precedent: these are read-and-forward
// payloads (the query layer forwards them to API/MCP callers verbatim) with
// no reducer field-level consumer, and per-nested-shape typing is out of
// scope for this migration. The outer shape is still asserted as
// []map[string]any rather than a fully open []any: an open []any generates
// an unconstrained "items: true" JSON Schema, a construct the collector
// conformance validator's supported schema subset rejects
// (sdk/go/collector/conformance) — see dataflow_function.go's CFGBlocks/
// CFGEdges field comments for the concrete failure this avoids.
//
// None of these six kinds are registered in specs/fact-kind-registry.v1.yaml
// (deferred to issue #4752): they are version-less on the wire, so the
// Postgres persist layer stamps SchemaVersion="0.0.0"
// (go/internal/storage/postgres/facts_streaming.go emptyToDefault), which the
// reducer's factschemaEnvelope adapter (factschema_decode.go) normalizes to
// the latest major exactly like codegraph's "file"/"repository" kinds.
//
// The reducer decodes only the latest struct for each kind. Version shims for
// an older schema major live in the parent factschema package's decode seam
// (decodeLatestMajor in decode.go), never in this package or in reducer
// handler code.
package v1
