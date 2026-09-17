# AGENTS.md — cmd/bootstrap-data-plane guidance for LLM assistants

## Read first

1. `go/cmd/bootstrap-data-plane/README.md` — binary purpose, configuration,
   idempotency contract, and gotchas
2. `go/cmd/bootstrap-data-plane/main.go` — `run`, `schemaBackendFromEnv`,
   `openNeo4j`, and `neo4jSchemaExecutor`; the full wiring is here
3. `go/internal/storage/postgres/README.md` — `ApplyBootstrap` and `Executor`;
   the Postgres DDL applied here
4. `go/internal/graph/README.md` — `EnsureSchemaWithBackend`, `CypherExecutor`,
   `SchemaBackend`; the graph DDL applied here
5. `go/internal/runtime/README.md` — `OpenPostgres`, `OpenNeo4jDriver`,
   `LoadGraphBackend`; shared config helpers

## Invariants this package enforces

- **Idempotency** — Postgres migration receipts skip completed SQL by path,
  variant, and checksum. First rollout to an existing database without the
  ledger replays all historical SQL once; preserve a recoverable copy and
  quiesce application traffic for that run. NornicDB marker-missing preserved
  graphs adopt the existing graph schema before DDL because repeated constraint
  checks can take minutes per statement on large graphs.
- **Both stores must succeed** — `run` applies Postgres first (logging with
  `EventAttr`), then graph; if either fails the process exits non-zero. Close
  errors are joined with `errors.Join` rather than swallowed. Enforced at
  `main.go:90` and `main.go:113`.
- **Backend gate** — `schemaBackendFromEnv` calls `LoadGraphBackend` and maps
  the result to `graph.SchemaBackend`; unknown values return an error before
  any DDL runs. Enforced at `main.go:156`.
- **Write session only** — `neo4jSchemaExecutor` always opens a session with
  `AccessModeWrite`; it must not be pointed at a read replica. Enforced at
  `main.go:226`.

## Common changes and how to scope them

- **Add a new Postgres migration** → add the DDL to `postgres.ApplyBootstrap`
  in `internal/storage/postgres/`; this binary calls it without change. Why:
  DDL ownership lives in the storage package, not here.

- **Add a new graph backend** → add a case to `schemaBackendFromEnv` mapping
  the new `runtimecfg.GraphBackend*` constant to a `graph.SchemaBackend`
  value; add a case in `graph.EnsureSchemaWithBackend`. Why:
  `schemaBackendFromEnv` is the only backend-selection point in this binary.

- **Change the Neo4j driver configuration** → touch `openNeo4j`; the
  `neo4jDeps` struct and its `close` func are the seam. Why: the close func
  must honor `neo4jCloseTimeout` (currently 10 seconds) to avoid leaking
  driver connections on error paths.

## Failure modes and how to debug

- Symptom: binary exits with a Postgres open error → cause: ESHU_POSTGRES_DSN
  wrong or Postgres not yet ready → check the env var; in Compose this binary
  is the `db-migrate` service that must run after Postgres health checks pass.

- Symptom: binary exits with `unsupported graph backend for schema` → cause:
  ESHU_GRAPH_BACKEND is not `neo4j` or `nornicdb` → check the env var spelling
  and value.

- Symptom: graph DDL fails with a Cypher parse error → cause: the graph backend
  does not recognize a DDL statement → compare the statement against the backend
  dialect; for NornicDB, check the NornicDB ADR and tuning reference for
  known Cypher dialect gaps.

## Anti-patterns specific to this package

- **Running normal data collection here** — this binary owns schema DDL and
  migration-owned data transformations. Repository collection, graph
  population, and fact emission belong in `bootstrap-index` or the ingester.

- **Adding a long-running loop** — the binary must exit after DDL completes.
  Adding a poll loop breaks the deployment bootstrap contract and prevents
  dependent services from starting.

## What NOT to change without an ADR

- The migration receipt and DDL idempotency contracts — removing them breaks
  safe retries and coordinated Kubernetes deployment; see
  `docs/public/deployment/service-runtimes.md`.
- The ESHU_GRAPH_BACKEND values understood by `schemaBackendFromEnv` — adding
  or renaming backend values is a multi-package change; see
  `docs/public/reference/backend-conformance.md`.
