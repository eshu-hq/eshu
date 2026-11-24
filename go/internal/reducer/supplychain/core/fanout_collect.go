// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"sort"

	"go.opentelemetry.io/otel/trace"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// collectFanOutHolders gathers the holders one fan-out claim owes: the stored
// continuation list, the in-flight claims set A does not prove new
// (addInFlightHolders, #10972 D4b), and the row-read holders of the lookup ids,
// minus the asking scope, sorted. It also returns what the in-flight arm did. Split out of fanOutHolders to
// keep that function under the repo's length limit. A failed read is recorded
// on the span and returned as a retryable fan-out error.
func (h SupplyChainImpactHandler) collectFanOutHolders(
	ctx context.Context,
	span trace.Span,
	cause string,
	intent reducercontract.Intent,
	early inFlightAtLoadEnd,
	lookupIDs, remaining []string,
	times *fanOutSiteTimes,
) ([]string, fanOutSplitStats, inFlightNarrowing, error) {
	var split fanOutSplitStats
	inFlight := newInFlightNarrowing(early)
	holders := make(map[string]struct{}, len(remaining))
	for _, id := range remaining {
		holders[id] = struct{}{}
	}
	// The in-flight read comes FIRST, then the row read; addInFlightHolders says
	// why, and which in-flight claims it flags (#10972 D4b).
	if len(lookupIDs) > 0 {
		var err error
		done := times.begin(telemetry.SupplyChainImpactFanOutSiteInFlightRead)
		inFlight, err = h.addInFlightHolders(ctx, intent, cause, early, holders, span)
		done()
		if err != nil {
			return nil, split, inFlight, err
		}
	}
	for _, chunk := range chunkStrings(lookupIDs, fanOutHolderReadChunk) {
		done := times.begin(telemetry.SupplyChainImpactFanOutSiteHolderReads)
		found, stats, err := h.readHolderScopesSplit(ctx, cause, intent.ScopeID, chunk)
		done()
		split.add(stats)
		if err != nil {
			span.RecordError(err)
			return nil, split, inFlight, err
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
	return ordered, split, inFlight, nil
}
