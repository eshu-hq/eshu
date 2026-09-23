# Postgres scope helpers

## Purpose

This package derives stored values from an ingestion scope. It is the
`scope/` leaf of the storage/postgres split (#6693) and, for now, holds only
the helpers the split hoisted out of the parent package.

## Ownership boundary

This package owns two pure derivations: the `ingestion_scopes.source_key`
value and the own-repo_id hint for git repository scopes. The parent
`postgres` package owns the SQL that writes and reads those values, the
ingestion commit, the deferred relationship backfill, and the
deferred-maintenance lock. `internal/scope` owns the `IngestionScope` type.

## Exported surface

- `SourceKey(scope.IngestionScope) string`
- `RepoIDFromScopeID(scopeID string) string`

See `doc.go` for the godoc contract.

## Dependencies

- `internal/scope` for `IngestionScope`.

## Telemetry

None. Both functions are pure string derivations; their callers own the
spans and metrics around the statements that use the result.

## Gotchas / invariants

- `SourceKey` must stay the single source of the `source_key` value. The
  ingestion commit writes it, and the deferred-maintenance lock falls back
  to it when a scope has no partition key; a second derivation could key
  that lock differently from the stored column.
- `RepoIDFromScopeID` is a performance hint. A wrong or empty result only
  moves rows from the fast arm to the fallback arm of the deferred
  relationship query; it never changes which rows match.
- Do not import the parent `postgres` package: that is an import cycle.

## Related docs

- [Postgres storage](../README.md)
- [storage/postgres target tree](../../../../../docs/internal/design/6693-postgres-target-tree.md)

## Verification

From `go/`, run `go test ./internal/storage/postgres/... -count=1` and
`go vet ./internal/storage/postgres/...`. From the repository root, run
`scripts/verify-package-docs.sh`.
