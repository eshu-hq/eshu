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
- `OpenBootstrapTemplate` builds one template database per (proof server,
  flavour) per test process with the caller's apply and caches it;
  `OpenBootstrapTemplateClone` (or the error-returning
  `CloneBootstrapTemplate`) clones it into a fresh disposable database.
  `DiscardBootstrapTemplates` drops every template the process built, called
  from the owning test package's `TestMain` (template_clone.go). Flavours are
  pinned apply shapes shared through `FlavourPublicBootstrap`,
  `FlavourPublicWithoutContentSearchIndexes`, and `FlavourIsolatedBootstrap`
  (the 276-shape production bootstrap in a fixed-name schema first on the
  search path, pg_trgm in public); distinct apply shapes need distinct
  flavours.

The godoc contract is in [doc.go](doc.go).

## Dependencies

`OpenDisposableDatabase` uses `pgx` to parse the administrative DSN and opens
a copied connection configuration whose database name is the generated proof
database and whose search path is `public`. Creating from `template0` excludes
extensions and relations installed in `template1`. Cleanup is registered
immediately after successful creation, closes the target pool, terminates the
database's other backends, and force-drops with a bounded SQLSTATE 55006 retry
(#9985), and reports cleanup errors. Every connection it opens to that database runs the infra read
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

## No-Regression Evidence (#9985)

- No-Regression Evidence: projector deadlock load test green 6 consecutive runs (claims 668–2071, 0 deadlocks, 0 errors, 0 overlapping leases), teardown clean every run; contention intact.
- Performance Evidence: teardown-only path; worst case about 2 s inside the 15 s cleanup budget, after all assertions; claim SQL and worker counts untouched.
- No-Observability-Change: test-only change, no runtime signal. Baseline: pre-fix teardown flaked with 55006 on loaded runners (local PG 18).

## Performance Evidence (#10148 PR-A)

- Performance Evidence: one template build per (proof server, flavour) per
  test process replaces one full bootstrap per caller. Local backend:
  PostgreSQL 18.6, 306-migration bootstrap. After: the committed
  differential builds 2 templates + 2 fresh bootstraps + fingerprints in
  11.5 s; the concurrency proof reuses the flavour-A template for 4
  parallel clones + fingerprints in 10.4 s; a converted gate test through
  the clone funnel passes in 0.91 s. Runner-side before/after belongs to
  the gate dispatch the coordinator owns (ruling dispatch 2 intact).
- No-Regression Evidence: the committed differential proves clone output
  identical to a fresh real bootstrap on every catalog line for both
  flavours (`bootstrap_vs_clone=true`); the template is built by the
  production entry point from `template0` in the same process, never reused
  across processes; the default direct-bootstrap path is unchanged for
  callers that do not opt in.
- No-Observability-Change: test-only helper, no metric, span, log key, or
  status surface touched; see Telemetry above.

## Performance Evidence (#10148 PR-B)

- Performance Evidence: one isolated-flavour template build per proof
  server per test process replaces one full bootstrap per isolated-schema
  caller (the retention family and the winners resweep proof). Local
  backend: PostgreSQL 18.6, 309-migration bootstrap (282 applied in the
  isolated flavour). After: the committed differential builds 3 templates +
  3 fresh bootstraps + fingerprints in 11.4 s; the isolated concurrency
  proof builds the template once, then 1 reference + 4 parallel clones +
  fingerprints in 29.9 s; a converted retention proof through the clone
  funnel passes in about 1 s after the process template exists.
  Runner-side before/after belongs to the gate dispatch the coordinator
  owns.
- No-Regression Evidence: the committed differential proves clone output
  identical to a fresh real bootstrap on every catalog line for all three
  flavours (`bootstrap_vs_clone=true`); the isolated parallel clones match
  the template fingerprint and the parked-session clone fails with SQLSTATE
  55006 after the bounded retry; the isolated template is built by the
  production entry point from `template0` in the same process, never reused
  across processes; the default direct-bootstrap path is unchanged for
  callers that do not opt in.
- No-Observability-Change: test-only helper, no metric, span, log key, or
  status surface touched; see Telemetry above.

## Performance Evidence (#10148 PR-A follow-up)

- Performance Evidence: fix2 refactors `CloneBootstrapTemplate` to expose
  the generated clone name and verifies the drop with one extra
  `SELECT EXISTS` probe against `pg_database` plus one fresh administrative
  handle per clone, after assertions inside test cleanup. No hot-path
  change: clone creation DDL is untouched. Fix2 proof: `postgresproof`
  package green (2.327 s, 3.378 s); `storage/postgres` suite green
  (27.414 s, 93.710 s) per `/tmp/eshu-10148-s3fix2-prepush.log`.
- No-Regression Evidence: fix2 leaves the clone DDL path untouched, so
  the PR-A `bootstrap_vs_clone=true` differential it ran against still
  applies; the fix2 proof ran both changed packages green with and without
  race. Three scripted unit tests pin the new contract: leaked clone
  reports loudly, probe failure reports loudly, clean drop stays silent.
- No-Observability-Change: test-only helper, no metric, span, log key, or
  status surface touched; the verification reports via `t.Errorf` test
  output only. See Telemetry above.

## Performance Evidence (#10148 fix3 hermetic pin)

- Performance Evidence: fix3 is a behavior-preserving extraction
  (`registerCloneTeardown`: same verify-then-cleanup registration order as
  the two inline `t.Cleanup` calls it replaces) plus routing the
  `pg_database` probe through `newCloneExistenceProber`. No hot-path
  change: clone creation DDL and the drop path are untouched. Fix3 proof:
  3 new hermetic tests PASS (`ok 0.322s`, rerun `ok 0.609s`), no live
  server dialed; `postgresproof` package vet clean.
- No-Regression Evidence: registration order identical to before, so the
  PR-A `bootstrap_vs_clone=true` differential still applies; pre-existing
  tests in the file untouched. The LIFO wiring test replays the recorded
  registration in reverse and fails the swapped-order mutant.
- No-Observability-Change: test-only helper, no metric, span, log key, or
  status surface touched; reports via `t.Errorf` test output only.
  Baseline: unmerged tip `ccac709983`; rebased onto `87d4e7cab` with no
  content change. See Telemetry above.
