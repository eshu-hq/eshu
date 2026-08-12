// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

type activeCICDWorkflowImageFactLoader interface {
	ListActiveCICDWorkflowImageFacts(ctx context.Context, repositoryIDs []string) ([]facts.Envelope, error)
}

type activeCICDRunCorrelationFactLoader interface {
	ListActiveCICDRunCorrelationFacts(ctx context.Context, digests []string, imageRefs []string) ([]facts.Envelope, error)
}

// cicdRunRepositoryIDs extracts the distinct repository owners named by valid
// ci.run facts. Decode failures are left for the handler's main classification
// pass so one malformed fact is quarantined exactly once.
func cicdRunRepositoryIDs(envelopes []facts.Envelope) []string {
	repositoryIDs := make([]string, 0)
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.CICDRunFactKind {
			continue
		}
		run, err := decodeCICDRun(envelope)
		if err != nil {
			continue
		}
		if repositoryID := trimmedCICDPtr(run.RepositoryID); repositoryID != "" {
			repositoryIDs = append(repositoryIDs, repositoryID)
		}
	}
	return uniqueSortedStrings(repositoryIDs)
}

func (h CICDRunCorrelationHandler) loadActiveCICDWorkflowImageFacts(
	ctx context.Context,
	repositoryIDs []string,
) ([]facts.Envelope, error) {
	if len(repositoryIDs) == 0 {
		return nil, nil
	}
	loader, ok := h.FactLoader.(activeCICDWorkflowImageFactLoader)
	if !ok {
		return nil, nil
	}
	loaded, err := loader.ListActiveCICDWorkflowImageFacts(ctx, repositoryIDs)
	if err != nil {
		return nil, classifyFactLoadError(err)
	}
	return filterCICDWorkflowImageFactsForRepositories(loaded, repositoryIDs), nil
}

// crossScopeIdentityLookupPlanned reports whether this pass will actually ask
// the cross-scope container-image-identity loader anything.
//
// Both false cases matter to the #5709 readiness floor, because both produce an
// empty result that says nothing about the producer:
//
//   - No loader implementing the cross-scope seam. loadActiveCICDRunCorrelationFacts
//     returns no envelopes without querying, exactly like an unwired readiness
//     seam.
//   - No digests and no image refs. FactStore.ListActiveCICDRunCorrelationFacts
//     short-circuits an empty filter to no rows
//     (container_image_identity_support_fact_loader.go), so a CI run that
//     published no container artifacts -- normal for any repository whose CI
//     never builds images -- can never resolve anything here.
//
// Gating the floor on this is what keeps such a run from deferring for the full
// crossScopeProducerReadinessMaxWait on a non-backing-off retry: its class
// freezes attempt_count, so the exponential term never grows and the row would
// re-claim every 30 seconds for 30 minutes, per repair cycle, to look up
// nothing.
//
// This is the single predicate for both the load and the floor so the two
// cannot disagree about whether a lookup happened. ciArtifactDigests and
// ciWorkflowImageRefs already trim blanks and deduplicate, which is what
// FactStore's own filter normalization does, so the emptiness test here matches
// the one the storage layer applies.
func (h CICDRunCorrelationHandler) crossScopeIdentityLookupPlanned(digests []string, imageRefs []string) bool {
	if _, ok := h.FactLoader.(activeCICDRunCorrelationFactLoader); !ok {
		return false
	}
	return len(digests) > 0 || len(imageRefs) > 0
}

func (h CICDRunCorrelationHandler) loadActiveCICDRunCorrelationFacts(
	ctx context.Context,
	digests []string,
	imageRefs []string,
) ([]facts.Envelope, error) {
	if !h.crossScopeIdentityLookupPlanned(digests, imageRefs) {
		return nil, nil
	}
	loader, ok := h.FactLoader.(activeCICDRunCorrelationFactLoader)
	if !ok {
		return nil, nil
	}
	envelopes, err := loader.ListActiveCICDRunCorrelationFacts(ctx, digests, imageRefs)
	if err != nil {
		return nil, classifyFactLoadError(err)
	}
	return envelopes, nil
}

// filterCICDWorkflowImageFactsForRepositories is the reducer-side ownership
// fence for the cross-scope storage seam. The Postgres query is owner-bounded
// for performance, while this typed check keeps any alternative or faulty
// adapter from attaching foreign workflow evidence to a run. Decode failures
// stay in the batch so the quarantine-aware core can classify them per fact.
func filterCICDWorkflowImageFactsForRepositories(
	loaded []facts.Envelope,
	repositoryIDs []string,
) []facts.Envelope {
	requested := make(map[string]struct{}, len(repositoryIDs))
	for _, repositoryID := range repositoryIDs {
		requested[repositoryID] = struct{}{}
	}
	seenFactIDs := make(map[string]struct{}, len(loaded))
	filtered := make([]facts.Envelope, 0, len(loaded))
	for _, envelope := range loaded {
		if envelope.FactKind != facts.CICDWorkflowImageEvidenceFactKind {
			continue
		}
		if _, duplicate := seenFactIDs[envelope.FactID]; duplicate {
			continue
		}
		evidence, err := decodeCICDWorkflowImageEvidence(envelope)
		if err == nil {
			repositoryID := trimmedCICDField(evidence.RepositoryID)
			if _, ok := requested[repositoryID]; !ok {
				continue
			}
		}
		seenFactIDs[envelope.FactID] = struct{}{}
		filtered = append(filtered, envelope)
	}
	return filtered
}
