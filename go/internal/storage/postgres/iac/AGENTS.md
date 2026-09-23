# AGENTS.md — Postgres IaC reachability store guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` for Postgres storage conventions.
3. `reachability.go` for the store, DDL, and cleanup-finding queries.
4. `reachability_test.go` for the store contract; its
   `TestIngestionStoreMaterializeIaCReachabilityWritesActiveCorpusRows` test
   also covers the reducer-side materializer that stays in the parent
   `postgres` package (`iac_reachability_materializer.go`) until
   `ingestion/` moves (`#6693`).

## Invariants

- Keep the DDL, upsert batching, and cleanup-finding predicates
  byte-identical when moving code; predicate or lifecycle changes need
  their own issue with EXPLAIN and contention proof per
  `eshu-postgres-rigor`.
- `ListCleanupFindings`/`ListLatestCleanupFindings` must keep excluding
  `used` rows unconditionally; only the `ambiguous` inclusion is
  caller-controlled.
- `ListLatestCleanupFindings`/`CountLatestCleanupFindings`/`HasLatestRows`
  must keep joining `scope_generations` on `status = 'active'`.
- Keep the package clause as `package iacstore`; callers import the
  `storage/postgres/iac` path without an alias.
- Never import the parent `postgres` package from here.

## Common changes

- Change the DDL only with `TestIaCReachabilitySchemaSQL`'s expected
  fragments updated in lockstep.
- Change cleanup-finding predicates only with the upsert/list/count/has-rows
  test updated and an idempotency proof for the `ON CONFLICT` upsert.

## Failure modes

- Importing the parent `postgres` package creates an import cycle.
- Dropping the `used`-exclusion filter would surface in-use artifacts as
  cleanup candidates, an operator-facing correctness bug.
- Dropping the active-generation join would let a superseded generation's
  stale rows leak into the "latest" read surface.

## Verification

From `go/`, run:

```bash
go test ./internal/storage/postgres/iac/... -count=1
go vet ./internal/storage/postgres/iac/...
```

Run `scripts/verify-package-docs.sh` from the repository root.
