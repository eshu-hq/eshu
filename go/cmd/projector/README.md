# cmd/projector

## Purpose

`cmd/projector` builds the focused local verification runtime for source-local
projection. It claims projector queue items from Postgres, runs
`internal/projector`, writes canonical graph nodes and content rows, publishes
graph readiness, and enqueues reducer follow-up.

## Ownership boundary

This binary is for local verification and Compose debugging. In the deployed
stack, source-local projection runs inside `eshu-ingester`. The command owns
process wiring only; projection behavior lives in `internal/projector`, graph
write contracts in `internal/storage/cypher`, and queue storage in
`internal/storage/postgres`.

## Exported surface

The command package exports no API. Its process contract is version probing,
standard Postgres/graph environment config, `ESHU_NEO4J_BATCH_SIZE`,
`ESHU_PROJECTOR_RETRY_ONCE_SCOPE_GENERATION` for tests, hosted admin routes,
and signal-driven shutdown. See `doc.go` for the binary summary.

## Dependencies

The binary wires `internal/projector`, `internal/storage/postgres`,
`internal/storage/cypher`, `internal/content`, `internal/runtime`,
`internal/app`, `internal/status`, and `internal/telemetry`.

## Telemetry

Startup uses `telemetry.NewBootstrap("projector")`, service/component
`projector`, `telemetry.NewInstruments`, an instrumented canonical writer, and
the shared `/healthz`, `/readyz`, `/metrics`, and `/admin/status` routes.
Projection-specific spans and metrics are emitted by `internal/projector`.

## Gotchas / invariants

- Version probes must exit before telemetry, Postgres, graph, or status setup.
- The projector queue and reducer queue are separate handles even though this
  binary wires both.
- `ESHU_PROJECTOR_RETRY_ONCE_SCOPE_GENERATION` is a fault-injection knob and
  must not appear in normal deployment.
- Large canonical writes need lease heartbeat support; otherwise a one-minute
  claim can be reclaimed while work is still running.
- Empty queues are not errors. The binary polls until stopped.

## Focused tests

```bash
cd go
go test ./cmd/projector -run 'Test.*Runtime|Test.*Service|Test.*Retry|Test.*Batch|Test.*Driver' -count=1
go test ./cmd/projector ./internal/projector -count=1
```

Docs-only edits should also pass the package-doc verifier and `git diff --check`.

No-Regression Evidence: `go test ./cmd/projector -run 'TestBuildProjectorService(HeartbeatsLongRunningClaims|UsesWorkerCountFromEnv|WiresRetryPolicyFromEnv)' -count=1` covers standalone projector lease renewal, worker tuning, and retry-policy wiring. Remote E2E Compose starts this runtime after bootstrap indexing so hosted collector `source_local/projector` rows have an always-on claimer.

Observability Evidence: the standalone projector wires the same projector spans,
metrics, structured logs, runtime memory gauge, `/metrics`, `/admin/status`,
and optional pprof startup log used by hosted data-plane workers.

## Related docs

- `docs/public/deployment/service-runtimes.md`
- `docs/public/reference/local-testing.md`
- `go/internal/projector/README.md`
- `go/internal/storage/cypher/README.md`
