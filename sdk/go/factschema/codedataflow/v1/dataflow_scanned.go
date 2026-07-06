// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// DataflowScanned is the schema-version-1 typed payload for the
// "code_dataflow_scanned" fact kind (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md).
//
// A code_dataflow_scanned fact is the git collector's once-per-generation
// reconciliation marker (go/internal/collector/git_followup_facts.go
// dataflowScannedFactEnvelope), emitted whenever the value-flow gate
// (ESHU_EMIT_DATAFLOW) ran for a repository, regardless of whether the scan
// produced any taint/interproc findings. It carries no findings; its sole
// purpose is to let the projector's reducer-intent builders
// (go/internal/projector/code_function_summary_intents.go,
// code_taint_evidence_intents.go, code_interproc_evidence_intents.go) trigger
// their reconciliation domains even on a generation whose finding set is
// empty, so stale evidence from a prior generation is retracted rather than
// left stranded.
//
// RepoID is the only field any consumer reads (the projector's
// buildCodeFunctionSummaryReducerIntent falls back to it when no summary fact
// is present in the same batch). It is OPTIONAL here, matching the
// projector's own tolerant read (payloadString returns "" on a missing key
// without failing the marker's trigger role) — the marker's job is "the gate
// ran," which is true regardless of whether repo_id resolved, so promoting it
// to required would dead-letter a fact whose only content-bearing field the
// consumer already handles as optional.
type DataflowScanned struct {
	// RepoID is the scanned repository's canonical id. Optional: the
	// projector's trigger-fact fallback already tolerates an absent value
	// (buildCodeFunctionSummaryReducerIntent), so requiring it here would
	// dead-letter a marker whose sole job (signaling "the gate ran") does not
	// depend on it.
	RepoID *string `json:"repo_id,omitempty"`

	// Reason is a human-readable note on why this marker was emitted.
	// Optional: always emitted by dataflowScannedFactEnvelope, but read by no
	// consumer.
	Reason *string `json:"reason,omitempty"`
}
