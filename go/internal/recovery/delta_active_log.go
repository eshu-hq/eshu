// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recovery

import (
	"context"
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// DeltaActiveGeneration is one refinalized pair whose generation is a delta,
// with the outcome the refinalize reports for it (#7797).
type DeltaActiveGeneration struct {
	ScopeID      string
	GenerationID string
	// Outcome is DeltaActiveOutcomeReindexRequested for a git default-branch
	// scope (IsGitDefaultBranchScope), else DeltaActiveOutcomeReindexUnsupported.
	Outcome string
}

// deltaActiveScopeMessage is the per-scope log message of a refinalize that
// re-projected a delta generation.
const deltaActiveScopeMessage = "refinalize re-projected a delta generation; graph incomplete until a full generation activates"

// deltaActiveSummaryMessage is the one summary WARN a refinalize with any
// delta-active scope emits.
const deltaActiveSummaryMessage = "refinalize re-projected delta generations; graph incomplete for these scopes " +
	"until a full generation activates; see the response's delta_active_scopes for samples"

// LogDeltaActive writes the delta-active logs of one committed refinalize
// with a bounded WARN budget, so a recovery over hundreds of delta-active
// scopes does not flood the WARN stream an operator reads (#7797).
//
// The first DeltaActiveScopeSampleLimit entries of delta log at WARN with
// scope_id, generation_id, and outcome; every further entry logs the same
// fields at INFO. When delta is not empty, one summary WARN follows with the
// exact count per outcome from report, the per-scope WARN limit, and the INFO
// count. An empty delta logs nothing. A nil logger uses slog.Default().
func LogDeltaActive(ctx context.Context, logger *slog.Logger, report DeltaActiveScopes, delta []DeltaActiveGeneration) {
	if len(delta) == 0 {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}
	for i, generation := range delta {
		level := slog.LevelWarn
		if i >= DeltaActiveScopeSampleLimit {
			level = slog.LevelInfo
		}
		logger.Log(ctx, level, deltaActiveScopeMessage,
			slog.String(telemetry.LogKeyScopeID, generation.ScopeID),
			slog.String(telemetry.LogKeyGenerationID, generation.GenerationID),
			slog.String("outcome", generation.Outcome),
		)
	}
	logger.WarnContext(ctx, deltaActiveSummaryMessage,
		slog.Int("delta_active_total", report.Total()),
		slog.Int(DeltaActiveOutcomeReindexRequested, report.ByOutcome[DeltaActiveOutcomeReindexRequested]),
		slog.Int(DeltaActiveOutcomeReindexUnsupported, report.ByOutcome[DeltaActiveOutcomeReindexUnsupported]),
		slog.Int("per_scope_warn_limit", DeltaActiveScopeSampleLimit),
		slog.Int("per_scope_info_count", max(len(delta)-DeltaActiveScopeSampleLimit, 0)),
	)
}
