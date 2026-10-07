// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

// RuntimeStatusSnapshotAvailableMetric is a per-response Prometheus gauge. It
// is assembled by the runtime metrics handler rather than registered with the
// OTEL meter, because concurrent scrapes can have different status outcomes.
const RuntimeStatusSnapshotAvailableMetric = "eshu_runtime_status_snapshot_available"

// RuntimeStatusSummaryStaleMetric is a per-response Prometheus gauge, like
// RuntimeStatusSnapshotAvailableMetric, labeled by service_name and model_key.
// It is 1 when the scrape served the newest decodable active-work summary row
// (a stale one, aged to the read) or the zero summary instead of a fresh stored
// row, and 0 when it served a fresh row. It is rendered only while
// ESHU_STATUS_SUMMARY_READ_ENABLED is on.
const RuntimeStatusSummaryStaleMetric = "eshu_runtime_status_summary_stale"

// RuntimeStatusSummaryAgeSecondsMetric is the per-response Prometheus gauge,
// labeled by service_name and model_key, for the age in seconds of the
// active-work summary the scrape served: the database clock at the read minus
// the served row's as_of. It is NaN when the scrape served the zero summary,
// which has no row and so no age. Alert on RuntimeStatusSummaryStaleMetric, not
// on this value alone.
const RuntimeStatusSummaryAgeSecondsMetric = "eshu_runtime_status_summary_age_seconds"
