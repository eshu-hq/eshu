// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
)

// GraphProjectionPhaseRepair is the root spelling of [gpphase.PhaseRepair].
type GraphProjectionPhaseRepair = gpphase.PhaseRepair

// GraphProjectionPhaseRepairQueue is the root spelling of
// [gpphase.PhaseRepairQueue].
type GraphProjectionPhaseRepairQueue = gpphase.PhaseRepairQueue

// GraphProjectionPhaseRepairsFromStates forwards to
// [gpphase.PhaseRepairsFromStates].
func GraphProjectionPhaseRepairsFromStates(
	states []GraphProjectionPhaseState,
	lastError string,
	enqueuedAt time.Time,
) []GraphProjectionPhaseRepair {
	return gpphase.PhaseRepairsFromStates(states, lastError, enqueuedAt)
}
