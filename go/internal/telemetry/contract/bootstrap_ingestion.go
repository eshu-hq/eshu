// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

// Span name for bootstrap/ingestion pipeline stage telemetry. Extracted as a
// sibling file so the frozen root contract.go does not grow, mirroring
// collector_stage.go and thirdparty/vaultlive.go. The MetricDimensionSourceFileKind
// and MetricDimensionBootstrapPhase dimensions this stage registers stay in
// root contract.go; their registration into the ordered metricDimensionKeys
// slice happens in root registration.go, not here (see that file's load-bearing
// order comment).
//
// The bootstrap-index pipeline ran 70+ minutes with 4 log lines and zero
// per-stage duration metrics, forcing strace/DB forensics to locate bottlenecks
// (#3678). These constants drive two new instruments:
//
//   - eshu_dp_content_entity_emitted_total (source_file_kind) — lets operators
//     spot a lockfile or config entity explosion (like #3676) from metrics
//     without manual SQL.
//   - eshu_dp_bootstrap_pipeline_phase_seconds (bootstrap_phase) — shows the
//     long pole in a full-corpus run (collection vs. projection vs. backfill
//     vs. iac_reachability vs. config_state_drift vs. content index
//     finalization) from the metrics port.
//
// The SpanBootstrapCollectorCycle span name wraps each drainCollector iteration
// so individual repo collection cycles are trace-visible.

// SpanBootstrapCollectorCycle wraps one collector drain cycle (one repository
// collected and committed) during bootstrap so traces show per-repo collection
// latency alongside the collector.observe span.
const SpanBootstrapCollectorCycle = "bootstrap.collector_cycle"
