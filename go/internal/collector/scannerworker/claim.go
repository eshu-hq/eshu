// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scannerworker

import (
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// NewClaimInput validates and copies a workflow claim into scanner-worker input.
func NewClaimInput(
	item workflow.WorkItem,
	claim workflow.Claim,
	analyzer AnalyzerKind,
	target TargetScope,
	limits ResourceLimits,
) (ClaimInput, error) {
	observedAt := item.LastClaimedAt
	if observedAt.IsZero() {
		observedAt = claim.ClaimedAt
	}
	return NewClaimInputAt(item, claim, analyzer, target, limits, observedAt)
}

// NewClaimInputAt validates and copies a workflow claim using the supplied
// observation time to reject claims whose lease already expired.
func NewClaimInputAt(
	item workflow.WorkItem,
	claim workflow.Claim,
	analyzer AnalyzerKind,
	target TargetScope,
	limits ResourceLimits,
	observedAt time.Time,
) (ClaimInput, error) {
	if err := item.Validate(); err != nil {
		return ClaimInput{}, fmt.Errorf("validate scanner work item: %w", err)
	}
	if err := claim.Validate(); err != nil {
		return ClaimInput{}, fmt.Errorf("validate scanner claim: %w", err)
	}
	if item.CollectorKind != scope.CollectorScannerWorker {
		return ClaimInput{}, fmt.Errorf("work item collector_kind must be %q", scope.CollectorScannerWorker)
	}
	if item.SourceSystem != string(scope.CollectorScannerWorker) {
		return ClaimInput{}, fmt.Errorf("work item source_system must be %q", scope.CollectorScannerWorker)
	}
	if item.Status != workflow.WorkItemStatusClaimed {
		return ClaimInput{}, fmt.Errorf("work item status must be %q", workflow.WorkItemStatusClaimed)
	}
	if claim.Status != workflow.ClaimStatusActive {
		return ClaimInput{}, fmt.Errorf("claim status must be %q", workflow.ClaimStatusActive)
	}
	if claim.WorkItemID != item.WorkItemID {
		return ClaimInput{}, fmt.Errorf("claim work_item_id %q does not match item %q", claim.WorkItemID, item.WorkItemID)
	}
	if claim.ClaimID != item.CurrentClaimID {
		return ClaimInput{}, fmt.Errorf("claim_id %q does not match active item claim %q", claim.ClaimID, item.CurrentClaimID)
	}
	if claim.FencingToken != item.CurrentFencingToken {
		return ClaimInput{}, fmt.Errorf("claim fencing_token %d does not match active item fencing_token %d", claim.FencingToken, item.CurrentFencingToken)
	}
	if claim.OwnerID != item.CurrentOwnerID {
		return ClaimInput{}, fmt.Errorf("claim owner_id %q does not match active item owner_id %q", claim.OwnerID, item.CurrentOwnerID)
	}
	if !claim.LeaseExpiresAt.Equal(item.LeaseExpiresAt) {
		return ClaimInput{}, fmt.Errorf("claim lease_expires_at %s does not match active item lease_expires_at %s", claim.LeaseExpiresAt.Format(time.RFC3339Nano), item.LeaseExpiresAt.Format(time.RFC3339Nano))
	}
	if observedAt.IsZero() {
		return ClaimInput{}, fmt.Errorf("observed_at must not be zero")
	}
	if !claim.LeaseExpiresAt.After(observedAt) {
		return ClaimInput{}, fmt.Errorf("claim expired before scanner-worker input construction")
	}
	lane, ok := AnalyzerLane(analyzer)
	if !ok {
		return ClaimInput{}, fmt.Errorf("unknown analyzer %q", analyzer)
	}
	if lane != LaneScannerWorker {
		return ClaimInput{}, fmt.Errorf("analyzer %q belongs to %q, not %q", analyzer, lane, LaneScannerWorker)
	}
	if err := target.validateFor(item); err != nil {
		return ClaimInput{}, err
	}
	if err := limits.validate(); err != nil {
		return ClaimInput{}, err
	}

	return ClaimInput{
		WorkItemID:     item.WorkItemID,
		ClaimID:        claim.ClaimID,
		FencingToken:   claim.FencingToken,
		OwnerID:        claim.OwnerID,
		Analyzer:       analyzer,
		Target:         target,
		Limits:         limits,
		GenerationID:   item.GenerationID,
		Attempt:        item.AttemptCount,
		ClaimedAt:      claim.ClaimedAt.UTC(),
		LeaseExpiresAt: claim.LeaseExpiresAt.UTC(),
		ObservedAt:     observedAt.UTC(),
	}, nil
}

func (input ClaimInput) validate() error {
	if strings.TrimSpace(input.WorkItemID) == "" {
		return fmt.Errorf("work_item_id must not be blank")
	}
	if strings.TrimSpace(input.ClaimID) == "" {
		return fmt.Errorf("claim_id must not be blank")
	}
	if input.FencingToken <= 0 {
		return fmt.Errorf("fencing_token must be positive")
	}
	if strings.TrimSpace(input.OwnerID) == "" {
		return fmt.Errorf("owner_id must not be blank")
	}
	if strings.TrimSpace(string(input.Analyzer)) == "" {
		return fmt.Errorf("analyzer must not be blank")
	}
	lane, ok := AnalyzerLane(input.Analyzer)
	if !ok {
		return fmt.Errorf("unknown analyzer %q", input.Analyzer)
	}
	if lane != LaneScannerWorker {
		return fmt.Errorf("analyzer %q belongs to %q, not %q", input.Analyzer, lane, LaneScannerWorker)
	}
	if err := input.Limits.validate(); err != nil {
		return err
	}
	if input.GenerationID != input.Target.GenerationID {
		return fmt.Errorf("generation_id %q does not match target generation_id %q", input.GenerationID, input.Target.GenerationID)
	}
	if input.ObservedAt.IsZero() {
		return fmt.Errorf("observed_at must not be zero")
	}
	if strings.TrimSpace(input.Target.LocatorHash) == "" {
		return fmt.Errorf("target locator_hash must not be blank")
	}
	if err := validateSafeLocatorHash(input.Target.LocatorHash); err != nil {
		return err
	}
	return nil
}
