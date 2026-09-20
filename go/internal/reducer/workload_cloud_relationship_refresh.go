// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/reducer/code/value/affected"
)

// refreshResultSignals runs the value-flow refresh emit gate over the
// committed rows' workload ids and merges the signal into base. It
// short-circuits to 0 without a graph read when nothing was written, and
// fails open to 1 (with a warning) when the gate is unwired or its read
// errors: a spurious refresh is a bounded extra solve, a missed one is silent
// accuracy loss.
func (h WorkloadCloudRelationshipMaterializationHandler) refreshResultSignals(
	ctx context.Context,
	intent Intent,
	base map[string]float64,
	readyRows int,
	rows []map[string]any,
) map[string]float64 {
	count := 0.0
	if readyRows > 0 {
		count = 1
		if h.AffectedGraph != nil {
			n, err := affected.ReposWithCloudCallersForWorkloads(ctx, h.AffectedGraph, committedWorkloadIDs(rows))
			if err != nil {
				slog.Warn("value-flow refresh gate failed open",
					"domain", DomainWorkloadCloudRelationshipMaterialization,
					"scope_id", intent.ScopeID,
					"generation_id", intent.GenerationID,
					"error", err,
				)
			} else {
				count = float64(n)
			}
		}
	}
	return affected.WithRefreshSignal(base, count)
}

// committedWorkloadIDs lists the distinct non-empty workload ids of committed
// rows for the emit gate.
func committedWorkloadIDs(rows []map[string]any) []string {
	seen := make(map[string]struct{}, len(rows))
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		id := anyToString(row["workload_id"])
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}
