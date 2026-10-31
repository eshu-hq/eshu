# Engine OOM Bulk-Load Shed Evidence (#9697)

The resolution-engine OOM livelock is allocation churn, not a leak: pprof
showed ~1 GB/s lifetime allocation (`encoding/json` decode 47%, pgx/bytes
copies 33%) from unbounded per-intent fleet-wide fact reloads. Two changes
shed the churn at its source; this note records the micro-proof.

## What changed

- `CorrelatedWorkloadProjectionInputLoader` evaluates a
  candidate-precondition probe plus the own-generation and corpus-fence
  checks before `LoadFactsForKinds`. The probe
  (`FactStore.ScopeMayHoldWorkloadCandidates`, one EXISTS statement over
  the intent's own scope generation) fires the pre-gate only on an explicit
  true, so candidate-free scopes keep their pinned vacuous success and the
  post-load gates still decide truth afterwards.
- `FactStore` coalesces concurrent `ListActiveContainerImageSLSAFacts` and
  `ListActiveRepositoryFacts` passes in a shared in-flight group that
  retains nothing: 16 racing workers pay for one pass, and sequential
  callers always reread the database.

## Benchmark Evidence

Hermetic in-process fake-DB harnesses (no live Postgres; the probe SQL
semantics carry a CI integration live test,
`workload_candidate_precondition_live_test.go`). Baseline `origin/main`
`69331f8862`, after `drive/9697-engine-oom` HEAD, same host, same bench
sources on both trees:

Deferred 200-file scope pass, own generation inactive
(`BenchmarkWorkloadProjectionDeferredScopeInputs`, `-benchtime=100x`,
`-count=3`):

| metric | before | after |
| --- | --- | --- |
| fact-loads/op | 1.000 | 0 |
| B/op | ~27,000 | 96 |
| allocs/op | 1614 | 1 |

16 concurrent workers racing one 50-row (~1 KB/row) SLSA load
(`BenchmarkSharedFleetSLSALoads`, `-benchtime=10x`, `-count=3`):

| metric | before | after |
| --- | --- | --- |
| db-passes/op | 16.00 | 1.000 |
| B/op | ~4,190,000 | ~764,000 |
| allocs/op | ~27,300 | ~3,350 |

Commands:

```bash
cd go && go test ./internal/reducer/ -run '^$' -bench 'BenchmarkWorkloadProjectionDeferredScopeInputs' -benchmem -benchtime=100x -count=3
cd go && go test ./internal/storage/postgres/ -run '^$' -bench 'BenchmarkSharedFleetSLSALoads' -benchmem -benchtime=10x -count=3
```

The SLSA wall time is dwell-dominated on both sides (100 ms test-only
overlap margin so all 16 goroutines pile onto one flight); the production
win is passes and bytes, which is what feeds the 1 GB/s churn. The
post-fix per-op bytes include the 16 defensive copies each served caller
gets so no caller can mutate shared rows.

## Observability Evidence

No-Observability-Change: the pre-gate emits no signal of its own. A
deferral surfaces as the durable failure class
`workload_materialization_resolution_not_ready` on the work item; the
probe and fence statements are timed by
`eshu_dp_postgres_query_duration_seconds`; the owning pass stays covered
by `eshu_dp_reducer_executions_total` and
`eshu_dp_reducer_run_duration_seconds`. The repository-catalog load
histograms are still recorded once per served caller from the shared rows,
so per-domain attribution is unchanged. Coverage rows:
`docs/public/observability/telemetry-coverage.md` (workload projection
bulk-load pre-gate, workload candidate-precondition probe contract).

## Why it is safe

- The post-load own and corpus gates run unchanged after the bulk load, so
  ready intents decide on identical inputs; only the load-then-discard
  order changed.
- The probe fires only on an explicit true; a missing probe, a probe
  error, or false proceeds down the old path. Candidate-free scopes keep
  their pinned vacuous success (`TestWorkloadProjectionInputsZeroCandidatesSkipFencedRead`
  passes unmodified).
- The shared group deletes each flight on completion and serves defensive
  copies; sequential callers reread the database
  (`TestSharedFleetLoadServesFreshTruthSequentially`), so a superseded
  generation is never served stale.
- Full `go/internal/reducer`, `containerimage`, `factload`, `crossrepo`,
  and `accepted` suites plus the `storage/postgres` unit suite pass with
  `-race` on the touched surfaces; the only red is the pre-existing
  environmental `TestSchemaBootstrapCoordinationDefaultsFitTheJobDeadline`
  (missing `ESHU_DEPLOY_CHECKOUT`, fails identically on untouched base).
