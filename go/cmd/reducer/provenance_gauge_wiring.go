// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/query"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/telemetry/snapshot"
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

	// graphGaugeRefreshIntervalEnv is the delay between background refreshes of
	// the graph-backed observable gauges (#7062).
	graphGaugeRefreshIntervalEnv = "ESHU_GRAPH_GAUGE_REFRESH_INTERVAL"
	// graphGaugeRefreshTimeoutEnv bounds each background graph read that feeds
	// those gauges (#7062).
	graphGaugeRefreshTimeoutEnv = "ESHU_GRAPH_GAUGE_REFRESH_TIMEOUT"

	// Closed values of the `gauge` label on the eshu_dp_gauge_snapshot_*
	// metrics; one per graph-backed gauge served from a background snapshot.
	gaugeEdgesBySourceTool = "edges_by_source_tool"
	gaugeFilesByLanguage   = "files_by_language"
	gaugeGraphOrphanNodes  = "graph_orphan_nodes"
)

// cachedEdgesBySourceTool serves eshu_dp_edges_by_source_tool from a snapshot.
type cachedEdgesBySourceTool struct{ source *snapshot.Source }

func (c cachedEdgesBySourceTool) EdgesBySourceTool(ctx context.Context) (map[string]int64, error) {
	return c.source.Counts(ctx)
}

// cachedFilesByLanguage serves eshu_dp_files_by_language from a snapshot.
type cachedFilesByLanguage struct{ source *snapshot.Source }

func (c cachedFilesByLanguage) FilesByLanguage(ctx context.Context) (map[string]int64, error) {
	return c.source.Counts(ctx)
}

// cachedGraphOrphanNodes serves eshu_dp_graph_orphan_nodes from a snapshot.
type cachedGraphOrphanNodes struct{ source *snapshot.Source }

func (c cachedGraphOrphanNodes) GraphOrphanNodeCounts(ctx context.Context) (map[string]int64, error) {
	return c.source.Counts(ctx)
}

// registerGraphBackedGauges wires every observable gauge whose value comes from
// a graph read (edges by source_tool, files by language, graph orphan nodes)
// to a background snapshot refresher instead of reading the graph inside the
// /metrics collection. A slow or stuck graph read used to hold the metrics
// collection lock and wedge every scrape (#7062); now the gauge callbacks only
// read the last published snapshot, and the refresher reads the graph on its
// own goroutines with a per-read deadline. It returns the refresher without
// starting it: the caller starts it with the process shutdown context via
// startGraphGaugeRefresher and waits for it before closing the graph driver.
//
// The provenance gauges are exact, index-answered counts (the edge gauge sums
// per-relationship-type aggregates; the file gauge is a File-label group), so a
// series dropping to zero means extraction stopped emitting that tool or
// language, not a sampling artifact. The group cap bounds label cardinality
// only. Each gauge is skipped when its read port is nil (binaries without a
// graph read port, or the orphan sweep disabled). It returns nil when no gauge
// registered a source.
func registerGraphBackedGauges(
	instruments *telemetry.Instruments,
	meter metric.Meter,
	graphReader query.GraphQuery,
	orphanObserver telemetry.GraphOrphanObserver,
	getenv func(string) string,
	logger *slog.Logger,
) (*snapshot.Refresher, error) {
	if graphReader == nil && orphanObserver == nil {
		return nil, nil
	}
	refresher, err := snapshot.New(snapshot.Config{
		Interval: loadDurationOrDefault(getenv, graphGaugeRefreshIntervalEnv, snapshot.DefaultInterval),
		Timeout:  loadDurationOrDefault(getenv, graphGaugeRefreshTimeoutEnv, snapshot.DefaultTimeout),
		Meter:    meter,
		Logger:   logger,
	})
	if err != nil {
		return nil, fmt.Errorf("build graph gauge snapshot refresher: %w", err)
	}
	if graphReader != nil {
		if err := registerProvenanceCoverageGauges(refresher, instruments, meter, graphReader, getenv); err != nil {
			return nil, err
		}
	}
	if orphanObserver != nil {
		source, err := refresher.Register(gaugeGraphOrphanNodes, orphanObserver.GraphOrphanNodeCounts)
		if err != nil {
			return nil, fmt.Errorf("register graph orphan snapshot source: %w", err)
		}
		if err := telemetry.RegisterGraphOrphanObservableGauge(instruments, meter, cachedGraphOrphanNodes{source: source}); err != nil {
			return nil, fmt.Errorf("register graph orphan observable gauge: %w", err)
		}
	}
	return refresher, nil
}

// startGraphGaugeRefresher starts refresher (a no-op for nil) under ctx, the
// process shutdown context, and returns a function that blocks until its loops
// have exited. Call that function before closing the graph driver so no
// in-flight refresh read fails against a closed driver.
func startGraphGaugeRefresher(ctx context.Context, refresher *snapshot.Refresher) (wait func()) {
	if refresher == nil {
		return func() {}
	}
	refresher.Start(ctx)
	return refresher.Wait
}

// registerProvenanceCoverageGauges registers the extraction-provenance
// coverage gauges against snapshot sources whose reads run through the graph
// read port on the refresher, never on the scrape.
func registerProvenanceCoverageGauges(
	refresher *snapshot.Refresher,
	instruments *telemetry.Instruments,
	meter metric.Meter,
	graphReader query.GraphQuery,
	getenv func(string) string,
) error {
	store := sourcecypher.NewProvenanceCountStore(graphReader)
	store.GroupLimit = loadPositiveIntOrDefault(getenv, graphCountLimitEnv, defaultGraphCountLimit)
	edges, err := refresher.Register(gaugeEdgesBySourceTool, store.EdgesBySourceTool)
	if err != nil {
		return fmt.Errorf("register edges by source_tool snapshot source: %w", err)
	}
	if err := telemetry.RegisterEdgesBySourceToolObservableGauge(instruments, meter, cachedEdgesBySourceTool{source: edges}); err != nil {
		return fmt.Errorf("register edges by source_tool observable gauge: %w", err)
	}
	files, err := refresher.Register(gaugeFilesByLanguage, store.FilesByLanguage)
	if err != nil {
		return fmt.Errorf("register files by language snapshot source: %w", err)
	}
	if err := telemetry.RegisterFilesByLanguageObservableGauge(instruments, meter, cachedFilesByLanguage{source: files}); err != nil {
		return fmt.Errorf("register files by language observable gauge: %w", err)
	}
	return nil
}
