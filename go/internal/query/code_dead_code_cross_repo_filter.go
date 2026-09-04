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

// crossRepoDeadCodeConsumerReadPlan decides how one request's consumer-evidence
// lookup is bounded, and reports false when it cannot be bounded at all.
//
// A request that names consumers binds those, because that is where its row cap
// belongs: with a grant of several repositories and a selector naming one, the
// page used to be cut from the whole grant and the requested consumer could
// fall off the end of it. consumerRepoIDs is already resolved through the grant
// by applyRepositorySelectorForCapability; intersecting again is the belt to
// that braces, and a scoped caller left with nothing gets no read rather than
// the unbounded statement an empty list renders.
//
// Such a request also skips the signal read. That read exists to count
// consumers outside the grant, and filterCrossRepoDeadCodeEvidence drops every
// signal row outside the selector before counting -- while every selector entry
// the grant admits is, by definition, inside the grant. Its contribution is
// empty by construction, so not running it removes a whole traversal rather
// than relaxing an answer.
func crossRepoDeadCodeConsumerReadPlan(
	access repositoryAccessFilter,
	consumerRepoIDs []string,
) (crossRepoDeadCodeConsumerReads, bool) {
	if len(consumerRepoIDs) > 0 {
		page := consumerRepoIDs
		if access.Scoped() {
			page = grantedCrossRepoDeadCodeConsumerIDs(access, consumerRepoIDs)
			if len(page) == 0 {
				return crossRepoDeadCodeConsumerReads{}, false
			}
		}
		return crossRepoDeadCodeConsumerReads{PageRepositoryIDs: page}, true
	}
	if !access.Scoped() {
		return crossRepoDeadCodeConsumerReads{}, true
	}
	grant := access.RepositorySearchIDs()
	if len(grant) == 0 {
		return crossRepoDeadCodeConsumerReads{}, false
	}
	return crossRepoDeadCodeConsumerReads{PageRepositoryIDs: grant, Signal: true}, true
}

// grantedCrossRepoDeadCodeConsumerIDs keeps the requested consumers the grant
// admits, preserving the request's order so the bound array stays deterministic.
func grantedCrossRepoDeadCodeConsumerIDs(
	access repositoryAccessFilter,
	consumerRepoIDs []string,
) []string {
	granted := make([]string, 0, len(consumerRepoIDs))
	for _, repoID := range consumerRepoIDs {
		if access.AllowsRepositoryID(repoID) {
			granted = append(granted, repoID)
		}
	}
	return granted
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
