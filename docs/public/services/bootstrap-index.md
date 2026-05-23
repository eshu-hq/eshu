# Bootstrap Index

`eshu-bootstrap-index` is the one-shot runtime for seeding an empty or
recovered Eshu environment. It collects a finite repository set, writes facts to
Postgres, runs source-local projection, triggers the bootstrap-only
post-collection passes, and exits.

It is not a steady-state service in the public Helm chart. Use it for
cold-start seeding, recovery validation, or known-scope end-to-end indexing
proofs. Use the ingester, hosted collectors, workflow coordinator, and
resolution engine for normal freshness.

## Runtime Shape

| Field | Value |
| --- | --- |
| Binary | `/usr/local/bin/eshu-bootstrap-index` |
| Source | `go/cmd/bootstrap-index/` |
| Storage | Postgres facts, queues, content, status; configured graph backend |
| Lifecycle | one-shot local or operator helper |
| Admin HTTP surface | none |
| Telemetry | OpenTelemetry export plus structured logs; optional pprof with `ESHU_PPROF_ADDR` |

## Workflow

```text
collect repository facts
project source-local graph and content state
backfill relationship evidence
wait for source-local projector drain
materialize IaC reachability
reopen deployment_mapping reducer work
enqueue config_state_drift reducer work
exit
```

After bootstrap exits, the steady-state reducer drains the reducer work that
needs cross-repository evidence or shared materialization.

## Concurrency

- Collection uses the shared repository sync and snapshot configuration.
- Projection workers default to `min(NumCPU, 8)` and can be changed with
  `ESHU_PROJECTION_WORKERS`.
- Projector queue claims use Postgres `FOR UPDATE SKIP LOCKED`.
- Long-running projection renews its lease by heartbeat.
- Superseded same-scope work exits without acking stale graph state.

Do not use single-worker settings as a shipped fix for a concurrency problem.
Use them only as diagnostic baselines.

## Operator Notes

- The binary exits with `0` only after every bootstrap step completes.
- Re-running it on an already-seeded environment re-indexes the selected corpus.
- It does not mount `/healthz`, `/readyz`, `/metrics`, or `/admin/status`.
- Use `ESHU_DISCOVERY_REPORT=<path>` to write a discovery advisory JSON array
  for noisy-repository tuning.
- Use `eshu scan` when you need the local CLI to launch bootstrap and wait for
  readiness evidence.
- Use `eshu index` when you only need to launch bootstrap for a local path.

## Related Docs

- [Bootstrap Runtime Services](../deployment/service-runtimes-bootstrap.md)
- [CLI Indexing](../reference/cli-indexing.md)
- [Local Testing](../reference/local-testing.md)
- [Profiling And Concurrency](../reference/local-testing/profiling-and-concurrency.md)
- [NornicDB Tuning](../reference/nornicdb-tuning.md)
