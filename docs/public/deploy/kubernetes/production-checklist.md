# Production checklist

Use this list before a team relies on a Kubernetes deployment.

## Storage

- Postgres is reachable from every database-backed workload.
- Postgres has `pg_trgm` available.
- Backups and restore tests exist for Postgres.
- Hosted restore proof has a current public-safe summary from
  `scripts/verify-hosted-backup-restore-proof.sh`.
- NornicDB is the graph backend unless you explicitly choose Neo4j.
- Graph credentials are stored in a Secret, not inline values.
- The ingester workspace PVC has enough capacity for the repositories you
  index.

## Configuration

- `contentStore.secretName` and `contentStore.dsnKey` point at the production
  Postgres DSN Secret; inline `contentStore.dsn` is not used for hosted
  production values.
- `neo4j.uri` points at the chosen Bolt endpoint.
- `env.ESHU_GRAPH_BACKEND`, `env.DEFAULT_DATABASE`, and `env.NEO4J_DATABASE`
  match the chosen backend.
- `apiAuth.secretName` exists and contains the configured key.
- Repository sync rules are narrow enough for the intended team.
- Public API docs stay disabled unless you intentionally enable them in the
  chart environment map.
- `networkPolicy.egress.mode` is set to `restricted` unless a separate cluster
  policy system owns outbound controls. Broad egress is documented as a
  temporary governance risk.
- Restricted NetworkPolicy values include only the required DNS, datastore,
  graph, internal-service, collector-provider, semantic-provider, and extension
  destinations.

## Workloads and operations

- Runtime resources are sized for expected traffic and repository volume.
- The ingester is the only workload with the repository workspace PVC.
- Resolution-engine replica count and Postgres connection limits are sized
  together.
- API, MCP, logs, metrics, and runtime status are checked during rollout.
- A hosted Helm rollout proof artifact exists for the current values, chart
  version, image tag or digest, rendered workloads, schema bootstrap, API/MCP
  readback, queue state, and first bounded query result.
- Workflow coordinator claims are active only when you intentionally enable
  claim-driven collectors. The chart requires `workflowCoordinator.enabled=true`,
  `workflowCoordinator.deploymentMode=active`, `workflowCoordinator.claimsEnabled=true`,
  and at least one coordinator collector instance for that path.
- OTEL export points at the correct collector.
- Prometheus metrics and `ServiceMonitor` resources are enabled when your
  platform expects direct scraping.
- Helm rollback, database restore, graph rebuild, queue terminal state, and
  API/MCP readback are covered by the runbook.
- Upgrade and rollback declarations separate durable Postgres state, queue
  state, preserved volumes, Helm rollback, Postgres restore, and graph rebuild
  decisions.
