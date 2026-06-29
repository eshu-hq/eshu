// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "context"

func (h *CodeHandler) filterCrossRepoDeadCodeResultsWithoutProducerLocalIncomingEdges(
	ctx context.Context,
	results []map[string]any,
	label string,
) ([]map[string]any, error) {
	if len(results) == 0 {
		return results, nil
	}
	incoming, err := h.legacyDeadCodeIncomingEntityIDs(ctx, results)
	if err != nil {
		return nil, err
	}
	graphIncoming, err := h.deadCodeResultsWithGraphIncomingEdges(
		ctx,
		deadCodeResultsNeedingGraphIncomingProbe(results, label),
		label,
	)
	if err != nil {
		return nil, err
	}
	return applyDeadCodeIncomingEdges(results, incoming, graphIncoming), nil
}
