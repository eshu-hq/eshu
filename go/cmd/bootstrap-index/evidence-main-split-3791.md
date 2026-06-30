# Evidence — bootstrap-index main.go split (#3791, overlaps #3687)

Epic D / D-5 modularization. Records the performance and observability evidence
for splitting `cmd/bootstrap-index/main.go` (the four-phase bootstrap
orchestrator) by phase into same-package sibling files, so the hot-path evidence
gate (`scripts/verify-performance-evidence.sh`) has a tracked, in-repo record.

## Change shape

Pure same-package file split — no logic was edited, only moved (`main.go`
1207 → 230):

- `bootstrap_pipeline.go` — `runPipelined`, `drainProjectorPipelined`,
  `drainingWorkSource`, `projectionWorkerCount`
- `bootstrap_collector.go` — `drainCollector`, discovery-advisory report writers
- `bootstrap_projector.go` — `drainProjector`, `drainProjectorWorkItem`, the
  projector heartbeat, `recordBootstrapProjectionResult`, the sequential drain
- `bootstrap_db.go` — `bootstrapSQLDB`, `openBootstrapDB`, `applySchema`,
  `openBootstrapGraph`

`main.go` keeps `main`, the shared types, and `run`. Per-file imports were
narrowed to what each split file uses.

## No-Regression Evidence:

- **Baseline / after:** the phase-ordering invariant lives entirely inside
  `runPipelined`, whose body was moved verbatim to `bootstrap_pipeline.go`. The
  six-step call order (drainCollector+drainProjectorPipelined →
  BackfillAllRelationshipEvidence → projector drain → MaterializeIaCReachability
  → ReopenDeploymentMappingWorkItems → EnqueueConfigStateDriftIntents) is
  unchanged because no statement inside `runPipelined` was edited. Structural diff
  confirms all 30 top-level declarations from the original `main.go` are present
  exactly once across the five resulting files.
- **Backend/version:** no Postgres or graph backend interaction changed; schema
  apply, the NornicDB canonical-writer wiring, projection worker counts, lease
  heartbeat interval, and drain loops are byte-for-byte unchanged (moved, not
  modified).
- **Input shape / terminal counts:** the collector/projector drain behavior,
  `maxEmptyPolls` logic, worker parallelism (`projectionWorkerCount`), and the
  terminal `errProjectorDrained` sentinel handling are unchanged. No queue,
  batch, worker, lease, or ordering behavior changed.
- **Why safe:** `go build`, `go vet`, `go test ./cmd/bootstrap-index/...
  -count=1` (including `main_test.go`'s phase-ordering tests, the telemetry test,
  and the graph-schema tests), and `golangci-lint run` (filelength plugin loaded)
  are all green. A pure code-movement refactor cannot regress throughput,
  correctness, or the phase-ordering invariant because no instruction changed.

## No-Observability-Change:

No spans, metrics, logs, or telemetry names were added, removed, or renamed. The
bootstrap binary emits the same per-stage timing logs, the same
`eshu_dp_projector_run_duration_seconds` / `eshu_dp_queue_claim_duration_seconds`
signals, the same projection-result records, and the same heartbeat logs as
before; the split only relocates functions between sibling files in the same
`package main`.
