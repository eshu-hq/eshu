# AGENTS.md — Postgres scope helpers guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` for Postgres storage conventions.
3. `source_key.go` and `repo_id.go`, with their tests.

## Invariants

- Keep the package clause as `package scopestore`; callers import the
  `storage/postgres/scope` path without an alias, next to `internal/scope`.
- Keep both functions pure: no database access, no logging, no globals.
- Keep `SourceKey` the only derivation of `ingestion_scopes.source_key`.
- Never import the parent `postgres` package from here.

## Common changes

- Changing `SourceKey`'s fallback changes a persisted column and the
  deferred-maintenance lock key for scopes with no partition key. Update
  both tests in `source_key_test.go` and trace every caller in the parent
  package before doing it.
- A new scope-ID prefix for `RepoIDFromScopeID` needs a table case in
  `repo_id_test.go`.

## Failure modes

- Importing the parent `postgres` package creates an import cycle.
- A second, drifting source-key derivation lets the stored `source_key` and
  the deferred-maintenance lock fallback (scopes with no partition key)
  disagree.

## Verification

From `go/`, run:

```bash
go test ./internal/storage/postgres/... -count=1
go vet ./internal/storage/postgres/...
```

Run `scripts/verify-package-docs.sh` from the repository root.
