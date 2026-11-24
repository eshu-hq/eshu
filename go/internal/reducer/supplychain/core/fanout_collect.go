// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"fmt"
	"sort"

	"go.opentelemetry.io/otel/trace"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// collectFanOutHolders gathers the holders one fan-out claim owes: the stored
// continuation list, the in-flight scopes, and the row-read holders of the
// lookup ids, minus the asking scope, sorted. Split out of fanOutHolders to
// keep that function under the repo's length limit. A failed read is recorded
// on the span and returned as a retryable fan-out error.
func (h SupplyChainImpactHandler) collectFanOutHolders(
	ctx context.Context,
	span trace.Span,
	cause string,
	intent reducercontract.Intent,
	lookupIDs, remaining []string,
) ([]string, fanOutSplitStats, error) {
	var split fanOutSplitStats
	holders := make(map[string]struct{}, len(remaining))
	for _, id := range remaining {
		holders[id] = struct{}{}
	}
	// The in-flight read comes FIRST, then the row read. A holder still claimed or
	// running at the in-flight read is flagged. One that acknowledged before it has
	// committed its rows (a pass commits before it acknowledges), so the row read
	// that follows sees them. The other order leaves a gap: a holder with no rows
	// at the first read that commits and acknowledges before the second is missed
	// by both. A pass claimed after the in-flight read loads its evidence after the
	// source's facts became visible, so it needs no flag.
	if len(lookupIDs) > 0 {
		inFlight, err := h.FanOut.FindInFlightNonMarkerScopes(ctx, intent.ScopeID)
		if err != nil {
			h.countFanOutRead(ctx, cause, err, telemetry.FanOutReadSiteInFlight)
			span.RecordError(err)
			return nil, split, fanOutError{fmt.Errorf("find in-flight supply chain impact scopes: %w", err)}
		}
		for _, id := range inFlight {
			holders[id] = struct{}{}
		}
	}
	for _, chunk := range chunkStrings(lookupIDs, fanOutHolderReadChunk) {
		found, stats, err := h.readHolderScopesSplit(ctx, cause, intent.ScopeID, chunk)
		split.add(stats)
		if err != nil {
			span.RecordError(err)
			return nil, split, err
		}
		for id := range found {
			holders[id] = struct{}{}
		}
	}
	delete(holders, intent.ScopeID)
	ordered := make([]string, 0, len(holders))
	for id := range holders {
		ordered = append(ordered, id)
	}
	sort.Strings(ordered)
	return ordered, split, nil
}
