// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"errors"
	"fmt"

	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// ErrFindingSupportsUnavailable is the sentinel a granted impact-findings read
// answers when the winners gate is on and the per-support table cannot serve it
// exactly: the readiness predicate (marker complete at this binary's contract
// version, no marker for a higher version, empty old-writer fence) is false, or
// the probe failed, including a table that does not exist yet. The handler
// answers the 503 backend_unavailable envelope. There is no fallback: the winners
// row is one pick across scopes and cannot answer a grant, and the legacy fact
// read is the shape the supports table retires (arbiter ruling for #10880 PR-B,
// decision 1, "no silent fallback").
var ErrFindingSupportsUnavailable = errors.New(
	"supply chain impact findings supports read model is unavailable",
)

// readsFromSupports reports whether a gate-on list call is answered from the
// per-support table. Today that is every granted call: a repository or scope
// grant applies to the support row, which carries its own scope and repository,
// and the winners row cannot answer it.
func (f FindingFilter) readsFromSupports() bool {
	return len(f.AllowedRepositoryIDs) > 0 || len(f.AllowedScopeIDs) > 0
}

// requireSupportsReady refuses a supports-served call while the table is not
// ready. A probe failure refuses too: a readiness error must never read as ready.
func (s PostgresFindingStore) requireSupportsReady(ctx context.Context) error {
	ready, err := storagepostgres.SupplyChainImpactSupportsReady(ctx, s.queryer())
	if err != nil {
		return fmt.Errorf("%w: %w", ErrFindingSupportsUnavailable, err)
	}
	if !ready {
		return ErrFindingSupportsUnavailable
	}
	return nil
}

// SupplyChainImpactFreshness reports the freshness of the read model that
// answers filter. A call answered from the supports table is current by
// construction (the maintainer writes supports in the finding facts'
// transaction), so it reports no winners freshness and the handler leaves the
// truth envelope fresh. Every other call reports the winners watermark exactly as
// SupplyChainImpactWinnersWatermark does.
func (s PostgresFindingStore) SupplyChainImpactFreshness(ctx context.Context, filter FindingFilter) (WinnersFreshness, error) {
	if s.ReadFromWinners && filter.readsFromSupports() {
		return WinnersFreshness{}, nil
	}
	return s.SupplyChainImpactWinnersWatermark(ctx)
}
