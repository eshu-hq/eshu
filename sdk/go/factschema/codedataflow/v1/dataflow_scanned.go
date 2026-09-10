// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// DataflowScanned is the schema-version-1 typed payload for the
// "code_dataflow_scanned" fact kind (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md).
//
// A code_dataflow_scanned fact is the git collector's once-per-generation
// reconciliation marker (go/internal/collector/gitrepo/git_followup_facts.go
// dataflowScannedFactEnvelope), emitted whenever the value-flow gate
// (ESHU_EMIT_DATAFLOW) ran for a repository, regardless of whether the scan
// produced any taint/interproc findings. It carries no findings; its sole
// purpose is to let the projector's reducer-intent builders
// (go/internal/projector/code/function/summary/reducer_intent.go,
// go/internal/projector/code/taint/evidence/reducer_intent.go,
// go/internal/projector/code/interproc/evidence/reducer_intent.go) trigger
// their reconciliation domains even on a generation whose finding set is
// empty, so stale evidence from a prior generation is retracted rather than
// left stranded.
//
// RepoID is the only field any consumer reads. The summary projector uses it
// as a fallback and omits the payload key when it is absent; the marker still
// triggers reconciliation because the scan ran. Requiring RepoID here would
// reject a marker whose trigger role remains valid.
type DataflowScanned struct {
	// RepoID is the scanned repository's canonical id. Optional because the
	// function-summary projector tolerates an absent value; requiring it here
	// would dead-letter a marker whose sole job (signaling "the gate ran") does
	// not depend on it.
	RepoID *string `json:"repo_id,omitempty"`

	// Reason is a human-readable note on why this marker was emitted.
	// Optional: always emitted by dataflowScannedFactEnvelope, but read by no
	// consumer.
	Reason *string `json:"reason,omitempty"`
}
