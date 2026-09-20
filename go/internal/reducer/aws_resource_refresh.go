// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/reducer/code/value/affected"
)

// refreshResultSignals runs the value-flow refresh emit gate over the
// committed resource uids and merges the signal into base. It short-circuits
// to 0 without a graph read when nothing was written, and fails open to 1
// (with a warning) when the gate is unwired or its read errors: a spurious
// refresh is a bounded extra solve, a missed one is silent accuracy loss.
func (h AWSResourceMaterializationHandler) refreshResultSignals(
	ctx context.Context,
	intent Intent,
	base map[string]float64,
	writes int,
	rows []map[string]any,
) map[string]float64 {
	count := 0.0
	if writes > 0 {
		count = 1
		gateCtx, gate := beginRefreshGateEvaluation(ctx, h.Tracer, h.Instruments, DomainAWSResourceMaterialization)
		if h.AffectedGraph == nil {
			gate.end(refreshGateFailOpen, 0)
		} else {
			if n, err := affected.ReposWithCloudCallersForResources(gateCtx, h.AffectedGraph, committedResourceUIDs(rows)); err != nil {
				slog.Warn("value-flow refresh gate failed open",
					"domain", DomainAWSResourceMaterialization,
					"scope_id", intent.ScopeID,
					"generation_id", intent.GenerationID,
					"outcome", refreshGateFailOpen,
					"affected_repo_count", 0,
					"error", err,
				)
				gate.end(refreshGateFailOpen, 0)
			} else if n == 0 {
				count = 0
				gate.end(refreshGateSuppressed, 0)
			} else {
				count = float64(n)
				gate.end(refreshGateAffected, n)
			}
		}
	}
	return affected.WithRefreshSignal(base, count)
}

// committedResourceUIDs lists the distinct non-empty CloudResource uids of
// committed node rows for the emit gate.
func committedResourceUIDs(rows []map[string]any) []string {
	seen := make(map[string]struct{}, len(rows))
	uids := make([]string, 0, len(rows))
	for _, row := range rows {
		uid, _ := row["uid"].(string)
		if uid == "" {
			continue
		}
		if _, ok := seen[uid]; ok {
			continue
		}
		seen[uid] = struct{}{}
		uids = append(uids, uid)
	}
	return uids
}
