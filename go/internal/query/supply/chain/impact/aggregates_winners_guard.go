// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract/packet"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// ErrAggregateReadModelUnavailable is the sentinel a winners-served
// aggregate call answers when the maintained
// supply_chain_impact_canonical_winners read model cannot serve an exact
// answer: the gate is on for a winners-eligible filter but the maintainer
// watermark is absent (never populated), older than WinnersFreshnessWindow,
// or its probe failed. The handler answers the 503 backend_unavailable
// envelope; there is no fallback to the legacy canonical-facts CTE, which
// is the shape that misses the relay bound on QA-sized data. Exact totals
// and buckets cannot be labeled partial the way grouped buckets can, so an
// unavailable model fails visibly instead of serving an approximate answer.
var ErrAggregateReadModelUnavailable = errors.New(
	"supply chain impact aggregate winners read model is unavailable",
)

// AggregateFreshness reports how fresh the evidence behind an aggregate
// count or inventory answer is, using the readiness freshness labels
// (packet.FreshnessLabelFresh and packet.FreshnessLabelUnknown). Only fresh
// answers are served: a stale or unprobed winners model answers
// ErrAggregateReadModelUnavailable instead. The legacy live-facts read is
// always current, so it reports fresh with no watermark timestamp.
type AggregateFreshness struct {
	State            string `json:"state"`
	LatestObservedAt string `json:"latest_observed_at,omitempty"`
}

// AggregateInventoryPage is one page of inventory buckets plus the
// freshness of the evidence behind it.
type AggregateInventoryPage struct {
	Rows      []InventoryRow
	Freshness AggregateFreshness
}

// aggregateWinnersProbeFailedEvent is the event_name of the one WARN line a
// failed aggregate winners watermark probe emits. The operator also sees the
// 503 backend_unavailable answer the handler serves for the call.
const aggregateWinnersProbeFailedEvent = "supply_chain_impact.aggregate_winners_probe_failed"

// aggregateWinnersProbeErrorMaxBytes bounds the error text on that WARN line.
const aggregateWinnersProbeErrorMaxBytes = 256

// resolveAggregateFreshness decides how one aggregate call is served. It
// reports serveWinners=true with the watermark freshness when the filter is
// winners-eligible and the model is present and fresh. It returns
// ErrAggregateReadModelUnavailable when the filter is winners-eligible but
// the watermark is absent, stale, or unprobed. Otherwise (the legacy path:
// gate off, scoped-token grants, or existence filters) it reports
// serveWinners=false with a fresh, unwatermarked freshness and issues no
// probe, so the legacy read costs exactly one statement as before. A granted
// call with the gate on is answered from the supports table (#10880 PR-B2a): it
// probes the supports readiness predicate and refuses with
// ErrAggregateReadModelUnavailable (also matching ErrFindingSupportsUnavailable)
// while the table cannot serve an exact answer; the supports table is current by
// construction, so it reports fresh with no watermark.
func (s PostgresAggregateStore) resolveAggregateFreshness(
	ctx context.Context,
	filter AggregateFilter,
	readAt time.Time,
) (freshness AggregateFreshness, serveWinners bool, err error) {
	if s.readFromSupportsFor(filter) {
		if err := s.requireAggregateSupportsReady(ctx); err != nil {
			return AggregateFreshness{}, false, err
		}
		return AggregateFreshness{State: packet.FreshnessLabelFresh}, false, nil
	}
	if !s.readFromWinnersFor(filter) {
		return AggregateFreshness{State: packet.FreshnessLabelFresh}, false, nil
	}
	probe := PostgresFindingStore{DB: s.DB, reader: s.reader, ReadFromWinners: true}
	watermark, probeErr := probe.SupplyChainImpactWinnersWatermark(ctx)
	if probeErr != nil {
		s.logAggregateWinnersProbeFailure(ctx, probeErr)
		return AggregateFreshness{}, false, ErrAggregateReadModelUnavailable
	}
	if !watermark.Present {
		return AggregateFreshness{}, false, ErrAggregateReadModelUnavailable
	}
	freshness = AggregateFreshness{
		State:            packet.FreshnessLabelFresh,
		LatestObservedAt: watermark.MaterializedAt.UTC().Format(time.RFC3339Nano),
	}
	if readAt.Sub(watermark.MaterializedAt.UTC()) > WinnersFreshnessWindow {
		return AggregateFreshness{}, false, ErrAggregateReadModelUnavailable
	}
	return freshness, true, nil
}

func (s PostgresAggregateStore) logAggregateWinnersProbeFailure(ctx context.Context, err error) {
	if s.Logger == nil {
		return
	}
	text := err.Error()
	if len(text) > aggregateWinnersProbeErrorMaxBytes {
		text = text[:aggregateWinnersProbeErrorMaxBytes]
	}
	s.Logger.LogAttrs(
		ctx, slog.LevelWarn, "supply chain impact aggregate winners probe failed",
		telemetry.EventAttr(aggregateWinnersProbeFailedEvent),
		slog.String("error", text),
	)
}

// readFromSupportsFor reports whether one aggregate call is answered from the
// per-support table: the gate is on and the call carries a repository or scope
// grant. The winners row is one pick across scopes and cannot answer a grant
// (arbiter ruling for #10880 PR-B, decision 1).
func (s PostgresAggregateStore) readFromSupportsFor(filter AggregateFilter) bool {
	return s.ReadFromWinners &&
		(len(filter.AllowedRepositoryIDs) > 0 || len(filter.AllowedScopeIDs) > 0)
}

// requireAggregateSupportsReady refuses a supports-served aggregate while the
// table is not ready, wrapping both the aggregate and the supports sentinels so
// the handler can name the model that refused. A probe failure refuses too.
func (s PostgresAggregateStore) requireAggregateSupportsReady(ctx context.Context) error {
	probe := PostgresFindingStore{DB: s.DB, reader: s.reader, ReadFromWinners: true}
	if err := probe.requireSupportsReady(ctx); err != nil {
		s.logAggregateWinnersProbeFailure(ctx, err)
		return fmt.Errorf("%w: %w", ErrAggregateReadModelUnavailable, err)
	}
	return nil
}
