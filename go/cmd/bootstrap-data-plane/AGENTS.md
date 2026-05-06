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

- **Idempotency** — every DDL statement uses `CREATE ... IF NOT EXISTS`; the
  binary is safe to run as a Kubernetes initContainer or Compose `db-migrate`
  service on every deploy. This is the doc.go contract.
- **Both stores must succeed** — `run` applies Postgres first (logging with
  `EventAttr`), then graph; if either fails the process exits non-zero. Close
  errors are joined with `errors.Join` rather than swallowed. Enforced at
  `main.go:85` and `main.go:101`.
- **Backend gate** — `schemaBackendFromEnv` calls `LoadGraphBackend` and maps
  the result to `graph.SchemaBackend`; unknown values return an error before
  any DDL runs. Enforced at `main.go:128`.
- **Write session only** — `neo4jSchemaExecutor` always opens a session with
  `AccessModeWrite`; it must not be pointed at a read replica. Enforced at
  `main.go:176`.

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

- **Writing application data here** — this binary owns schema DDL only; it
  must not insert rows, create graph nodes, or emit facts. Data population
  belongs in `bootstrap-index` or the ingester.

- **Adding a long-running loop** — the binary must exit after DDL completes.
  Adding a poll loop breaks the Kubernetes init-container contract and prevents
  dependent services from starting.

## What NOT to change without an ADR

- The DDL idempotency contract (`CREATE ... IF NOT EXISTS`) — removing it
  breaks safe re-runs and coordinated Kubernetes deployment; see
  `docs/docs/deployment/service-runtimes.md`.
- The ESHU_GRAPH_BACKEND values understood by `schemaBackendFromEnv` — adding
  or renaming backend values is a multi-package change; see
  `docs/docs/adrs/2026-04-22-nornicdb-graph-backend-candidate.md`.
