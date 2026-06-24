// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func (h SupplyChainImpactHandler) loadActiveSupplyChainImpactRepositoryFacts(
	ctx context.Context,
	envelopes []facts.Envelope,
) ([]facts.Envelope, error) {
	loader, ok := h.FactLoader.(activeRepositoryFactLoader)
	if !ok || hasPackageSourceRepositoryFact(envelopes) {
		return nil, nil
	}
	if _, ok := h.FactLoader.(activePackageManifestDependencyFactLoader); !ok {
		return nil, nil
	}
	filter := supplyChainImpactManifestDependencyFilter(envelopes)
	if len(filter.Ecosystems) == 0 || len(filter.PackageNames) == 0 {
		return nil, nil
	}
	repositories, err := loader.ListActiveRepositoryFacts(ctx)
	if err != nil {
		return nil, classifyFactLoadError(err)
	}
	return repositories, nil
}
