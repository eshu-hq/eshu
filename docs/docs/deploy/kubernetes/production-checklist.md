# Production checklist

Use this list before a team relies on a Kubernetes deployment.

## Storage

- Postgres is reachable from every database-backed workload.
- Postgres has `pg_trgm` available.
- Backups and restore tests exist for Postgres.
- NornicDB is the graph backend unless you explicitly choose Neo4j.
- Graph credentials are stored in a Secret, not inline values.
- The ingester workspace PVC has enough capacity for the repositories you
  index.

## Configuration

- `contentStore.dsn` points at the production Postgres database.
- `neo4j.uri` points at the chosen Bolt endpoint.
- `env.ESHU_GRAPH_BACKEND`, `env.DEFAULT_DATABASE`, and `env.NEO4J_DATABASE`
  match the chosen backend.
- `apiAuth.secretName` exists and contains the configured key.
- Repository sync rules are narrow enough for the intended team.
- Public API docs stay disabled unless you intentionally set
  `env.ESHU_ENABLE_PUBLIC_DOCS: "true"`.

## Workloads and operations

- Runtime resources are sized for expected traffic and repository volume.
- The ingester is the only workload with the repository workspace PVC.
- Resolution-engine replica count and Postgres connection limits are sized
  together.
- API, MCP, logs, metrics, and runtime status are checked during rollout.
- Workflow coordinator claims remain off in Helm. Do not promote active
  coordinator claim ownership until the dedicated remote full-corpus proof
  passes with clean queue state and API/MCP truth checks.
- OTEL export points at the correct collector.
- Prometheus metrics and `ServiceMonitor` resources are enabled when your
  platform expects direct scraping.
- `helm rollback` and database restore are both covered by the runbook.
