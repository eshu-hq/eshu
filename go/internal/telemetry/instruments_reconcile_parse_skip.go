// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// registerReconcileParseSkipDecisions registers the pre-parse skip decision
// counter (#7978 follow-up, ruling a9-143). One increment per forced reconcile
// the git collector evaluates, labeled outcome: skipped when the parse was
// skipped and the active generation re-projected, or the reason it parsed
// (reason_ineligible, disabled, no_active_full, delta_active, head_moved,
// identity_absent, identity_mismatch, submodules_present, dirty_tree,
// valve_due, read_error), or the store's answer to a skip that did not
// re-open (reprojection_pending: already in flight, no parse;
// reprojection_superseded: fenced, the collector parses).
func registerReconcileParseSkipDecisions(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.ReconcileParseSkipDecisions, err = meter.Int64Counter(
		"eshu_dp_collector_reconcile_parse_skip_decisions_total",
		metric.WithDescription("Pre-parse skip decisions of forced git reconciles by outcome (#7978)"),
	); err != nil {
		return fmt.Errorf("register ReconcileParseSkipDecisions counter: %w", err)
	}
	if inst.PreparseIdentityDropped, err = meter.Int64Counter(
		"eshu_dp_collector_preparse_identity_dropped_total",
		metric.WithDescription("Git snapshots and fact streams that kept the pre-parse identity off a generation, by reason (#7978)"),
	); err != nil {
		return fmt.Errorf("register PreparseIdentityDropped counter: %w", err)
	}
	return nil
}

// PreparseIdentityDropReason is the closed `reason` label of
// eshu_dp_collector_preparse_identity_dropped_total (#7978 review F-2, F-7):
// why the git collector kept the pre-parse identity off a generation. Every
// call site uses one of the constants below; TestPreparseDropReasonsAreClosed
// reads the call sites and this registry.
type PreparseIdentityDropReason string

// Snapshot-phase reasons (the snapshot degraded before any fact was emitted)
// and stream-phase reasons (the fact stream could not read a file between the
// parse and the emit).
const (
	PreparseDropSCIPBinaryUnavailable PreparseIdentityDropReason = "scip_binary_unavailable"
	PreparseDropSCIPIndexerFailed     PreparseIdentityDropReason = "scip_indexer_failed"
	PreparseDropSCIPParseFailed       PreparseIdentityDropReason = "scip_parse_failed"
	PreparseDropFileReadFailed        PreparseIdentityDropReason = "file_read_failed"
	PreparseDropDocsReadFailed        PreparseIdentityDropReason = "docs_read_failed"
	PreparseDropCodeownersReadFailed  PreparseIdentityDropReason = "codeowners_read_failed"
	PreparseDropWorkflowDigestFailed  PreparseIdentityDropReason = "workflow_digest_failed"
	PreparseDropContentReadFailed     PreparseIdentityDropReason = "content_read_failed"
	PreparseDropWorkflowReadFailed    PreparseIdentityDropReason = "workflow_read_failed" // #nosec G101 -- metric label value, not a credential
	PreparseDropDocsBodyReadFailed    PreparseIdentityDropReason = "docs_body_read_failed"
	// PreparseDropOther is the bounded fallback label for a value outside the
	// set; NormalizePreparseIdentityDropReason maps any unknown value to it.
	PreparseDropOther PreparseIdentityDropReason = "other"
)

// PreparseIdentityDropReasons returns the closed reason set in a stable order,
// without the PreparseDropOther fallback.
func PreparseIdentityDropReasons() []PreparseIdentityDropReason {
	return []PreparseIdentityDropReason{
		PreparseDropSCIPBinaryUnavailable, PreparseDropSCIPIndexerFailed, PreparseDropSCIPParseFailed,
		PreparseDropFileReadFailed, PreparseDropDocsReadFailed, PreparseDropCodeownersReadFailed,
		PreparseDropWorkflowDigestFailed, PreparseDropContentReadFailed, PreparseDropWorkflowReadFailed,
		PreparseDropDocsBodyReadFailed,
	}
}

// NormalizePreparseIdentityDropReason returns reason when it is in the closed
// set and PreparseDropOther otherwise, so the label can never grow unbounded.
func NormalizePreparseIdentityDropReason(reason PreparseIdentityDropReason) PreparseIdentityDropReason {
	for _, known := range PreparseIdentityDropReasons() {
		if reason == known {
			return reason
		}
	}
	return PreparseDropOther
}
