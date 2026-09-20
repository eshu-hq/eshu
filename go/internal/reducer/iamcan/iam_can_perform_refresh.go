// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iamcan

import (
	"context"
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/reducer/code/value/affected"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// refreshResultSignals runs the value-flow refresh emit gate over the
// committed edge principal uids and merges the signal into base. It
// short-circuits to 0 without a graph read when nothing was written, and
// fails open to 1 (with a warning) when the gate is unwired or its read
// errors: a spurious refresh is a bounded extra solve, a missed one is silent
// accuracy loss.
func (h IAMCanPerformMaterializationHandler) refreshResultSignals(
	ctx context.Context,
	intent reducercontract.Intent,
	base map[string]float64,
	writes int,
	edges []map[string]any,
) map[string]float64 {
	count := 0.0
	if writes > 0 {
		count = 1
		if h.AffectedGraph != nil {
			n, err := affected.ReposWithCloudCallersForPrincipals(ctx, h.AffectedGraph, committedPrincipalUIDs(edges))
			if err != nil {
				slog.Warn("value-flow refresh gate failed open",
					"domain", reducercontract.DomainIAMCanPerformMaterialization,
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

// committedPrincipalUIDs lists the distinct non-empty principal uids of
// committed CAN_PERFORM edges for the emit gate.
func committedPrincipalUIDs(edges []map[string]any) []string {
	seen := make(map[string]struct{}, len(edges))
	uids := make([]string, 0, len(edges))
	for _, edge := range edges {
		uid, _ := edge["principal_uid"].(string)
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
