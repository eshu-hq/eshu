// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/reducer/code/value/affected"
)

// refreshResultSignals runs the value-flow refresh emit gate over the repos
// this projection materialized and merges the signal into base. It
// short-circuits to 0 without a graph read when nothing was written, and
// fails open to 1 (with a warning) when the gate is unwired or its read
// errors: a spurious refresh is a bounded extra solve, a missed one is silent
// accuracy loss.
func (h WorkloadMaterializationHandler) refreshResultSignals(
	ctx context.Context,
	intent Intent,
	base map[string]float64,
	totalWrites int,
	repoIDs []string,
) map[string]float64 {
	count := 0.0
	if totalWrites > 0 {
		count = 1
		gateCtx, gate := affected.BeginRefreshGateEvaluation(ctx, h.Tracer, h.Instruments, DomainWorkloadMaterialization)
		if h.AffectedGraph == nil {
			gate.End(affected.RefreshGateFailOpen, 0)
		} else if n, err := affected.ReposWithCloudCallers(gateCtx, h.AffectedGraph, repoIDs); err != nil {
			slog.Warn("value-flow refresh gate failed open",
				"domain", DomainWorkloadMaterialization,
				"scope_id", intent.ScopeID,
				"generation_id", intent.GenerationID,
				"outcome", affected.RefreshGateFailOpen,
				"affected_repo_count", 0,
				"error", err,
			)
			gate.End(affected.RefreshGateFailOpen, 0)
		} else if n == 0 {
			count = 0
			gate.End(affected.RefreshGateSuppressed, 0)
		} else {
			count = float64(n)
			gate.End(affected.RefreshGateAffected, n)
		}
	}
	return affected.WithRefreshSignal(base, count)
}
