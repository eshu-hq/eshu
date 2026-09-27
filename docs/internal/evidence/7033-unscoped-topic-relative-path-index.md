# #7033 — unscoped topic relative-path index

## Decision and scope

`investigate_code_topic` can probe `content_files.relative_path` without a
repository constraint. The primary key starts with `repo_id`, so it cannot
bound that substring probe. Migration 131 adds
`content_files_relative_path_trgm_idx` as a `gin_trgm_ops` index. It runs with
`CREATE INDEX CONCURRENTLY` for normal upgrades. Cold bootstrap records its
deferred no-op variant and `EnsureContentSearchIndexes` builds the same index
only after the write-heavy projection drain.

A populated live upgrade must use migration 131's concurrent build, then
validate the catalog before migration 132 publishes readiness.
`EnsureContentSearchIndexes` is a non-concurrent, transactional finalizer for
deferred bootstrap, not a live substitute for migration 131.

Migration 132 extends `eshu_content_substring_indexes_valid()` to require the
exact path-index shape. A wrong, partial, or invalid same-name index prevents
the ready state and guarded reads; an operator must remove it before the
finalizer can build the exact index. An absent index can be built by the
finalizer on deferred cold bootstrap.

## Performance Evidence

Performance Evidence: the candidate was measured before implementation with
the same local corpus, backend, storage state, and interleaved request order.
The full query used the same unscoped 16-term topic request in every sample;
its result digest was identical across old and new plans and contained 5,235
rows.

| Measurement | No path GIN median | Path GIN median | Result |
| --- | ---: | ---: | --- |
| Full 16-term unscoped topic query | 72,172.144 ms | 137.006 ms | Same 5,235-row digest |
| Isolated `relative_path` probe | 293.461 ms | 3.178 ms | Candidate predicate uses GIN |
| Concurrent index build, 145,000 rows | N/A | 558 ms median, 12 MiB | One-time upgrade cost |
| Cold ingest | Baseline | +24.9% when built during ingest | Deferred by design |

The cold-ingest cost is why the migration is not built during deferred
bootstrap. These are local Postgres measurements, not an ops-qa endpoint
claim. The earlier live ops-qa endpoint observation was 21.463 s before the
approved path-index rollout. After that rollout, the exact canonical 16-term
HTTP request took 11.521734 s on its first observed call, then 2.279568 s
and 2.284533 s on two repeats. These are **not interleaved before/after**
measurements and do not establish a speedup. The matched built-endpoint proof
below is separate from these historical calls and from the SQL shim.

## Built-endpoint Neo4j/Postgres proof

Two local API binaries built with Go 1.26.6 from the exact baseline parent
`6f2672728` and candidate commit
`968f7b8f94ad22d7f6de277a3aa7330e62f99d34` used the same live ops-qa
Postgres corpus and Neo4j backend through loopback, direct-Pod port forwards.
Their SHA-256 digests were respectively
`62654830e7fa74d7cc403b1ab00b6151059de204f56951fdb1b92153826ddb95`
and `d8e84ff61a21e31846de5a52ac3545e81503c47fdc5306cdaea055f6d4250e53`.
Both API processes used the production query profile and a Postgres DSN with
`default_transaction_read_only=on`, verified before startup. Graph backfill
markers were complete. The request was `POST /api/v0/code/topics/investigate`
with topic `config`, the 16 terms listed below, limit 25, and offset 0.

After one warmup per binary, five ABBA blocks returned HTTP 200 on all 20
calls. The baseline median was **2.107273 s**, the candidate median
**0.979321 s**, against the **<1 s** budget. The corpus fingerprint before
and after was 144,838 `content_files`, 2,640,262 `content_entities`, and
`max(indexed_at)=2026-09-27 00:08:45.783822+00`. The baseline samples in
seconds were `2.076486, 2.124461, 2.090724, 2.138238, 2.324484,
2.078702, 2.030057, 2.126646, 2.123823, 2.060433`; candidate samples
were `0.967924, 0.932942, 0.972175, 1.365053, 1.068303, 1.005354,
0.961492, 0.991513, 0.986466, 0.971270`. Several candidate calls exceeded
1 s, so the median has a narrow margin; it is not a tail-latency guarantee.
An earlier attempt was discarded when a service port-forward emitted an error
stream timeout. Later sporadic long calls were not assigned a root cause and
are not folded into a speedup claim.

Behavior controls against those same built endpoints were:

| Request | Baseline/candidate result | Contract observation |
| --- | --- | --- |
| `backpressure,microbenchmark`, limit 100 | 40 rows each; normalized count, truncation, and matched-row digest `4951106e351d39d90734b8bfab820a6b01b487ea52b2d4245603a82abf862191` on both | Uncapped ranked result matches. |
| `issue7033nohitzzx`, limit 100 | Zero rows each; response digest `9d6ea2d98a82ec4de7ffdb62f744d8c45587c791872c32100ef931ecad9a46d3` on both | Empty result matches. |
| `deployment`, limit 100 | 100 rows each, both truncated; response digest `8cfe4586be098b981642a791e37ef2b13c9f51b24886dc1f952a5c6cc70ef10c` on both | This capped sample happens to match; no general capped parity claim. |
| Canonical 16 terms, limit 25 | 25 rows each, both truncated; baseline 25 matched files/zero matched symbols, candidate 20 matched files/25 matched symbols | Ranked pages differ. The conditional parity claim applies only when the candidate pool is not truncated. |

This is a **local built-endpoint** comparison on live ops-qa data, not an
in-cluster canary acceptance measurement. The candidate `/readyz` returned
503: ops-qa has the earlier path-index migration receipts but not this
branch's renumbered migration 132, although the exact path GIN index exists
and the prior substring-index state is `ready`. Baseline readiness returned
200. No candidate schema migration was applied during this read-only proof;
the reviewed scoped migration rollout and same-topology canary remain required
before a PR can claim deployment-ready performance.

## Read-only ops-qa query-shape proof

The explicit representative terms were `config,content,deployment,environment,
file,function,handler,module,package,path,repo,repository,resource,service,
source,system`, with page limit 25 and offset 0. A read-only PostgreSQL 18.3
client compared the deployed full SQL against a path-first file-pool shim in
five interleaved pairs within one repeatable-read snapshot, after a colder
warmup pair. The median was **2,061.122 ms deployed versus 894.598 ms shim**.
Both routes were capped; their ranked pages overlapped **0/25** in every pair.
This is permitted only by the issue's conditional parity criterion, which
requires identical ranked results when no term pool is truncated. It is not
evidence that the capped output is equivalent or higher quality.

The two-term `backpressure,microbenchmark` control was uncapped: three paired
reads yielded 3,202.448 ms deployed versus 3,263.443 ms shim, with all 40
ranked rows and scores identical. That small median difference is inconclusive
as a regression or no-regression claim. Path-sparse and no-hit broad-term
requests remained over the 1 s SQL-only budget, so this candidate is not a
universal subsecond result.

PostgreSQL does not promise `UNION ALL` result order, and an outer `LIMIT`
without `ORDER BY` does not guarantee the proposed path precedence. The
corrected read-only shim materialized each capped path pool and limited its
content-only pool to the remaining slots. An adversarial fixture returned zero
violations for caps, duplicates, path precedence, and uncapped set equality.
On the same canonical 16 terms, five interleaved same-snapshot file-stage
pairs (after warmup) measured **181.074 ms** for the unordered shim versus
**171.429 ms** for the count-gated shape. Both returned 4,000 candidates and
matched an aggregate checksum in each pair; this stage check is not a full
ranked-page comparison or a built API measurement. The corrected production
query's local built-endpoint proof is above; its in-cluster canary acceptance
proof remains pending.

## Lifecycle proof

Disposable PostgreSQL 18 live tests exercise the production tracked bootstrap
entry point rather than a direct `ApplyDefinitions` call:

- populated pre-131 ready state plus indexed content, tracked 131 concurrent
  migration, tracked 132 lifecycle migration, then a guarded unscoped
  `relative_path ILIKE` read;
- wrong btree and partial GIN same-name path indexes while the other three
  lifecycle indexes are exact; 132 moves state to `not_built`, finalization
  fails closed, then removing the malformed index lets the finalizer recover to
  `ready` with the exact GIN;
- invalid interrupted concurrent-index cleanup, concurrent production-entry
  migration calls, and reapply behavior.

The earlier twice-passing full-package live-test timings were not retained
with a source SHA and stdout receipt, so they do not validate this branch.
On source commit `968f7b8f94ad22d7f6de277a3aa7330e62f99d34`, the host's
complete `./internal/storage/postgres` package test failed during linking with
`mapping output file failed: disk quota exceeded`, before any test ran. A
controlled file-list comparison isolated the link-size trigger to the existing
`aws_bindings_test.go` blank import of every AWS service binding; excluding
that one file cut the compiled archive count from 1,965 to 1,004. A file-list
shard excluding it passed the affected live tests (27.200 s, exit 0), but was
only partial proof.

The same source commit then passed the **complete package** test in an
isolated `golang:1.26.6-bookworm` container (image index digest
`sha256:116d58cbd88c1297624acc6e967a060012422bacf9930927e23fb719189c6f36`).
The worktree was bind-mounted read-only, the module cache read-only, and Go's
build cache writable; all test files, including `aws_bindings_test.go` and the
external tests, remained in the package. The focused non-live command below
passed in 0.064 s (exit 0). With PostgreSQL 18.6 in an explicitly disposable
container and administrative database, the same complete package binary ran
the affected live tests in 27.274 s (exit 0):

```bash
# Inside the isolated Go container, from the worktree's go/ directory:
go test ./internal/storage/postgres \
  -run 'ContentFilesRelativePath|ContentSearchIndex|Issue7033' -count=1
ESHU_TEST_CONTENT_INDEX_POSTGRES_DSN='<disposable admin DSN>' \
  ESHU_TEST_CONTENT_INDEX_POSTGRES_DISPOSABLE=1 \
  go test ./internal/storage/postgres \
  -run '^(TestContentFilesRelativePathIndex.*Live|TestContentSearchIndex.*Live)$' -count=1
```

The disposable database, PostgreSQL container, and test container were
removed afterward. This local receipt does not authorize using the deferred
finalizer on a populated live database.

The opt-in `issue7033_rollout` test runner selects only the embedded, checksum-
matched migrations 131 and 132. It requires the `public` schema and verifies
the target system identifier, database, current schema, primary role,
prerequisite receipts and three exact existing GIN indexes in a read-only
preflight. It applies tracked migration 131,
checks the exact new index, then applies tracked migration 132 and checks the
four-index readiness contract. On a disposable PostgreSQL 18.6 database, the
runner applied those two migrations and retried idempotently. Wrong target,
incomplete index state, a mismatched prerequisite ledger receipt, and a
non-`public` search path all failed before target DDL. The cross-schema
regression was red first: with `search_path=custom,public`, the unguarded
runner created its index in `custom` before failing its `public` postcheck.
Separate live regressions prove both that migration 125 can remain unapplied
during this scoped rollout and that all unrelated migrations 125–129 can remain
unapplied while 131–132 run. Normal bootstrap subsequently applies 125–129
without replaying 131 or 132; the rescoped service index and content-index
readiness remain valid on disposable PostgreSQL 18.

An opt-in `issue7033_canary_startup` test used disposable PostgreSQL and
Neo4j to prove a Neo4j-backed API starts with both backfill markers complete,
disabled admin bootstrap and OIDC refresh, and a read-only PostgreSQL pool.
Health, readiness, and the code-topic route returned HTTP 200; no governance
audit event persisted during startup. This test does not prove an audit write
was attempted. This is local startup
proof, not a live after measurement. The owner separately approved a temporary
isolated ops-qa canary for that proof.

## Observability Evidence

No-Observability-Change: this adds no metric, span, log key, route, worker,
lease, or runtime knob. Normal migration uses the existing
`bootstrap.postgres.migration.concurrent_index_build` start/finish events;
deferred finalization continues to use existing Postgres query spans and
`eshu_dp_postgres_query_duration_seconds`. Operators can inspect the exact
catalog shape, `content_substring_index_state`, and those existing migration
and finalization signals when a guarded read remains unavailable.
