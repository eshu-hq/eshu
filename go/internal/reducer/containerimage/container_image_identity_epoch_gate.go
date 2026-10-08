// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package containerimage

import (
	"context"
	"errors"
	"fmt"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// gatedActivationEpoch reads the intent's activation epoch and classifies a
// miss instead of failing it as a permanent bug (issue #6502). The epoch join
// cannot tell a pending generation from a superseded or missing one: it
// returns no row for all three, and Handle used to wrap that miss in a plain
// error, which the queue dead-letters as projection_bug on the first pass.
//
// On a miss the gate consults GenerationCheck, which sees the scope's live
// lifecycle row rather than the scope-state row:
//
//   - a pending, newer-than-active generation returns the check's
//     GenerationNotYetActiveError, so the queue retries the intent without
//     counting against its attempt budget until the generation activates;
//   - a superseded (or failed, or missing) generation returns an early
//     Result with Status superseded, which the queue acks succeeded;
//   - a generation the check still calls current re-reads the epoch once, in
//     case the activation landed between the two reads, and only surfaces
//     loudly when the row is still missing — that is the genuinely missing
//     epoch, which stays a hard lifecycle error.
//
// A nil GenerationCheck preserves the legacy loud error. Any other epoch
// read failure (duplicate row, invalid epoch, query error) is returned
// unchanged.
func (h ContainerImageIdentityHandler) gatedActivationEpoch(
	ctx context.Context,
	intent reducercontract.Intent,
) (int64, *reducercontract.Result, error) {
	epoch, err := h.Writer.ContainerImageIdentityActivationEpoch(
		ctx,
		intent.ScopeID,
		intent.GenerationID,
	)
	if err == nil {
		return epoch, nil, nil
	}
	if !errors.Is(err, reducercontract.ErrContainerImageIdentityGenerationNotActive) {
		return 0, nil, fmt.Errorf("read container image identity activation epoch: %w", err)
	}
	if h.GenerationCheck == nil {
		return 0, nil, fmt.Errorf("read container image identity activation epoch: %w", err)
	}
	current, checkErr := h.GenerationCheck(ctx, intent.ScopeID, intent.GenerationID)
	if checkErr != nil {
		return 0, nil, fmt.Errorf("check container image identity generation: %w", checkErr)
	}
	if !current {
		result := reducercontract.Result{
			IntentID:        intent.IntentID,
			Domain:          intent.Domain,
			Status:          reducercontract.ResultStatusSuperseded,
			CanonicalWrites: 0,
			CompletedAt:     time.Now(),
		}
		result.EvidenceSummary = fmt.Sprintf(
			"generation %s superseded for scope %s",
			intent.GenerationID, intent.ScopeID,
		)
		return 0, &result, nil
	}
	epoch, err = h.Writer.ContainerImageIdentityActivationEpoch(
		ctx,
		intent.ScopeID,
		intent.GenerationID,
	)
	if err != nil {
		return 0, nil, fmt.Errorf("read container image identity activation epoch: %w", err)
	}
	return epoch, nil, nil
}
