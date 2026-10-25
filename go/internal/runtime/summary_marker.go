// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"math"
	"strconv"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/status/summary"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// writeSummaryMarkers writes the stale and age gauges of every stored status
// model the scrape reads: the active-work summary behind the stage, queue, and
// domain gauges, and the coordinator snapshot behind the eshu_runtime_coordinator_*
// gauges (#8828). Each is written under its own model_key, so a stalled writer
// shows on whichever model it stopped refreshing.
func writeSummaryMarkers(
	writeGauge func(name string, labels map[string]string, value string),
	serviceName string,
	report statuspkg.Report,
) {
	writeSummaryMarker(writeGauge, serviceName, summary.ModelActiveWorkSummary, report.ActiveWorkSource)
	writeSummaryMarker(writeGauge, serviceName, summary.ModelCoordinatorSnapshot, report.CoordinatorSource)
}

// writeSummaryMarker says whether the gauges of the stored model modelKey above
// (active-work for the stage, queue, and domain gauges; coordinator_snapshot for
// the eshu_runtime_coordinator_* gauges, #8828) came from a fresh stored row. It writes nothing unless the stored-summary reader answered, so a
// scrape with the reader off is byte-identical to one from before the reader.
func writeSummaryMarker(
	writeGauge func(name string, labels map[string]string, value string),
	serviceName string,
	modelKey string,
	source statuspkg.ActiveWorkSource,
) {
	switch source.Source {
	case statuspkg.ActiveWorkSourceModel, statuspkg.ActiveWorkSourceLastRow, statuspkg.ActiveWorkSourceZero:
	default:
		return
	}
	labels := map[string]string{
		"service_name":                    serviceName,
		telemetry.MetricDimensionModelKey: modelKey,
	}
	stale, age := "0", strconv.FormatFloat(source.Age.Seconds(), 'f', -1, 64)
	if source.Stale {
		stale = "1"
	}
	if source.Source == statuspkg.ActiveWorkSourceZero {
		// No row was ever decodable, so there is no age: NaN is the exposition
		// format's value for it, and PromQL comparisons drop it.
		age = strconv.FormatFloat(math.NaN(), 'f', -1, 64)
	}
	writeGauge(telemetry.RuntimeStatusSummaryStaleMetric, labels, stale)
	writeGauge(telemetry.RuntimeStatusSummaryAgeSecondsMetric, labels, age)
}
