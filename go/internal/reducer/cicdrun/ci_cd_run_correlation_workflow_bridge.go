// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cicdrun

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/schemadecode"
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
		run, err := schemadecode.DecodeCICDRun(envelope)
		if err != nil {
			continue
		}
		if repositoryID := TrimmedCICDPtr(run.RepositoryID); repositoryID != "" {
			repositoryIDs = append(repositoryIDs, repositoryID)
		}
	}
	return payloadcore.UniqueSortedStrings(repositoryIDs)
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
		return nil, factload.ClassifyFactLoadError(err)
	}
	return filterCICDWorkflowImageFactsForRepositories(loaded, repositoryIDs), nil
}

// crossScopeIdentityLookup resolves the cross-scope container-image-identity
// loader this pass will actually query, or nil when the pass will ask nothing.
//
// Both nil cases matter to the #5709 readiness floor, because both produce an
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
// Returning the loader rather than a bool is what keeps the load and the floor
// from disagreeing about whether a lookup happened: Handle resolves this once
// and hands the same value to both, so there is no second type assertion that a
// later condition change could leave answering differently. ciArtifactDigests
// and ciWorkflowImageRefs already trim blanks and deduplicate, which is what
// FactStore's own filter normalization does, so the emptiness test here matches
// the one the storage layer applies.
func (h CICDRunCorrelationHandler) crossScopeIdentityLookup(
	digests []string,
	imageRefs []string,
) activeCICDRunCorrelationFactLoader {
	loader, ok := h.FactLoader.(activeCICDRunCorrelationFactLoader)
	if !ok {
		return nil
	}
	if len(digests) == 0 && len(imageRefs) == 0 {
		return nil
	}
	return loader
}

// loadActiveCICDRunCorrelationFacts reads the cross-scope container-image
// identity facts through the loader crossScopeIdentityLookup resolved. A nil
// loader means no lookup was planned, and the empty result is reported without
// querying anything.
func loadActiveCICDRunCorrelationFacts(
	ctx context.Context,
	loader activeCICDRunCorrelationFactLoader,
	digests []string,
	imageRefs []string,
) ([]facts.Envelope, error) {
	if loader == nil {
		return nil, nil
	}
	envelopes, err := loader.ListActiveCICDRunCorrelationFacts(ctx, digests, imageRefs)
	if err != nil {
		return nil, factload.ClassifyFactLoadError(err)
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
		evidence, err := schemadecode.DecodeCICDWorkflowImageEvidence(envelope)
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
