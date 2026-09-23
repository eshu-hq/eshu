# internal/storage/postgres/migrations

The embedded Postgres bootstrap SQL files (`*.sql`) plus the Go leaf that
owns their `//go:embed` and ordering: `Definition`, `BootstrapDefinitions`,
and `Checksum`.

## Why this package exists

Before #6693 D2, root's `schema.go` carried `//go:embed migrations/*.sql`
even though the files it embeds live one directory away. That split the
migration content from the code that reads it. This leaf collapses the two:
the `.sql` files and the embed directive that reads them are one package, so
the directory is the single source of truth root already documented it as.

## Exported surface

- `Definition` -- one ordered bootstrap SQL payload: `Name`, `Path`, `SQL`,
  plus `Variant` and `FullChecksum` for the deferred-content-search-index
  variant (root's `BootstrapDefinitionsWithoutContentSearchIndexes` sets
  these; most callers leave them zero).
- `BootstrapDefinitions()` -- reads every `*.sql` file from the embed,
  derives `Name` by stripping the `NNN_` prefix and `.sql` suffix, and
  returns them sorted by `Path`. Files without an `NNN_` prefix are skipped.
- `Checksum(statement)` -- sha256 hex digest of a migration statement, the
  value recorded in the `eshu_schema_migrations` ledger's `checksum_sha256`
  column.

## Invariants

- **Never edit a shipped `.sql` file, for any reason, once it has merged.**
  Widen or fix behavior through a new migration instead. `#7002`:
  `093_cross_scope_completion_queue.sql` shipped, was edited in place twice
  (`#6785`, then `#6923`) to widen a `CHECK` constraint and a trigger's `WHEN`
  list, and both edits broke `applyTrackedDefinitions`'s checksum guard for
  every database that had already recorded 093 -- including a stuck ops-qa
  rollout. `093` was restored to its originally shipped bytes; `112` and `120`
  are the correct pattern for that same widening (a new file, guarded by
  `pg_get_constraintdef`/`pg_get_triggerdef` `NOT LIKE` checks, that converges
  an existing database once and no-ops on every boot after). `112` and `120`
  are themselves now shipped and must never be edited either, including to
  "fix" the fresh-bootstrap comment at their own top: a fresh bootstrap runs
  `093` (original) then every later `.sql` file in path order, so the current
  full domain list lives in whichever upgrade file was added last (`120`
  today) -- see `checksum_alias.go` for the narrow,
  path-scoped exception that let already-applied databases from the `#6785`/
  `#6923` window keep working, and
  `../cross_scope_completion_schema_test.go`'s
  `TestCrossScopeCompletionSchemaCoversCatalogDomainsExactly` for the test
  that must track whichever file is current.
- `Path` stays exactly `go/internal/storage/postgres/migrations/<file>.sql`.
  The migration tracker (`schema_bootstrap_lock.go` in root) keys applied
  migrations by `path + variant + checksum_sha256`, so any change to `Name`,
  `Path`, `SQL`, or definition order makes bootstrap try to re-apply or
  diverge on migrations already applied to an existing database. See
  `embed_invariant_test.go`'s golden digest and
  `migration_checksum_manifest_test.go`'s per-file manifest (the latter names
  the exact file when one drifts or is deleted).
- The embed pattern is `*.sql` only. `embed.go`, `doc.go`, `README.md`, and
  `AGENTS.md` must never appear as definitions.
- No `.sql` file in this directory is added, removed, renamed, or edited by
  this package split -- the split only moves where the embed directive
  lives.
- Root's `BootstrapDefinitionsWithoutContentSearchIndexes` stays in root: it
  needs the content store's deferred DDL, which this leaf does not own.

## Verification

```bash
cd go && go test ./internal/storage/postgres/migrations -count=1
cd go && go test ./internal/storage/postgres/... -count=1
```
