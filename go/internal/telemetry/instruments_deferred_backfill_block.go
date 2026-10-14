// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// registerDeferredBackfillBlockInstruments registers the per-block duration
// histogram of the whole deferred maintenance pass (#8082) on inst.
//
// eshu_dp_deferred_backfill_block_duration_seconds{block}: one sample per block
// per pass. The block label is the closed nine-value set of
// DeferredBackfillBlock* (catalog_load, params_build, partition_list,
// memo_gate, fact_load, discover, write_batches, fanin, reopen) and is the only
// label: no scope, repository, or reopen-domain value. The reopen block
// records once per domain call. A block that fails records no sample; its span
// carries the error. The buckets reach 300 s because the pass has taken 62 to
// 99 s with one block at 39 s.
func registerDeferredBackfillBlockInstruments(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.DeferredBackfillBlockDuration, err = meter.Float64Histogram(
		"eshu_dp_deferred_backfill_block_duration_seconds",
		metric.WithDescription("Wall time of one block of the whole deferred relationship maintenance pass, labeled by block (catalog_load, params_build, partition_list, memo_gate, fact_load, discover, write_batches, fanin, reopen). One sample per block per pass; reopen records once per domain call. Splits eshu_dp_deferred_backfill_duration_seconds, which covers only the whole pass, so an operator can name the block that owns a slow pass. A failed block records no sample."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.01, 0.05, 0.1, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300),
	); err != nil {
		return fmt.Errorf("register DeferredBackfillBlockDuration histogram: %w", err)
	}
	return nil
}
