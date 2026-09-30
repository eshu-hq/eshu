// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// registerSearchDocumentGenerationSuperseded registers the search-document
// abandon counter (#7458) on inst. It is separate from
// eshu_dp_superseded_generation_fence_total because that counter's contract is
// projector failure_class values, and an abandoned search-document projection
// carries no failure class: a routine supersede is acked succeeded.
func registerSearchDocumentGenerationSuperseded(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.SearchDocumentGenerationSuperseded, err = meter.Int64Counter(
		"eshu_dp_search_document_generation_superseded_total",
		metric.WithDescription("Search-document projections abandoned because their generation was superseded, by phase (page, finalize) (#7458)"),
	); err != nil {
		return fmt.Errorf("register SearchDocumentGenerationSuperseded counter: %w", err)
	}
	return nil
}
