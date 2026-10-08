// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package census samples, on an interval, how many graph nodes with an id the
// labeled Neo4j entity-context anchor cannot reach (#7212), and records the
// snapshot as a gauge and a log line after each pass.
//
// [Runner] takes one pass at startup so the startup log line carries the
// count, then one pass per interval, each under its own deadline. The scan is
// read-only and idempotent, so replicas need no lease: each reports its own
// snapshot, read at its own time. The result is recorded after the pass and
// never during a metrics scrape. A failed or timed-out pass leaves the gauge
// and the last-success time at their last good values, so the age of the
// snapshot grows.
package census
