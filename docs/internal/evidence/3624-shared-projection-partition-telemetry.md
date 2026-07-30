# Shared-Projection Partition Telemetry Evidence (#3624 Phase 1)

Moved from `go/internal/reducer/README.md` (issue #5786) to keep the
package README under the repository's 500-line cap. Content is
unchanged from the original section.

## Shared-Projection Partition Telemetry (#3624 Phase 1)

Phase 1 adds per-(domain, partition) attribution instruments to the
`SharedProjectionRunner` drain path. These are the missing signals called out in
the #3624 root-cause analysis.

### New Instruments

- **`eshu_dp_shared_projection_partition_processing_seconds`** — histogram of
  full `ProcessPartitionOnce` wall time labeled by `domain` and
  `partition_id` (0-based slot). This is the primary long-pole signal: read
  `max by (domain, partition_id)` to find which (domain, partition)
  pair dominates the drain cycle. The `partition_id` dimension is bounded by
  `ESHU_SHARED_PROJECTION_PARTITION_COUNT` (operator-configured, ≤64 by
  convention).

- **`eshu_dp_shared_projection_intents_completed_total`** — counter of intents
  marked completed per `domain`. Combine with an intent-emit counter
  to compute per-domain drain rate (completed/s) and pending backlog depth
  without a per-scrape table scan.

Graph-write gate-acquire wait is already covered by the existing
`eshu_dp_graph_write_backpressure_wait_seconds` instrument (labeled by
`operation`), which records every write that blocked for a permit from the
shared `ESHU_GRAPH_WRITE_MAX_IN_FLIGHT` pool. No new gate-wait instrument is
needed.

### Observability Evidence

`eshu_dp_shared_projection_partition_processing_seconds` and
`eshu_dp_shared_projection_intents_completed_total` provide the per-domain
attribution missing before this change. An operator running Grafana against the
next corpus run can identify the long-pole domain and its partition distribution
from metrics alone, without strace or DB forensics.

Verified by three focused tests in `shared_projection_runner_test.go`:
- `TestRecordSharedProjectionPartitionMetrics_HistogramAndCounter` — emission
  and label correctness under a real `sdkmetric.ManualReader`.
- `TestRecordSharedProjectionPartitionMetrics_SkipsZeroDuration` — no spurious
  zero-bucket emission for idle cycles.
- `TestRecordSharedProjectionPartitionMetrics_CardinalityBounded` — data points
  carry no forbidden unbounded labels (intent_id, scope_id, generation_id,
  acceptance_unit_id, repository_id).

### Performance Evidence

Phase 2 remote measurement pending. These instruments exist so the next corpus
run can provide the per-domain latency distribution that will confirm the Phase
2 scaling parameters (`ESHU_SHARED_PROJECTION_PARTITION_COUNT`,
`ESHU_SHARED_PROJECTION_WORKERS`). No scaling changes land in Phase 1.

### No-Regression Evidence

Both instruments record via two concurrency-safe OTEL calls
(`Float64Histogram.Record`, `Int64Counter.Add`) in the existing
`processPartitionWithTelemetry` path. No new goroutine, query, lease claim,
graph write, or queue behavior is introduced. The recording path holds no shared
mutable state — all inputs (`domain`, `partitionID`, `totalDurationSeconds`,
`result.ProcessedIntents`) are call-local. Verified by
`go test ./internal/reducer ./cmd/reducer ./internal/telemetry -count=1 -race`
(2678 tests, all passing).
