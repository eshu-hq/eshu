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

`enqueue config_state_drift reducer work` is a one-shot sweep over every
`state_snapshot:*` scope active at the moment bootstrap runs; it does not
repeat after exit. A `terraform_state_snapshot` that lands later — through
`collector-terraform-state` and the ingester's steady-state projector, not
through bootstrap-index — is drift-evaluated by a separate runtime
delta-trigger the ingester fires when that scope's generation activates, so
drift evaluation follows the data instead of waiting for the next
bootstrap-index run (issue #5593). Both producers enqueue into the same
`config_state_drift` reducer domain and dedupe against each other by
`(scope_id, generation_id)`.

**Known gap:** if that runtime evaluation runs before the Terraform config
repo owning the backend has been added to Eshu and synced, the handler
rejects the snapshot generation with "no config repo owns this backend."
That rejection is NOT durably recorded anywhere queryable: the reducer work
item is marked succeeded with no failure payload, and the only trace is a
`drift candidate rejected` structured WARN log line at evaluation time,
subject to whatever log retention the deployment has configured. The
rejection is durably terminal only in the narrow sense that the same
generation is never automatically retried; nothing durable records *why* it
was rejected. In practice this self-heals on the next `terraform apply`,
since a new apply produces a new snapshot generation that is evaluated
independently. A state that never changes again after racing once will not
be re-evaluated on its own; three convergence paths cover the rest:

- `eshu-bootstrap-index` re-run — a fresh sweep over every currently active
  `state_snapshot:*` scope, including that one, but only on demand.
- The reducer's `config_state_drift` catch-up sweep — a background loop in
  the steady-state reducer process (`go/cmd/reducer/config_state_drift_catchup_sweeper.go`)
  that re-scans active `state_snapshot:*` scopes on a bounded interval
  (default 5 minutes) and re-enqueues through the same idempotent
  `(scope_id, generation_id, domain)` work item every other producer uses.
  It closes the gap for a lost or never-fired runtime trigger without
  waiting for either a new `terraform apply` or an operator-initiated
  bootstrap re-run.
- The explicit "unresolved" read-model outcome tracked in sibling branch
  5594-local-backend-default-path (issue #5593's own second acceptance
  criterion), which is out of scope here.

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
