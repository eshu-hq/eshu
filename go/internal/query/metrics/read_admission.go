// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package metrics

import (
	"context"
)

// MetricsReadAdmissionContractVersion versions the #9669 O6
// admission-contract shape for the metrics time-series read (GET
// /api/v0/metrics/timeseries). Canonical reads persist through
// explicit core-owned admission contracts: eshu-api keeps the HTTP
// shape and calls this versioned port, which the core-owned time
// series source satisfies, composed by the consumer. A breaking
// change to the read shape fails the frozen method-set test until
// that list is deliberately updated; bump this version alongside
// (manually — the bump is convention, the frozen list is the
// enforcement) so dest consumers can pin the shape.
const MetricsReadAdmissionContractVersion = "v0"

// MetricsReadAdmission is the versioned admission-contract port the
// API handler will depend on instead of the shared handle. It
// declares exactly the one read GET /api/v0/metrics/timeseries
// admits through — the bounded time-series range read (RangeQuery,
// mirroring TimeSeriesSource) — so the boundary is held honest by
// the compile-time mirror assertion below plus the frozen method-set
// contract test. It will back the eshu-api extraction handoff for
// this read. The seam is read-only on purpose: the declared method
// is a bounded read and the port carries no mutation surface, so a
// dest consumer implementing only this contract can serve the
// time-series read but cannot acquire a write it did not bargain
// for through it.
//
// D-wave consumer: O6 pin 2 (per the arbiter O6 ruling minting
// order). The satellite records this pin in a follow-up; a
// satellite PR consuming an unminted pin is invalid. Arbiter review
// of this pin precedes satellite termination of the metrics
// time-series leg.
//
// Narrow by construction, recorded not silently assumed: the method
// re-declares the TimeSeriesSource signature in this Metrics-scoped
// shape instead of embedding or aliasing TimeSeriesSource. This pin
// therefore grants nothing to the other families that read through
// graph or content ports — the code-graph-metrics POST leg and every
// other GraphQuery consumer keep their own per-route relay reasons
// until deliberate pins cover them.
//
// Explicitly OUT, recorded not silently assumed:
//
//   - The code-graph-metrics POST (POST /api/v0/code/call-graph/metrics)
//     is a GraphQuery reader (CodeHandler.CallGraphMetricsData reads via
//     h.Neo4j.Run), not a TimeSeriesSource consumer: it stays outside
//     this pin and keeps its own per-route relay reason.
//   - The grant check, the metric allow-list, and the fixed failure
//     messages are handler behavior, not port shape: there is no
//     method set to freeze, the same standing as QueryProfile in this
//     handler and Clock/FinalizeTimeout in the mutation pins.
//
// Rollback: revert this pin. The seam is unwired — no handler,
// route, or caller depends on it yet — so rollback moves no traffic
// and changes no data.
type MetricsReadAdmission interface {
	RangeQuery(ctx context.Context, query RangeQuery) ([]Point, error)
}

// The mirror assertion keeps the Metrics-scoped re-declaration
// honest, and it works in one direction only: removal, rename, or
// re-signing of the mirrored method on the shared handle fails
// compilation here. Shared-handle addition deliberately does not
// widen this pin — a new shared-handle method is adopted only by
// deliberate re-declaration plus a frozen-list update. It pins no
// shared handle — the assertion names the handle only as the shape
// this pin mirrors, and no other family may cite this pin as
// coverage.
var _ MetricsReadAdmission = TimeSeriesSource(nil)
