# postgresproof agent instructions

## Read first

- `README.md`
- `doc.go`
- `database.go`
- `database_test.go`
- `schema.go`
- `schema_test.go`

## Invariants

- Keep this package test-only and free of production runtime dependencies.
  Only `_test.go` files import it.
- `OpenDisposableDatabase` rejects unsafe DSNs before opening a connection or
  issuing DDL, and requires the explicit disposable opt-in and the
  administrative `postgres` database.
- `OpenIsolatedSchema` checks neither. It opens the DSN it is given, installs
  `pg_trgm` in that database's `public` schema, and creates and drops only the
  schema it generated (`prefix_<unix nanos>`). The `live-postgres-readiness`
  runner refuses a proof DSN that does not name the administrative `postgres`
  database or lacks its `*_DISPOSABLE=1`; a run outside that runner is not
  checked, so point it only at a disposable server.
- Create random proof databases and schemas, and clean up only names this
  package generated.
- Add a failing safety regression before changing validation or cleanup
  logic, including adding a DSN guard to `OpenIsolatedSchema`.

## Common changes

- Add validation cases to `database_test.go` before changing DSN rules.
- Keep generated database names inside the fixed `eshu_content_index_proof_`
  namespace.
- `OpenIsolatedSchema` takes the schema setup as a function; never import a
  storage package here (storage/postgres tests import this package).
- No code outside this package references `TrigramExtensionLockKey`; callers
  install `pg_trgm` through `InstallTrigramExtension`. Any other `pg_trgm`
  install on a shared proof server must take the same key.
- Verify cleanup through a disposable PostgreSQL instance, never a retained
  Eshu database.

## Failure modes

- Reusing `ConnConfig.ConnString()` after changing `Database` reconnects with
  the original connection string. Register the copied config with `stdlib`.
- Closing a target connection does not remove the database. Cleanup must close
  it and then issue `DROP DATABASE ... WITH (FORCE)` through the administrative
  connection.

## Anti-patterns

- Do not weaken the opt-in to accept truthy strings.
- Do not accept arbitrary administrative database names.
- Do not log credentials or complete DSNs.
