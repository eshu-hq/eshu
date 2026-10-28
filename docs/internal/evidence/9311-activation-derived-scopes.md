# Activation-derived OSV scopes: runtime cost and signals (#9311)

## What changed at runtime

After the egress decision, `scheduleComponentExtensionWork` calls a kind-scoped
hook (`component_extension_service.go`, `scheduleActivationDerivedScopes`).
For a `vulnerability_intelligence` activation it decodes the activation's
`config` block, reads the same owned-package and installed-evidence evidence an
in-tree instance reads, plans one extra derived run, and sends it through the
same durable admission (`createWorkflowWorkIfNoOpenTargets`). The extra run is a
second run beside the generic host-scope run. Startup and `Config.Validate`
also check the derive block (`checkActivationDerivation`).

No query text changed. Proof, run against the base `4997c3eba2`:

- `git diff --name-only 4997c3eba2..HEAD` lists no file under
  `go/internal/storage`.
- `git diff -U0 4997c3eba2..HEAD -- 'go/**/*.go' ':!*_test.go'` has no added line
  that matches `SELECT`, `INSERT INTO`, `UPDATE `, `DELETE FROM`, `MATCH (`,
  `MERGE (`, `QueryContext`, or `ExecContext` (`rg` exit 1).
- The coordinator reuses `vulnerabilityOwnedTargets` and
  `vulnerabilityInstalledEvidenceTargets`. Both now take the decoded derivation
  instead of the instance, so the in-tree instance and the activation share one
  reader call. The reader is `FactStore.ListOwnedPackageDependencyTargets`
  (`storage/postgres/owned_package_targets.go`), unchanged.
- Admission is `WorkflowControlStore.CreateRunWithWorkItemsIfNoOpenTargets`
  (`storage/postgres/workflow_control_open_targets.go`), unchanged.

The cost is one more call of two existing statements per reconcile tick, per
enabled `vulnerability_intelligence` activation. This note measures both.

## Call frequency

- The reconcile tick is `ESHU_WORKFLOW_COORDINATOR_RECONCILE_INTERVAL`, default
  30 s (`schedule.DefaultReconcileInterval`, `schedule/derivation.go:18`;
  `config.go:99`). `runReconcile` runs every scheduler on every tick
  (`in_tree_schedulers.go:139` lists `scheduleComponentExtensionWork`).
- The hook runs on each tick for every enabled, claims-enabled activation whose
  config parses (`component_extension_service.go:93`). The owned-package read
  runs before admission, so it runs on every tick even when every scope is
  already open. The plan key and rotation offset use the scan interval, not the
  tick. Nothing caches the read between ticks. The in-tree instance does the
  same (`vulnerability_intelligence_service.go`).
- The read runs only when `derive_from_owned_packages.enabled` is true. The two
  installed-evidence reads (OS package and SBOM component) run only when
  `derive_from_installed_evidence.enabled` is true. A host-only activation reads
  nothing. If an in-tree instance and an activation both enable derivation,
  each pays its own read.

## Performance Evidence

No-Regression Evidence: the change adds no query text. It calls the existing
owned-package read and the existing guarded admission once more per reconcile
tick for each enabled activation. On a private PostgreSQL 18.6 the read costs
230.6 ms server time (median of eight warm runs, 15,305 distinct targets) and
the planning and admission add 1.1 to 1.5 ms at 1,000 targets. There is no
before figure to compare, because the activation planned no derived scopes
before this change; the baseline is the in-tree instance's identical read.

Backend: private PostgreSQL 18.6 (Homebrew, aarch64 macOS), built for this
note on port 55911 and torn down afterwards. Schema: the production bootstrap
(`postgres.ApplyBootstrap`, 255 migrations). Host load average (1 min) was 9.6
to 12.5 during the read timings: a shared laptop, so absolute times are noisy
and the ratios matter more.

### Owned-package read (statement unchanged)

Input shape: modeled on the #9285 corpus in
`go/internal/storage/postgres/gotchas-and-invariants.md`. 185,000
`fact_records` rows: 85,000 dependency facts (65,000 in active generations,
20,000 in superseded generations) and 100,000 other `content_entity` rows;
650 scopes, each with one active and one superseded generation. Active
dependency facts by manager: maven 20,000, gradle 3,312, npm 25,000, go 8,000,
pypi 8,688. The gradle share is 5.1%, not the 4% in the #9285 corpus, so the
corpus is close to the earlier one, not identical.

Statement: `listOwnedPackageDependencyTargetsQuery(versionSpecific=true,
expandAliases=true)` with `ecosystems = ["maven"]`, so `package_manager` is
`ANY('{gradle,maven}')`. It was run through `PREPARE` and `EXECUTE` with
`limit = 1001` (a `target_limit` of 1000 plus the one-row lookahead) and a
different rotation offset on each run. The exact text came from the Go function.

Terminal row counts: 43,312 index rows scanned, 23,312 active gradle and maven
facts joined, 15,305 distinct targets ranked (the #9285 count), 1,001 returned.

Plan shape (`EXPLAIN (ANALYZE, BUFFERS)`):

- `Index Scan using fact_records_active_package_dependency_entity_idx` on the
  `package_manager` expression, filter `NOT is_tombstone`, 43,312 rows.
- Nested loop to `scope_generations_active_scope_idx` (43,312 loops, 0.54 rows
  each), then to `ingestion_scopes_active_generation_idx` (23,312 loops).
- `Sort` and `Unique` (23,312 to 15,305 rows), `Incremental Sort`, two
  `WindowAgg` nodes, and a top-N heapsort for `LIMIT`.
- Shared buffer hits 243,777, planning time 10.5 ms.

One discarded warm-up, then eight warm runs each:

| Measure | Runs (ms) | Median (ms) |
| --- | --- | --- |
| Server `Execution Time` | 238.3, 228.1, 227.3, 220.4, 237.8, 234.1, 231.2, 230.0 | 230.6 |
| Client wall time, `psql \timing`, 1,001 rows | 252.8, 233.9, 229.3, 237.0, 273.6, 235.5, 285.6, 311.2 | 244.9 |

Per call, the read takes about 231 ms on the server and about 245 ms at the
client. At the default 30 s tick that is about 0.8% of one database backend per
enabled activation (231 ms over 30,000 ms). It matches the in-tree instance's
read; the plan is the one the #9285 note records for the same query.

### Admission and planning (real store, in-memory reader)

Measured through the real `Service.scheduleComponentExtensionWork` and
`WorkflowControlStore` on the same cluster, in an isolated schema per case. The
reader was an in-memory fake returning N maven targets, so this table excludes
the SQL read above. The generic host-scope run is included in every case.
Warm ticks are the median of eight ticks (the first tick of each series of
nine was discarded).

| Case | Items after tick 1 | First tick (ms) | Same-bucket tick, all open (ms) | Next-bucket tick, all open (ms) |
| --- | --- | --- | --- | --- |
| Host only (no derive block) | 1 (1 run) | 1.9 | 0.3 | 0.3 |
| Derive, 1,000 targets | 25 (24 derived, 2 runs) | 8.5 | 1.8 | 1.4 |
| Derive, 5,000 targets (the cap) | 122 (121 derived, 2 runs) | 30.3 | 5.0 | 4.7 |

An earlier run of the same test gave 9.4, 2.3, and 2.1 ms for the 1,000-target
case, so run-to-run noise on the warm ticks is up to about 30%.

Reading the table:

- The target limit counts OSV queries, and the planner groups them into scopes,
  so 1,000 queries became 24 work items and 5,000 became 121. Admission work is
  the item count (up to 122 here), not the query count.
- The open-target guard reads once per call, not once per scope in a loop:
  `workItemsWithoutOpenTargets` runs one statement over all items. It first
  takes `pg_advisory_xact_lock` keyed on `(collector_kind,
  collector_instance_id)` (`workflow_control_open_targets.go:204`), the same key
  the host-scope run takes. The two runs go one after the other in one
  goroutine, so they do not wait on each other. Two coordinators serialize on
  that key as before.
- In steady state (every scope open) a tick inserts nothing: items stayed at 25
  and 122 across 18 later ticks per case (9 in the same bucket, 9 in later
  hourly buckets).
- Per tick, admission plus planning adds 1.1 to 1.5 ms at 1,000 targets and
  4.4 to 4.7 ms at the 5,000 cap, over the host-only tick.

Per tick for a 1,000-target activation: about 231 ms (read) plus about 1.5 ms
(plan and admission) is about 233 ms, dominated by the existing read. The
installed-evidence reads are unchanged queries and are not measured here.

### Startup and validation checks

`checkActivationDerivation` decodes one small JSON block and applies pure
rules. A throwaway benchmark (a four-ecosystem owned-package block, 20,000
iterations, five runs) gave 1,989 to 2,078 ns/op, 1,472 B/op, 36 allocs/op. It
reads no store and logs nothing. It runs once per desired instance in
`Config.Validate` (`config.go:273`), once per registry activation in
`componentActivationRuntimeConfig` (`component_activation_config.go:181`), and
once per hook call as a backstop. That is about 2 microseconds per tick per
activation, so the checks add no measurable per-reconcile cost.

### Why this is safe

- No statement changed. The two statements that run once more per tick are the
  existing reader and the existing guarded admission, with the plan above.
- The derived run is bounded by the existing `target_limit` clamp (default 100,
  at most 5,000), so one tick cannot plan an unbounded set.
- The derived run id has a `derived` segment, so it never equals the generic
  run for the same plan key, and the open-target guard admits per scope, so the
  open host item does not block derived items.
- A concurrent second coordinator is covered by the existing advisory lock.
  `TestActivationDerivedScopesAdmissionLive` and
  `TestActivationDerivedScopesConcurrentCoordinatorsLive` passed against this
  private cluster (exit 0, both PASS).

Line numbers below are at the commit that adds this note.

## Observability Evidence

Observability Evidence: one new counter, one INFO log line, and one ERROR log
line, all listed below, plus the existing reconcile counter and read histogram.

New signal: `eshu_dp_workflow_coordinator_activation_derived_scopes_planned_total`.
It did not exist on `origin/main` (`git grep` count 0).

| Signal | Where | What an operator sees |
| --- | --- | --- |
| Counter `eshu_dp_workflow_coordinator_activation_derived_scopes_planned_total` | registered `telemetry/instruments_component_extension.go:71`, field `instruments.go:2980`, wired `cmd/workflow-coordinator/main.go:260`, recorded `component_extension_service.go:300` and `:321` | Labels `collector_kind`, `collector_instance_id`, `ecosystem`. Adds the planned scope count per ecosystem per tick, before admission. A flat line for an enabled activation means nothing is derived. A tick inside one plan bucket counts the same scopes again, so read it as a rate, not as admitted work. |
| INFO `workflow coordinator planned derived scopes for component activation` | `component_extension_service.go:307` | `planned_work_items`, `enqueued_work_items`, `plan_key`, `run_id`, `owned_package_candidates`, `installed_evidence_candidates`. Planned minus enqueued is the open-target skip. |
| ERROR `workflow coordinator refused derived scopes for component activation` | `component_extension_service.go:246` (host mismatch) and `:258` (blank list) | The blank-list line carries `derive_section`, `derive_list`, and `reason=derive_lists_not_explicit`. It carries no configured value. The host-mismatch line carries `host_source_system`, `host_scope_kind`, and `reason`. |
| `eshu_dp_workflow_coordinator_reconcile_total{outcome="reconcile_error"}` | `coordinator/metrics.go:20`; recorded `service.go:284` when a scheduler returns an error | The refusal above returns an error from the hook, so the tick counts as `reconcile_error` and `Service.Run` returns it. |
| `eshu_dp_postgres_query_duration_seconds{store="owned_package_targets", operation="read"}` | `cmd/workflow-coordinator/main.go:194-199`, `storage/postgres/instrumented.go:227`; histogram `telemetry/instruments.go:6261` | The read histogram. Its top bucket is 2.5 s and the measured 0.23 s read falls in the 0.25 s bucket. A rise here is the first sign of corpus growth. |
| Startup failure | `config.go:273` and `component_activation_config.go:172,183` | A bad block stops `LoadConfig` with `collector instance "<id>": ...` or `load component activation config block for "<id>": ...`. `main.go:66` logs `workflow coordinator failed` and exits 1, before any durable write. The error text names the section and list, never the values. |

No-Observability-Change: the `owned_package_targets` store label, the read
histogram, the reconcile counter and its `outcome` set, and the in-tree
scheduler's existing signals keep their names, labels, and meaning. This change
adds one counter and two log lines, listed above, and reuses the rest.

The signal is documented in `docs/public/observability/telemetry-coverage.md`
(row for extension host, coordinator, and activation derived scopes) and
`docs/public/reference/telemetry/metrics-ingestion-collectors.md`. Both rows
are added by this PR.

## Not measured

- The two installed-evidence reads (OS package, SBOM component). They are
  unchanged queries that run only when `derive_from_installed_evidence` is
  enabled.
- Production or QA corpus size. The corpus here is synthetic and sized like the
  #9285 note, not read from a real deployment.
- The effect of other load on the Postgres instance during concurrent ticks of
  several activations. Each activation adds its own read.
