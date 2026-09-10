// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/reducer/packages/correlation"
)

func (h SupplyChainImpactHandler) loadActiveSupplyChainImpactRepositoryFacts(
	ctx context.Context,
	envelopes []facts.Envelope,
) ([]facts.Envelope, error) {
	loader, ok := h.FactLoader.(correlation.ActiveRepositoryFactLoader)
	if !ok || correlation.HasPackageSourceRepositoryFact(envelopes) {
		return nil, nil
	}
	if _, ok := h.FactLoader.(correlation.ActivePackageManifestDependencyFactLoader); !ok {
		return nil, nil
	}
	filter := supplyChainImpactManifestDependencyFilter(envelopes)
	if len(filter.Ecosystems) == 0 || len(filter.PackageNames) == 0 {
		return nil, nil
	}
	repositories, err := loader.ListActiveRepositoryFacts(ctx)
	if err != nil {
		return nil, factload.ClassifyFactLoadError(err)
	}
	return repositories, nil
}
