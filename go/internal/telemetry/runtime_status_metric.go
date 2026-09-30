// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

// RuntimeStatusSnapshotAvailableMetric is a per-response Prometheus gauge. It
// is assembled by the runtime metrics handler rather than registered with the
// OTEL meter, because concurrent scrapes can have different status outcomes.
const RuntimeStatusSnapshotAvailableMetric = "eshu_runtime_status_snapshot_available"
