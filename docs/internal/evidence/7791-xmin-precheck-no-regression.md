# Evidence: #7791 — the xmin-precheck init container needs no performance regression proof

Scope: the `xmin-precheck` init container added to the schema bootstrap Job
(`deploy/helm/eshu/templates/job-schema-bootstrap.yaml`) and its image default
`schemaBootstrap.precheckImage` (`postgres:18-alpine`, follows
`docker-compose.yaml`) in `deploy/helm/eshu/values.yaml`. The values change is
two static strings (repository and tag). No runtime knob, pool size, batch
size, timeout budget, worker count, query plan, or migration changed.

No-Regression Evidence:
No benchmark applies because nothing on a hot path changed. The precheck runs
once per Job start and issues two `pg_stat_activity` catalog queries against
the target database, bounded by `PGCONNECT_TIMEOUT=10` and
`statement_timeout=30000`, against backend PG 18 catalog columns
(`backend_xmin`, `xact_start`, `datname`) that need no psql-to-server version
match. Baseline: the Job started migrations blind. After: the rendered script
was exercised with a stub `psql` at the PR head — missing DSN exits 2, no
holders exits 0, two holders exits 1 with count 2 and both rows listed, DB
down exits 2. Every error path fails closed: a refusal fails the init
container, and with the chart's `backoffLimit: 1` the Job fails after two
fast attempts instead of waiting out the holder, so the operator re-runs it
once the holder is gone — never a blind concurrent build. The default image pin is the same
`postgres:18-alpine` the Compose stack already runs.

Observability Evidence:
The gate's operator signal is the init-container log plus Job status, both
quoted in the park-holder runbook (`docs/public/operate/park-holder-runbook.md`):
`xmin-precheck: no backend_xmin holder older than 5 minutes; starting schema
bootstrap` on pass, and the `refusing to start schema bootstrap` line with the
holder list on refusal. No new metric was added: a refusing gate is read from
Job state, and one line of refusal text does not warrant a new telemetry
surface.
