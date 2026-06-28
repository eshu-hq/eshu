// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/query"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/metric"
)

const (
	// graphCountLimitEnv caps the number of distinct groups returned by the
	// provenance coverage gauges (eshu_dp_edges_by_source_tool,
	// eshu_dp_files_by_language). It bounds returned label cardinality, NOT the
	// rows counted — per-group counts stay exact. The closed source_tool and
	// language vocabularies are small, so the cap is a safety valve rather than a
	// limit reached in practice.
	graphCountLimitEnv     = "ESHU_GRAPH_COUNT_LIMIT"
	defaultGraphCountLimit = 10_000
)

// registerProvenanceCoverageGauges wires the extraction-provenance coverage
// gauges (edges by source_tool, files by language) to the graph read port. Both
// are exact, index-answered counts (the edge gauge sums per-relationship-type
// aggregates; the file gauge is a File-label group), so they give a sound drift
// signal: a series dropping to zero means extraction stopped emitting that tool
// or language, not a sampling artifact. The group cap bounds label cardinality
// only. It is a no-op when graphReader is nil so binaries without a graph read
// port skip the gauges.
func registerProvenanceCoverageGauges(
	instruments *telemetry.Instruments,
	meter metric.Meter,
	graphReader query.GraphQuery,
	getenv func(string) string,
) error {
	if graphReader == nil {
		return nil
	}
	store := sourcecypher.NewProvenanceCountStore(graphReader)
	store.GroupLimit = loadPositiveIntOrDefault(getenv, graphCountLimitEnv, defaultGraphCountLimit)
	if err := telemetry.RegisterEdgesBySourceToolObservableGauge(instruments, meter, store); err != nil {
		return fmt.Errorf("register edges by source_tool observable gauge: %w", err)
	}
	if err := telemetry.RegisterFilesByLanguageObservableGauge(instruments, meter, store); err != nil {
		return fmt.Errorf("register files by language observable gauge: %w", err)
	}
	return nil
}
