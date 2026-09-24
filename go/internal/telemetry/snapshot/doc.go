// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package snapshot serves graph-backed observable-gauge values from a
// background-refreshed cache so a slow backend read can never stall the
// /metrics collection (#7062).
//
// An OpenTelemetry observable-gauge callback runs on the meter collection
// goroutine while the reader holds its collection lock, so a callback that
// waits on a graph read blocks every scrape behind it. Register a Fetch for
// each gauge with Refresher.Register and hand the returned Source to the gauge
// callback: Source.Counts is a lock-free read of the last published snapshot
// and never performs I/O. Refresher.Start runs one deadline-bounded read at a
// time per Source on its own goroutine, publishes each success atomically,
// keeps the previous snapshot on error or timeout, and stops when the context
// passed to Start ends.
//
// The refresher emits eshu_dp_gauge_snapshot_refreshes_total,
// eshu_dp_gauge_snapshot_refresh_duration_seconds, and
// eshu_dp_gauge_snapshot_age_seconds, all labeled by the closed `gauge` value
// the caller registers, and logs a WARN for each failed refresh.
package snapshot
