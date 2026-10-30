// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// commitEdges retracts this scope's USES edges and rewrites every row. The
// retract is skipped only for the first commit of a scope's first generation;
// recommit (an earlier commit of this generation is in the ledger) forces it.
func (h WorkloadCloudRelationshipMaterializationHandler) commitEdges(ctx context.Context, intent reducercontract.Intent, rows []map[string]any, recommit bool) error {
	skipRetract := false
	if !recommit {
		var err error
		if skipRetract, err = h.shouldSkipRetract(ctx, intent); err != nil {
			return err
		}
	}
	if !skipRetract {
		if err := h.EdgeWriter.RetractWorkloadCloudRelationshipEdges(
			ctx,
			[]string{intent.ScopeID},
			intent.GenerationID,
			workloadCloudRelationshipEvidenceSource,
		); err != nil {
			return fmt.Errorf("retract workload cloud USES edges: %w", err)
		}
	}
	if len(rows) == 0 {
		return nil
	}
	if err := h.EdgeWriter.WriteWorkloadCloudRelationshipEdges(
		ctx,
		rows,
		intent.ScopeID,
		intent.GenerationID,
		workloadCloudRelationshipEvidenceSource,
	); err != nil {
		return fmt.Errorf("write workload cloud USES edges: %w", err)
	}
	return nil
}

func (h WorkloadCloudRelationshipMaterializationHandler) shouldSkipRetract(ctx context.Context, intent reducercontract.Intent) (bool, error) {
	if h.PriorGenerationCheck == nil || intent.AttemptCount > 1 {
		return false, nil
	}
	hasPrior, err := h.PriorGenerationCheck(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil {
		return false, fmt.Errorf("check prior generation for workload cloud relationship retract: %w", err)
	}
	return !hasPrior, nil
}
