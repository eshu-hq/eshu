# Reader query identity proof for #7247

## Scope

The reader signal copies the native pgx PID and TCP peer from the leased
connection. It records `postgres.reader_query_start` on the active recording
span before the business query. The sequence belongs to one `Access` instance.
It does not add SQL, change the query, or change pool limits or timeouts.

The TCP peer can identify a service address. It does not establish physical
pod identity. Deployed cancellation proof must bind the event to the physical
backend and its start time through separate runtime observations.

## Correctness proof

The native same-lease test passed on 2026-10-03. The copied PID matched
`pg_backend_pid()` on that leased connection. The connection reported recovery
and read-only mode. The recording span retained the event. The test did not
skip. Its production source was commit `b07e83038425a027f1f00e990c63530300fc4e7d`.

The new collector test uses `telemetry.NewProviders`, the production OTLP gRPC
exporter, and its batch processor. An owned local collector verifies the trace
and parent IDs, PID, peer, role, event sequence, event count, and zero dropped
events after `ForceFlush`. A seeded collector mutation removed events and made
this test fail. After restoring the file, the focused race test passed. The
full `internal/runtime/postgres` package test also passed.

## Combined signal cost

`TestReaderQuerySignalCostNative` passed without skipping on 2026-10-03.
The test borrowed one native pgx connection to a read-only standby. It used the
production observer and batched OTLP exporter with an owned local collector.
The timed interval starts before span creation and ends after `span.End()`.
It includes the actual connection identity capture and event recording.
Flush, network delivery, and receiver assertions occur outside the interval.

Each event count uses two rounds of four arms: ABBA, then BAAB. Each arm has
128 samples. A baseline span records zero events. An event span records the
stated count. The nearest-rank p95 values below are per-arm maxima, not a
pooled percentile or deployed endpoint latency.

| Events per span | Baseline p95 range | Event p95 range | Use |
| --- | --- | --- | --- |
| 1 | 1.666–8.000 microseconds | 2.666–6.958 microseconds | Representative frozen #7247 paths |
| 2 | 1.625–5.583 microseconds | 6.375–9.875 microseconds | Synthetic stress |
| 22 | 1.208–3.083 microseconds | 19.083–28.792 microseconds | Synthetic stress |

All measured event-arm p95 values, including the whole timed interval, were
below the initial 1 millisecond added-signal budget. Baseline noise prevents
claiming a precise speedup or a pooled incremental p95. All 3,072 spans and
12,800 events were received with the expected parent and identity fields.
The process had no numeric `OTEL_BSP_*` or `OTEL_SPAN_*` environment overrides.
The host used Go 1.27.1 on darwin/arm64 and pgx v5.9.2; OTEL SDK was v1.45.0.
This bounded test does not measure exporter saturation or deployed retention.

Reported allocation counts are process-wide and include asynchronous exporter
and collector work. They are not isolated allocations of the identity getter.
The one-event phases reported 13.000–13.016 allocations per sample, compared
with 3.000–3.047 in the baseline phases for that experiment.

Source analysis at `b07e830` found that the frozen inventory arguments issue
one business reader query. The frozen function-language arguments issue zero
or one, depending on whether graph rows require content enrichment. Canonical
repository selector resolution adds no reader query. MCP forwards to these
HTTP handlers. Reader fences and writer checkpoints are separate operations.

The native cost window and cleanup took 6.325 seconds. All owned local process
groups were reaped. The unique application marker had zero remaining sessions
on the bound reader after completion. No API workload or corpus query ran.

## Commands and limits

```sh
go test ./internal/runtime/postgres -run '^TestReaderQuerySignalProductionOTLPDelivery$' -count=1
go test -race ./internal/runtime/postgres -run '^TestReaderQuerySignalProductionOTLPDelivery$' -count=1
go test ./internal/runtime/postgres -count=1
go test ./internal/runtime/postgres -run '^TestReaderQueryIdentityMatchesOneNativeLease$' -count=1 -v
go test ./internal/runtime/postgres -run '^TestReaderQuerySignalCostNative$' -count=1 -v
```

The two native tests require `ESHU_READER_TEST_READER_DSN` for an explicitly
owned read-only fixture. The local collector test has no database requirement.
Native fixture credentials were passed only in memory and were not retained.
Machine-specific receipts remain in the operator's private proof packet.

Deployed API/MCP request correlation, cancellation, trace retention, endpoint
p95, and full-corpus acceptance remain unchecked. This signal proof does not
close #7247 or any other latency issue.
