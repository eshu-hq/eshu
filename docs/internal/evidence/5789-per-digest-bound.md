# #5789 — per-digest bound on the supply-chain cloud-runtime probe

## The bug, quantified

The probe promotes a finding to `deployment_truth_tier=runtime_confirmed` when a
current, authorized `CloudResource` runs the finding's subject digest. It bounded
the owner-ledger read with ONE total-row cap (200) across every digest on the
page, applied after `ORDER BY (digest, arn, uid)`.

A total cap does not share. Measured on a skewed corpus — one digest running on
30,000 resources, twenty others on 100 each, 21 digests requested:

```
                     plan                            digests   rows    exec
ARM A  Index Scan, one scan + global LIMIT 200          1/21     200   0.142 ms
ARM B  Nested Loop + per-digest Limit (loops=21)       21/21     210   0.286 ms
```

Which digest wins is deterministic but arbitrary — the lexicographically first:

```
 digest                                                                  | count
 sha256:0000000000000000000000000000000000000000000000000000000000000000 |   200
(1 row)
```

**Twenty of twenty-one findings received no runtime evidence at all.** Not a
truncated answer, a missing one — and invisible, because a finding with no
runtime evidence is indistinguishable from a finding whose image runs nowhere.

## The fix

`buildCloudResourceRuntimeDigestQuery` drives a `CROSS JOIN LATERAL` from the
distinct digest set, so each digest gets its own bounded, ordered index scan
capped at `supplyChainCloudRuntimeProbePerDigestLimit(len(digests))` — the 200-row
total budget shared across the page, with a floor of
`supplyChainCloudRuntimeProbePerDigestMinResults` (10). A 21-digest page lands on
that floor; a single-digest page keeps all 200.

Both arms stay on `graph_node_owner_cloud_resource_runtime_digest_idx` — no seq
scan, no fallback. Total work is bounded and deterministic at
`len(digests) x perDigestLimit`. Because the limit floors at 10 and the digest
count is capped by `supplyChainCloudRuntimeProbeMaxDigests` (200), the worst case
is 2000 rows — up from the old 200, bounded and stated rather than incidental.

Ten per digest is sufficient for the decision being made: `runtime_confirmed`
needs only that at least one current, authorized resource runs the digest, so a
bounded sample answers it.

No-Regression Evidence: ARM A 0.142 ms -> ARM B 0.286 ms on the skewed corpus
above (Postgres 17, `EXPLAIN (ANALYZE, BUFFERS)`). Both index-backed; the
increase is 21 bounded index scans replacing one, on a sub-millisecond query,
and it buys correct answers for 20 findings that previously got none.

Observability Evidence: no new metric or span. The probe's existing
`eshu.subject_digest_count` / `eshu.runtime_confirmed_digest_count` span
attributes become meaningful rather than misleading — before this change the
confirmed count could only ever reflect one digest per page on a skewed corpus.

## Proof

`TestCloudResourceRuntimeDigestPerDigestBoundPreventsStarvationLive` seeds one
digest on 600 resources plus twenty on 20 each, calls the production
`CurrentAuthorizedCloudResourcesByDigest`, and requires every requested digest to
be represented, no digest to exceed the bound, and two identical calls to return
identical ordered rows.

Against the pre-fix global cap it fails exactly as the bug predicts:

```
20 of 21 digests got NO runtime evidence (hot digest returned 200 rows)
```

## Why eligibility runs BEFORE the bound

The first draft bounded rows and filtered after. That is what the original code
did, and it is wrong for the same reason at a smaller scale: a digest whose
first N `(arn, uid)` rows are stale, tombstoned, or outside the caller's grants
returns nothing even though a later row is current and authorized.

Measured on the pathological shape — one digest on 50,000 resources where only
the LAST 50 (by arn order) are authorized for a scoped caller:

```
ARM 2  bound candidates (200), then filter    0.449 ms   rows=0    <- WRONG
ARM 1  eligibility inside, bound eligible     93.193 ms  rows=50   <- correct
       (Index Scan, Rows Removed by Filter: 49950)
```

**The fast shape returns the wrong answer.** It reports a genuinely running
vulnerable image as not running, which is the failure this whole issue is about,
reached from the other direction.

So eligibility moved inside the LATERAL, before its LIMIT. The cost is real and
bounded: the scan runs until it finds K eligible rows, so the worst case is one
full index-range scan of a single digest. 93 ms on 50,000 rows sits well inside
this store's own `cloudResourceListInteractiveSLO` of 2 seconds, and the shape
that produces it — a hot digest where nearly every resource is unauthorized for
the caller — is the rare one.

A hybrid (inner candidate bound, outer eligible bound) was considered and
rejected: it reintroduces ARM 2's wrong answer for exactly the rows the inner
bound cuts. Getting both would need eligibility in the index, which does not
exist.

No-Regression Evidence: the healthy shape is unchanged — when eligible rows are
not concentrated at the end, the scan stops as soon as it has K, which is the
0.286 ms measured above. The 93 ms figure is the deliberate worst case, stated
rather than discovered later.

## Side effect on scoped callers, deliberate

With eligibility inside the bound, a scoped caller now receives a full budget of
rows it can actually see, rather than the survivors of a budget spent mostly on
rows it cannot. On the existing hot-candidate fixture that changes the scoped
result from ~75 rows to the full bound. That is more evidence, not different
evidence, and it is the direct consequence of counting the right thing.

## Live fixtures use a private schema, not TEMP tables

The two guards in
`go/internal/query/cloud_resource_runtime_digest_starvation_live_test.go` first
seeded `CREATE TEMP TABLE` fixtures behind `db.SetMaxOpenConns(1)`. A TEMP table
belongs to the session that created it, and `database/sql` owns session
lifetime, not the test. `SetMaxOpenConns(1)` caps how many connections are open
at once; it does not pin identity. Let the server close the connection, or an
idle timeout or a network blip drop it, and `database/sql` opens a replacement
whose `pg_temp` is empty.

Measured both halves against Postgres 17.10, terminating our own backend with
`SELECT pg_terminate_backend(pg_backend_pid())` and re-running the same
unqualified query:

```
clean database
  TEMP table    + SetMaxOpenConns(1)  ->  ERROR: relation "ingestion_scopes" does not exist (SQLSTATE 42P01)
  private schema via search_path      ->  count=1, err=<nil>

database that already holds ingestion_scopes in public
  TEMP table    + SetMaxOpenConns(1)  ->  count=2, err=<nil>   <- silently read the WRONG table
  private schema via search_path      ->  count=1, err=<nil>
```

The second row is the one that matters. `ingestion_scopes`, `scope_generations`,
`fact_records`, and `graph_node_owner` are real Eshu tables (migration
`001_ingestion_scopes.sql` onward), so on a migrated database the reconnect does
not raise a missing-relation error at all. It resolves the unqualified name
against the real, empty tables and the guard fails as
`21 of 21 digests got NO runtime evidence` — reading exactly like the starvation
regression it exists to catch, and sending whoever hits it in CI to the wrong
place.

The fixtures are now ordinary tables in a schema named per test run
(`eshu_digest_fixture_<pid>_<nanos>`), dropped in `t.Cleanup`. `search_path` is
carried as a pgx connection runtime parameter rather than issued once as
`SET search_path`, so Postgres applies it during startup on every connection the
handle opens, replacements included; a `SET` would be session-scoped and have
the same defect. `public` is deliberately left out of the path, so a name the
fixture forgot to create fails loudly instead of resolving against whatever the
target database already holds.

A pinned `*sql.Conn` from `db.Conn(ctx)` was the other candidate and was
rejected: `PostgresCloudResourceListStore` takes a `*sql.DB`, so pinning would
mean widening the production constructor to a connection-shaped interface purely
for test plumbing. The schema fix needs no production change and holds under
parallel runs against a shared database.

Both guards still fail against their mutations after the change — the
eligibility guard returns `matches = 0, want exactly 1` when the eligibility
predicate is moved out of the `CROSS JOIN LATERAL` to the outer query, and the
starvation guard reports `20 of 21 digests got NO runtime evidence (hot digest
returned 10 rows)` when the per-digest `LIMIT` is replaced by a single global
one. Ten consecutive runs of both against the fixed query pass.

### The runtime parameter has its own guard

Neither starvation guard ever loses a connection, so both keep passing against a
session `SET search_path`. Measured, with the runtime parameter swapped for a
one-off `SET`:

```
--- FAIL: TestRuntimeDigestFixtureSchemaSurvivesReconnectLive
    query after the reconnect: ERROR: relation "fixture_marker" does not exist (SQLSTATE 42P01)
--- PASS: TestCloudResourceRuntimeDigestPerDigestBoundPreventsStarvationLive
--- PASS: TestCloudResourceRuntimeDigestBoundCountsEligibleRowsOnlyLive
```

`TestRuntimeDigestFixtureSchemaSurvivesReconnectLive` is the one test that tells
the two shapes apart. It runs under `SetMaxOpenConns(1)` — the pool setting the
TEMP-table fixtures relied on — kills its own backend with
`SELECT pg_terminate_backend(pg_backend_pid())`, then requires the replacement
connection to report a different `pg_backend_pid()`, a `current_schema()` equal
to the fixture schema, and an unqualified read of a fixture table seeded before
the kill.

### The DSN role now needs CREATE on the database

TEMP tables need no grant. A per-run schema needs `CREATE` on the database, so a
role provisioned with only `TEMPORARY` fails here for the first time. It fails
loudly and names the grant rather than skipping:

```
role "temponly" lacks CREATE on database "eshu", so this test cannot create its per-run fixture schema.
Run: GRANT CREATE ON DATABASE "eshu" TO "temponly";
```

Skipping was rejected. These two guards are the only thing between the #5789
starvation bug and a silent return, and a guard that quietly does not run is the
same shape of defect as the bug. Falling back to TEMP tables was rejected
outright — it restores the reconnect defect. Without the precheck the same role
gets `ERROR: permission denied for database eshu (SQLSTATE 42501)` from
`CREATE SCHEMA`, which names neither the privilege nor the fix.

### Leftover schemas are swept at start, not only dropped at end

`t.Cleanup` does not run on `SIGKILL`, a killed container, or a timeout that
cannot unwind, so on a shared live-test database the per-run schemas would
accumulate without bound. `openRuntimeDigestFixtureDB` now sweeps before it
creates its own, which is what makes a killed run self-heal on the next one.

The sweep deletes schemas, so its scoping is the part that matters. A name is
dropped only when it clears all three:

1. `starts_with(nspname, 'eshu_digest_fixture_')` in SQL — a literal prefix, not
   `LIKE`, whose `_` would act as a single-character wildcard.
2. A full-anchored match on the helper's own naming pattern,
   `^eshu_digest_fixture_[0-9]{1,10}_([0-9]{1,19})$`, built from the same prefix
   constant the names are built from.
3. The nanosecond timestamp inside the name parses and is more than two hours
   old. A future-dated name is left alone; a backwards clock is not a reason to
   delete somebody else's schema.

`TestRuntimeDigestFixtureSweepDropsOnlyStaleFixtureSchemasLive` seeds one schema
the sweep must drop and four it must not, then opens a second fixture handle and
lets that handle's own start-of-run sweep do the work — calling the sweep
function directly would pass just as well with the sweep unwired. Each of the
three scoping rules was removed in turn and the guard failed:

```
sweep unwired from openRuntimeDigestFixtureDB
  schema eshu_digest_fixture_98485_1786480783485819000 survived the sweep, want it dropped

age gate removed
  schema eshu_digest_fixture_99778_1786567228836538000 was dropped by the sweep, want it left alone
  drop fixture schema eshu_digest_fixture_99778_1786567228312029000: ERROR: schema ... does not exist
  (the sweep destroyed a live concurrent run's own schema)

trailing pattern anchor removed
  schema eshu_digest_fixture_198_1786480871841818000_extra was dropped by the sweep, want it left alone
```

Schema identifiers go through `pgx.Identifier{name}.Sanitize()` rather than being
hand-wrapped in double quotes. For the names generated today the two agree
exactly, so this changes no behaviour; it stops a future naming change from
producing a name that escapes its own quotes:

```
input   : eshu_digest_fixture_"; DROP SCHEMA public CASCADE; --
manual  : "eshu_digest_fixture_"; DROP SCHEMA public CASCADE; --"
sanitize: "eshu_digest_fixture_""; DROP SCHEMA public CASCADE; --"
```

No-Observability-Change: test-fixture isolation only; no production code, query
text, metric, span, or log changed.
