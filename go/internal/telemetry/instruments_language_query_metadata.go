// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// MetricDimensionMetadataPath labels eshu_dp_language_query_metadata_rows_total
// with the content read that enriched a language-query row (#7247). The value
// set is closed: anchored (one entity_id = ANY lookup keyed by the graph rows'
// own ids) or page_read (the filtered content page merged by path, type, name
// and start line, which is not keyed to the graph rows).
const MetricDimensionMetadataPath = "metadata_path"

// AttrMetadataPath returns a metadata_path attribute. v must be anchored or
// page_read.
func AttrMetadataPath(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionMetadataPath, v)
}

// registerLanguageQueryMetadataInstruments registers the language-query
// content-metadata row counter on inst (#7247).
func registerLanguageQueryMetadataInstruments(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.LanguageQueryMetadataRows, err = meter.Int64Counter(
		"eshu_dp_language_query_metadata_rows_total",
		metric.WithDescription("Graph rows POST /api/v0/code/language-query sent to content-metadata enrichment, by metadata_path (anchored: one entity_id lookup keyed by the graph rows; page_read: the filtered content page merged by path/type/name/line) and outcome (enriched: the row merged content metadata; missing: the row merged no content metadata -- no matching content row, a content row with no metadata beyond fingerprint keys, or on anchored a content row in another repository)"),
	); err != nil {
		return fmt.Errorf("register LanguageQueryMetadataRows counter: %w", err)
	}
	return nil
}
