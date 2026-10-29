# postgresproof

## Purpose

`postgresproof` creates an empty PostgreSQL database, or an isolated schema,
for live tests that need destructive schema or data setup.

## Ownership boundary

This package owns proof-database and proof-schema creation, connection
setup, and cleanup, and the DSN validation of `OpenDisposableDatabase`. It does not bootstrap Eshu's schema or seed fixtures. The calling test
owns those steps after it receives the isolated connection.

## Exported surface

- `OpenDisposableDatabase` creates a random database from `template0` and
  drops it during test cleanup (database.go).
- `OpenDisposableBootstrapDatabase` opens that disposable database and runs
  the caller's apply (usually the full production bootstrap) on it before
  returning, so public-qualified `SECURITY DEFINER` bodies install into
  `public` (database.go). The caller passes the administrative DSN and the
  explicit opt-in; neither is derived.
- `OpenIsolatedSchema` creates and drops a timestamped schema in the
  database its DSN names and runs the caller's schema setup on it; it is
  subset-DDL-only since the #9299 lane (a full bootstrap must use
  `OpenDisposableBootstrapDatabase`), so this package still owns no Eshu
  schema (schema.go).
- `InstallTrigramExtension` installs `pg_trgm` in `public` under the
  transaction advisory lock `TrigramExtensionLockKey`, so concurrent proofs on
  one server install it one at a time. No code outside this package
  references the constant.
- `DeferredPartitionProofDSN` reads `ESHU_DEFERRED_PARTITION_PROOF_DSN`, then
  `ESHU_LATEST_GENERATION_PROOF_DSN`, or skips.
- `RaisePoolForConcurrentClaimers` sizes a proof pool to one pinned
  connection per concurrent claimer plus slack, so claim-race proofs never
  serialize behind one connection (pool.go). Callers prove the concurrency
  is real with distinct `pg_backend_pid` values and pool stats, not sleeps.

The godoc contract is in [doc.go](doc.go).

## Dependencies

`OpenDisposableDatabase` uses `pgx` to parse the administrative DSN and opens
a copied connection configuration whose database name is the generated proof
database and whose search path is `public`. Creating from `template0` excludes
extensions and relations installed in `template1`. Cleanup is registered
immediately after successful creation, closes the target pool before dropping
the database, and reports cleanup errors. Every connection it opens to that database runs the infra read
model's derive-aware writer `SET`

No-Regression Evidence: moving proof creation from `template1` to `template0`
with a pinned `public` search path changes only test databases, never a
production query or queue path. The isolation live test fails on a
template-sourced database with extensions=1 public_relations=1 and passes
pristine in 0.24s; the queue fence fixture passes in 5.30s and 10.27s; the
readiness hermetic suite passes at 242 proofs. Backend: local PostgreSQL
16.15, with the CI PostgreSQL 18 wall time recorded before merge.

No-Observability-Change: no metric, span, log key, or status surface is
touched; the helper reports only test errors and cleanup errors.
(`storage/postgres/infra/inventory.WriterConnectOption`), the same setting
`runtime.OpenPostgres` applies, so a proof that seeds infra-typed content rows
cannot leave rolling-upgrade fence marks behind. That is the package's only
Eshu import. `OpenIsolatedSchema` opens plain `pgx` pools without that `SET`,
as the storage/postgres helper it replaced did.

## Telemetry

This test helper emits no metrics, spans, or structured logs. Test failures
identify the failed lifecycle step without printing the DSN.

## Gotchas / invariants

`OpenDisposableDatabase` must never accept an application database such as
`eshu`, and tests that use it must not issue `DROP SCHEMA` against its
administrative connection. Its opt-in value must be exactly `1`, and the DSN's
database must be exactly `postgres`. Its cleanup force-drops only a name it
generated.

`OpenIsolatedSchema` applies none of those checks. It creates its schema in
whatever database the DSN names, installs `pg_trgm` there, and at cleanup runs
`DROP SCHEMA ... CASCADE` on that generated schema only, through its own
connection. Its callers take the DSN from `DeferredPartitionProofDSN` or
their own variables. The `live-postgres-readiness` runner refuses a proof DSN
that does not target the administrative `postgres` database or lacks its
`*_DISPOSABLE=1`; a local run outside the runner is not checked, so point it
only at a disposable server.

## Related docs

The repository's live-test and database verification rules are in
[`docs/public/reference/local-testing.md`](../../../../../docs/public/reference/local-testing.md).

Focused verification:

```bash
cd go
go test ./internal/testutil/postgresproof -count=1
```
