// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crossplane

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
)

// crossplaneSatisfiedByFactLoader loads, on one consistent read, everything
// one materialization resolves: the intent scope's active Claim candidates
// and every scope's active CrossplaneXRD content entities (#10005). A store
// whose active generation can be a git delta implements it so a delta reads
// overlaid on its base full generation: the handler retracts every edge of
// the scope before it writes, so a load that returned only the delta's own
// rows would delete the edges of unchanged claims and of claims that resolve
// against an XRD in an untouched file.
// postgres.FactStore.ListActiveCrossplaneSatisfiedByFacts implements it.
type crossplaneSatisfiedByFactLoader interface {
	ListActiveCrossplaneSatisfiedByFacts(ctx context.Context, scopeID string) ([]facts.Envelope, error)
}

// crossplaneXRDFactLoader loads the cross-scope active CrossplaneXRD
// content-entity facts a Claim candidate resolves against. XRDs commonly live
// in a platform repo separate from the Claims that reference them, so they
// are loaded across scopes exactly like kubernetesCorrelationSourceFactLoader
// loads cross-scope OCI source evidence. It is the fallback for a loader that
// does not implement crossplaneSatisfiedByFactLoader.
type crossplaneXRDFactLoader interface {
	ListActiveCrossplaneXRDFacts(ctx context.Context) ([]facts.Envelope, error)
}

// loadEdgeFacts loads the facts one materialization resolves. A loader that
// implements crossplaneSatisfiedByFactLoader returns the scope's active Claim
// candidates and every active XRD, each delta overlaid on its base. The
// runtime's generation freshness guard runs the handler only while the
// intent's generation is the scope's active one: Runtime.execute defers a
// pending generation and, for this scope-keyed domain, rebinds a superseded
// intent onto the active generation (#10106), so the scope's active
// generation is the intent's by the time this load runs. Any other loader
// keeps the generation-keyed
// path: the intent generation's own content_entity facts plus the
// cross-scope active XRDs, mirroring
// KubernetesCorrelationMaterializationHandler.loadEdgeFacts.
func (h CrossplaneSatisfiedByMaterializationHandler) loadEdgeFacts(
	ctx context.Context,
	intent reducercontract.Intent,
) ([]facts.Envelope, error) {
	if loader, ok := h.FactLoader.(crossplaneSatisfiedByFactLoader); ok {
		envelopes, err := loader.ListActiveCrossplaneSatisfiedByFacts(ctx, intent.ScopeID)
		if err != nil {
			return nil, fmt.Errorf("load facts for crossplane satisfied-by materialization: %w", factload.ClassifyFactLoadError(err))
		}
		return envelopes, nil
	}
	envelopes, err := factload.LoadFactsForKinds(
		ctx,
		h.FactLoader,
		intent.ScopeID,
		intent.GenerationID,
		[]string{factload.FactKindContentEntity},
	)
	if err != nil {
		return nil, fmt.Errorf("load facts for crossplane satisfied-by materialization: %w", err)
	}
	xrdFacts, err := h.loadActiveXRDFacts(ctx)
	if err != nil {
		return nil, fmt.Errorf("load active crossplane xrd facts: %w", err)
	}
	return append(envelopes, xrdFacts...), nil
}

// loadActiveXRDFacts loads the cross-scope active CrossplaneXRD facts through
// the optional crossplaneXRDFactLoader interface. A loader that does not
// implement it yields no XRD facts (and therefore no edges), never an error.
func (h CrossplaneSatisfiedByMaterializationHandler) loadActiveXRDFacts(ctx context.Context) ([]facts.Envelope, error) {
	loader, ok := h.FactLoader.(crossplaneXRDFactLoader)
	if !ok {
		return nil, nil
	}
	envelopes, err := loader.ListActiveCrossplaneXRDFacts(ctx)
	if err != nil {
		return nil, factload.ClassifyFactLoadError(err)
	}
	return envelopes, nil
}
