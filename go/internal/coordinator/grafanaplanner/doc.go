// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package grafanaplanner validates Grafana planning requests and builds
// deterministic workflow rows for selected configured targets.
//
// The planner preserves configured work-item order, emits privacy-safe sorted
// requested-scope metadata, derives or accepts a valid trigger kind, and
// returns a populated pending run for valid empty selections. Callers retain
// responsibility for scheduling, collector-egress filtering, tenant-grant
// authorization, durable admission, retries, and telemetry.
package grafanaplanner
