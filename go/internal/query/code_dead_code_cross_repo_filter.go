// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

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

func filterCrossRepoDeadCodeEvidence(
	evidence []crossRepoDeadCodeEvidence,
	allowedConsumers map[string]struct{},
	access repositoryAccessFilter,
) ([]crossRepoDeadCodeEvidence, []crossRepoDeadCodeEvidence) {
	visible := make([]crossRepoDeadCodeEvidence, 0, len(evidence))
	hidden := make([]crossRepoDeadCodeEvidence, 0)
	for _, row := range evidence {
		if row.NeedsEvidence && row.ConsumerRepoID == "" {
			visible = append(visible, row)
			continue
		}
		if len(allowedConsumers) > 0 {
			if _, ok := allowedConsumers[row.ConsumerRepoID]; !ok {
				continue
			}
		}
		if !access.AllowsRepositoryID(row.ConsumerRepoID) {
			hidden = append(hidden, row)
			continue
		}
		visible = append(visible, row)
	}
	return visible, hidden
}

func crossRepoDeadCodeUnknownReasons(
	row map[string]any,
	evidence []crossRepoDeadCodeEvidence,
	hiddenCount int,
	evidenceAvailable bool,
) []string {
	reasons := make([]string, 0)
	if !evidenceAvailable {
		reasons = append(reasons, "cross_repo_evidence_unavailable")
	}
	if hiddenCount > 0 {
		reasons = append(reasons, "permission_hidden_consumer")
	}
	if row["classification"] == deadCodeClassificationAmbiguous {
		reasons = append(reasons, "candidate_ambiguous")
	}
	for _, item := range evidence {
		if item.NeedsEvidence || item.Ambiguous || !strings.EqualFold(item.GenerationStatus, "active") {
			reason := strings.TrimSpace(item.Reason)
			if reason == "" && !strings.EqualFold(item.GenerationStatus, "active") {
				reason = "stale_generation"
			}
			if reason == "" {
				reason = "needs_evidence"
			}
			reasons = append(reasons, reason)
			continue
		}
		if item.Confidence <= codeprovenance.Confidence(codeprovenance.MethodRepoUniqueName) {
			reasons = append(reasons, "ambiguous_consumer_ownership")
		}
	}
	slices.Sort(reasons)
	return slices.Compact(reasons)
}
