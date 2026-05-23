# cmd/bootstrap-data-plane Agent Rules

These rules apply only inside `go/cmd/bootstrap-data-plane/`. Root
`AGENTS.md` still controls global proof, performance, concurrency, and skill
requirements.

## Read First

- `go/cmd/bootstrap-data-plane/README.md`
- `go/cmd/bootstrap-data-plane/doc.go`
- `go/cmd/bootstrap-data-plane/main.go`
- `go/internal/storage/postgres/README.md`
- `go/internal/graph/README.md`
- `go/internal/runtime/README.md`

## Local Invariants

- MUST keep this binary schema-only. It must not ingest repositories, emit
  facts, insert application rows, create graph data, drain queues, or run
  service loops.
- MUST keep version probes before store opening.
- MUST apply Postgres schema before graph schema.
- MUST keep graph schema application strict, idempotent, and protected by
  per-statement deadlines.
- MUST use automatic existing-schema adoption for marker-missing preserved
  NornicDB graphs before live DDL, unless
  `ESHU_GRAPH_SCHEMA_ADOPT_EXISTING=false` explicitly disables adoption.
- MUST mark graph schema as applied only after fingerprint verification,
  successful adoption, or successful graph DDL.
- MUST join close errors with the primary error instead of swallowing them.
- MUST keep graph schema sessions in write mode.
- MUST fail before DDL when `ESHU_GRAPH_BACKEND` is unsupported.

## Change Gates

- New Postgres DDL belongs in `internal/storage/postgres.ApplyBootstrap`, not
  in this command.
- New graph backend support MUST update `schemaBackendFromEnv`, graph schema
  selection, backend docs, and conformance evidence.
- Graph DDL changes MUST be validated against the active backend behavior and
  keep adoption/marker semantics correct for preserved-volume restarts.
- Long-running wait loops are not allowed here; dependent services need this
  job to exit.

## Focused Verification

```bash
cd go
go test ./cmd/bootstrap-data-plane -count=1
go doc -cmd ./cmd/bootstrap-data-plane
```
