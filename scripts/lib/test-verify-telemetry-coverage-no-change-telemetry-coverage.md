# Telemetry Coverage Contract

This page enumerates every observable stage in the Eshu data plane and the
metric, span, or log key it must emit. The CI coverage script (X2) diffs
against it.

## Reducer Stages

| stage | file:line | required metric name(s) | category |
| --- | --- | --- | --- |
| queue claim | go/internal/reducer/service.go:1 | `eshu_dp_queue_claim_duration_seconds` | reducer runtime |
| reducer run | go/internal/reducer/service.go:2 | `eshu_dp_reducer_run_duration_seconds` | reducer runtime |
| content re-read | go/internal/telemetry/instruments.go:1 | `No-Observability-Change: eshu_dp_content_rereads_total and eshu_dp_content_reread_skips_total counters are registered but no longer emit; facts emitted/fact batches committed cover the path` | reducer fact commit |
