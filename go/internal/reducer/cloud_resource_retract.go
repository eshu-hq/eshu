// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// CloudResourceNodeRetracter deletes globally-dead CloudResource node uids
// under the owner-ledger gate (graphowner.CloudResourceRetracter in
// production). Implementations MUST be idempotent by uid — the handler calls
// it on every attempt including retries and replays — and MUST only delete
// uids the global live-check proved dead in every scope, never a
// per-scope subset. A nil retracter is a no-op so the materialization domains
// stay safe to register before the retract slice is wired.
type CloudResourceNodeRetracter interface {
	RetractDeadCloudResourceNodes(ctx context.Context, uids []string, evidenceSource string) (retracted int, err error)
}

// retractDeadCloudResourceNodes runs one generation-diff retract: it resolves
// the scope's predecessor generation, loads that generation's source facts,
// extracts their node uids, diffs against the current generation's uid set,
// and hands the predecessor-only uids to the globally-gated retracter in
// sorted order.
//
// A nil retracter or nil prior-generation lookup skips the retract (the
// additive domains register before the slice is wired). A scope with no
// predecessor, or an empty diff, issues no retract call at all. Any other
// failure — predecessor lookup, predecessor load, predecessor extraction, or
// the retract itself — returns an error so the durable queue retries the
// whole intent; the write and retract it re-runs are both idempotent.
//
// Predecessor extraction drops quarantined facts without re-counting them:
// malformed input was already quarantined as input_invalid when its own
// generation materialized. Only valid predecessor rows seed candidates.
func retractDeadCloudResourceNodes(
	ctx context.Context,
	loader FactLoader,
	priorGeneration PriorGenerationID,
	retracter CloudResourceNodeRetracter,
	scopeID string,
	generationID string,
	factKinds []string,
	extractUIDs func([]facts.Envelope) (map[string]struct{}, error),
	currentUIDs map[string]struct{},
	evidenceSource string,
) (int, error) {
	if retracter == nil || priorGeneration == nil {
		return 0, nil
	}
	prior, found, err := priorGeneration(ctx, scopeID, generationID)
	if err != nil {
		return 0, fmt.Errorf("resolve prior generation for cloud resource retract: %w", err)
	}
	if !found {
		return 0, nil
	}
	priorEnvelopes, err := loadFactsForKinds(ctx, loader, scopeID, prior, factKinds)
	if err != nil {
		return 0, fmt.Errorf("load prior generation facts for cloud resource retract: %w", err)
	}
	priorUIDs, err := extractUIDs(priorEnvelopes)
	if err != nil {
		return 0, fmt.Errorf("extract prior generation uids for cloud resource retract: %w", err)
	}
	candidates := make([]string, 0, len(priorUIDs))
	for uid := range priorUIDs {
		if _, current := currentUIDs[uid]; !current {
			candidates = append(candidates, uid)
		}
	}
	if len(candidates) == 0 {
		return 0, nil
	}
	sort.Strings(candidates)
	retracted, err := retracter.RetractDeadCloudResourceNodes(ctx, candidates, evidenceSource)
	if err != nil {
		// An admission-drain refusal is a readiness miss, not a failure on
		// this intent's merits: classify it so the queue defers without
		// counting the attempt (see ClassifyCloudRetractError below).
		return 0, ClassifyCloudRetractError(fmt.Errorf("retract dead cloud resource nodes: %w", err))
	}
	return retracted, nil
}

// cloudRowUIDSet collects the non-blank row["uid"] values of extracted node
// rows: the shared uid-set derivation for the AWS/GCP/Azure predecessor
// diffs, whose extractors all key rows by the canonical cloud_resource_uid.
func cloudRowUIDSet(rows []map[string]any) map[string]struct{} {
	set := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if uid, _ := row["uid"].(string); uid != "" {
			set[uid] = struct{}{}
		}
	}
	return set
}

// cloudAdmissionNotReadyError marks a #6887 retract refused by the
// admission-drain fence as a readiness-gate miss: some scope's active
// generation still has a nonterminal cloud_inventory_admission work item, so
// the intent is waiting on an upstream phase, not failing on its own merits.
// The durable queue re-runs the intent once that admission lands.
type cloudAdmissionNotReadyError struct {
	cause error
}

func (e cloudAdmissionNotReadyError) Error() string { return e.cause.Error() }

func (e cloudAdmissionNotReadyError) Unwrap() error { return e.cause }

func (cloudAdmissionNotReadyError) Retryable() bool { return true }

// CloudAdmissionNotReadyFailureClass identifies an in-handler readiness-gate
// miss on the #6887 cloud-resource retract: the live-check found an
// active-generation cloud_inventory_admission work item still nonterminal
// (see reducercontract.ErrCloudAdmissionUndrained) and refused to prove
// death, so the whole materialization intent defers.
//
// Enrolled in nonCountingReducerRetryFailureClasses so the miss never erodes
// the retry budget and dead-letters a still-pending intent that the
// succeeded-only reopen path would never reopen — under continuous
// multi-scope ingest some scope is mid-activation much of the time, so a
// counting trip would dead-letter healthy node writes. Declaring the
// constant is not what enrolls it; TestEveryReadinessFailureClassIsEnrolled
// checks the registration.
const CloudAdmissionNotReadyFailureClass = "cloud_admission_not_ready"

func (cloudAdmissionNotReadyError) FailureClass() string {
	return CloudAdmissionNotReadyFailureClass
}

// ClassifyCloudRetractError maps a retract failure to the queue's readiness
// vocabulary: an error wrapping reducercontract.ErrCloudAdmissionUndrained
// becomes a Retryable, non-counting cloudAdmissionNotReadyError that still
// unwraps to the sentinel; any other error is returned unchanged. Exported
// so the queue's budget test can drive the exact error the handler returns.
func ClassifyCloudRetractError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, reducercontract.ErrCloudAdmissionUndrained) {
		return cloudAdmissionNotReadyError{cause: err}
	}
	return err
}
