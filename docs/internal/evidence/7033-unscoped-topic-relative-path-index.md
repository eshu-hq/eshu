# #7033 — unscoped topic relative-path index

## Decision and scope

`investigate_code_topic` can probe `content_files.relative_path` without a
repository constraint. The primary key starts with `repo_id`, so it cannot
bound that substring probe. Migration 133 adds
`content_files_relative_path_trgm_idx` as a `gin_trgm_ops` index. It runs with
`CREATE INDEX CONCURRENTLY` for normal upgrades. Cold bootstrap records its
deferred no-op variant and `EnsureContentSearchIndexes` builds the same index
only after the write-heavy projection drain.

A populated live upgrade without the index must use migration 133's concurrent
build, then validate the catalog before migration 134 publishes readiness.
`EnsureContentSearchIndexes` is a non-concurrent, transactional finalizer for
deferred bootstrap, not a live substitute for migration 133.

Migration 134 extends `eshu_content_substring_indexes_valid()` to require the
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

## Pre-rebase built-endpoint Neo4j/Postgres proof

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
503: ops-qa has the earlier path-index migration receipts but not that
pre-rebase binary's then-numbered migration 132. The exact path GIN exists
and the prior substring-index state is `ready`. Baseline readiness returned
200. No candidate schema migration was applied during this read-only proof;
the reviewed scoped migration rollout and same-topology canary remain required
before a PR can claim deployment-ready performance.

## Rebased-source built-endpoint Neo4j/Postgres proof

The baseline binary was rebuilt from `c15539e80` (SHA-256
`f4120e5c0ce1d058d3058ae74e7184916d66ecd5155f8a7d8e3ce4648a9fbc75`);
the candidate was rebuilt from the rebased query commit `eef8c5331`
(SHA-256 `efc4a13fda8c7b2e5ff092939d0f787d9014c74e9a2f55728a5ef89a534f4a8e`).
Both used Go 1.26.6, the production query profile, local loopback listeners,
the same direct-Pod port forwards to live ops-qa Postgres and Neo4j, and a
Postgres connection with `default_transaction_read_only=on`. The corpus
fingerprint was identical before and after the timed run: 144,838
`content_files`, 2,640,262 `content_entities`, and
`max(indexed_at)=2026-09-27 01:18:00.134756+00`. No migration or canary was
applied during this read-only proof.

After one warmup per binary, three interleaved ABBA blocks of the explicit-term
request (`topic=config`, the 16 listed `terms`, limit 25, offset 0) returned
HTTP 200 on all 12 calls. The baseline
median was **2.328872 s** (`4.231396, 2.319620, 2.338123, 2.372944,
2.298328, 2.043465`); the candidate median was **1.164390 s** (`1.217053,
1.086313, 1.059552, 1.332855, 1.228724, 1.111727`). Connection setup
was below 2 ms on every timed call; the response wait dominated. This is
a measured improvement but **misses the <1 s budget**. It is not an
in-cluster canary acceptance result.

The canonical pages remained capped and different: baseline returned 25
matched files and zero matched symbols; candidate returned 17 matched files
and 25 matched symbols. Each binary produced a stable normalized digest
throughout the interleaved run. Fresh rebased-binary controls returned HTTP
200 and identical normalized count, truncation, file, and symbol digests
across binaries: `backpressure,microbenchmark` returned 40 uncapped rows
(`8d688ec1f2a26864361f5bbc13aad96ea19d39d650c41965463debdc4f61dfeb`),
`issue7033nohitzzx` returned zero uncapped rows
(`e89ae473762b9c8f73b8013739213cfe00f78f8f68884c66c05d23b67df4fce3`),
and `deployment` returned 100 capped rows
(`679cf927ee337b7c23ccac0cf9a400b8d073b4edbab6fde1c9d2e3019897b28e`).
These controls do not establish general parity for capped pools.

The earlier endpoint script uses a **different HTTP payload**: one topic
phrase, `config content deployment environment file function handler module
package path repository resource service source system workspace`, with limit
25 and no explicit `terms`. Its returned `searched_terms` were nevertheless
the same 16 explicit terms above (`repo` was included; `workspace` was not).
An overlapping read-only EXPLAIN invalidated an initial phrase timing attempt,
which was discarded. After the overlap ended, three new interleaved ABBA
blocks on this exact phrase payload returned HTTP 200 on all 12 calls. The
baseline median was **3.671899 s** (`6.228958, 4.749287, 3.087109,
3.532375, 3.119871, 3.811422`), and the candidate median was
**1.663488 s** (`2.679785, 2.696517, 1.447691, 1.701225, 1.625751,
1.321018`). The fingerprint was unchanged before and after: 144,838 files,
2,640,262 entities, `max(indexed_at)=2026-09-27 01:22:14.297564+00`.
This distinct request also improved but missed the <1 s budget. Its capped
baseline and candidate pages returned 25 files/zero symbols and 17 files/25
symbols, respectively. No in-cluster canary acceptance is implied.

## Remaining entity-probe diagnosis and rejected shims

On the rebased candidate, one read-only `EXPLAIN (ANALYZE, BUFFERS)` of the
generated canonical 16-term SQL reported 2,408.894 ms execution: 1,961.386 ms
in `entity_probe` and 396.647 ms in `file_probe`. The entity bitmap heap scan
was 1,918.288 ms, including a 1,412.320 ms bitmap OR and a 1,142.368 ms
`content_entities_source_trgm_idx` scan. This single instrumented plan
identifies a likely long pole; its times are **not** endpoint timings or an
interleaved before/after comparison.

Two read-only entity-stage shims ran in separate three-block ABBA experiments
inside repeatable-read snapshots on the same ops-qa corpus. Name-first with a
source-cache-only residual changed the entity-stage median from 716.057 to
99.301 ms, but the capped canonical top 25 overlapped **0/25** and their
scores fell from 5–7 to 2–3. Source-cache-first with a name-only residual
preserved the canonical entity top 25 and their 5–7 score range, but changed
the entity-stage median only from 755.615 to 726.086 ms. Both exactly matched
the uncapped two-term and no-hit controls. Accuracy rejects the faster
name-first shape; the source-first saving cannot close the endpoint gap.

A third read-only shim replaced the correlated entity probe with independent
per-term `UNION ALL` branches while preserving the OR predicate. The full SQL
matched all 40 uncapped two-term rows and the zero-row no-hit control, but on
the capped canonical request **24 of the top 25** identities/scores/matched
terms differed; the baseline score range was 4–12 and the shim's was 3–4.
This fails the conservative capped-result quality gate, so no performance
claim or implementation follows from that shape. All three shims were
diagnostics only; no ops-qa schema or production code changed. The remaining
<1 s acceptance must be checked on the approved same-topology canary, and a
further query change still requires its own accuracy-safe theory proof.

## Isolated in-cluster Neo4j/Postgres diagnostic

On 2026-09-27, the reviewed branch `fab269ef8` was transported solely for the
approved temporary API canary. The baseline was `c15539e80`. Both API
binaries were built from their exact commits with the unchanged digest-pinned
Dockerfile, Go 1.26.6 Alpine, `linux/amd64`, CGO enabled, static linker flags,
and each commit's source epoch. Their SHA-256 digests were
`070badb6d81bb7942586d76ba1669b2cdb67e04c3dbee261c861911bf7e454a0`
and `fe7df3d6c4000951014c4339b0f7bd2f62f6840659277273f3e3ea2a402b0c70`,
respectively. Both were static ELF x86-64 binaries without an interpreter or
shared-library dependency; the copied files and running `/proc/<pid>/exe`
matched those hashes inside the canary.

One isolated Pod held a baseline and candidate container, each with the live
API's 250m/512 MiB request and 1 CPU/2 GiB limit, the same pinned runtime
image and backend credentials, production query profile, non-root/read-only
security context, and distinct API ports. No Service or Ingress selected it.
A dedicated NetworkPolicy denied in-cluster ingress and restricted egress to
DNS, Postgres, Neo4j, and OTEL. The Postgres DSN reported both
`default_transaction_read_only=on` and `transaction_read_only=on`; bootstrap
and OIDC refresh were disabled, and both startup-backfill markers were
complete. Both APIs connected to the live Neo4j and Postgres Pods, returned
HTTP 200 from `/healthz`, and served every timed topic request with HTTP 200.

This was **diagnostic only on an incomplete migration state**. Baseline
`/readyz` reported migration 132 missing; candidate `/readyz` reported
migration 134 missing. Migration 132 was absent for both binaries. The live
ledger held the earlier 130/131 path-index receipts but not 133/134. The last
successful ops-qa schema-bootstrap Job used an image built before merged
PR #7276 introduced 132; its log reported `applied=0 skipped=151`.
Neither canary binary was deployment-ready, and no migration was applied.

After one warmup per binary and payload, three sequential `B,C,C,B` blocks
ran through a loopback-only direct Pod port-forward. No EXPLAIN, migration,
control request, or other benchmark overlapped either timed block.

| Exact HTTP payload | Baseline median | Candidate median | `<1 s` budget |
| --- | ---: | ---: | --- |
| `topic=config`, explicit 16 terms, limit 25, offset 0 | 5.468828 s | 2.771594 s | Missed |
| Phrase-body topic below, limit 25 | 5.766047 s | 2.897130 s | Missed |

Explicit baseline samples in seconds were `11.259798, 5.472353, 5.300286,
5.550829, 5.309183, 5.465302`; candidate samples were `3.648028,
2.604370, 3.272268, 2.827875, 2.401684, 2.715312`. Phrase baseline
samples were `6.225116, 4.558604, 5.701360, 5.870951, 5.119677,
5.830733`; candidate samples were `2.311798, 2.894293, 3.051744,
3.052945, 2.899966, 2.327879`. The phrase was `config content deployment
environment file function handler module package path repository resource
service source system workspace`. The explicit terms were the 16 listed in
the read-only query-shape proof below.

The corpus fingerprint before and after all requests was 144,838 files,
2,640,262 entities, and `max(content_files.indexed_at)=2026-09-27
02:38:27.074724+00`. The Neo4j Pod identity/restart count was unchanged;
both canary containers had zero restarts and zero observed CPU throttles.
After the timed blocks, normalized no-hit and uncapped two-term response
hashes matched across binaries. The latter returned 40 rows (20 files and
20 symbols), with neither pool nor page truncated. The capped canonical
25-row pages differed: baseline had 25 files/zero symbols, candidate 18
files/25 symbols. This does not establish capped-page parity or higher
relevance. Both APIs shut down normally within the 20-minute measurement
cap. The temporary Pod and dedicated NetworkPolicy were deleted and
confirmed absent; the live API Deployment remained 1/1 Ready.

A 2026-09-27 03:25 UTC read-only ops-qa diagnostic found neither
`pgstattuple`/`pageinspect` nor `pgstatginindex`/`gin_metapage_info` available.
The source GIN index's pending-list size is therefore **NOT_CHECKED**; no
extension or maintenance command was run. Earlier additive GiST and signature
screens did not establish a safe subsecond candidate. The remaining measured
endpoint gap is 1.771594 s, and the source-index time from a separate SQL
profile cannot be subtracted from the endpoint median. No further production
change follows from this evidence alone.

## Existing ops-qa index admission proof

The earlier approved ops-qa index rollout left two full migration receipts:
`130_content_files_relative_path_trgm_index.sql` with checksum
`ef395de0a2c1ad86fcbc1f82abcda08c83e689508a1197d6e2848695978a8e41`
and `131_content_files_relative_path_trgm_index_lifecycle.sql` with checksum
`c0494bb1489ca3900f62675aacf0b37cd5524e868ba96a124640f6142b14ef2b`.
The new 133 index SQL has the same bytes and checksum as that earlier 130
index SQL, but a different tracked path; the new 134 lifecycle has different
SQL bytes from the earlier 131 lifecycle. Both old receipts must remain in
the ledger.

A read-only admission shim on ops-qa compared both exact path, variant, and
checksum triples; checked that no 133 receipt exists; checked the public
index's table, single `relative_path` key, GIN access method, `gin_trgm_ops`,
nonpartial/nonexpression shape, and valid/ready/live flags; and checked
`content_substring_index_state=ready` with the current validator returning
true. All five booleans were true, with exit 0. This proves that ops-qa meets
the proposed narrow legacy-adoption predicate. It does **not** apply 133/134,
prove a future no-op, or authorize treating a different same-name index as
equivalent. [PostgreSQL 18's `CREATE INDEX` documentation](https://www.postgresql.org/docs/18/sql-createindex.html)
explicitly does not guarantee definition equality for `IF NOT EXISTS`.

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

- populated pre-133 ready state plus indexed content, tracked 133 concurrent
  migration, tracked 134 lifecycle migration, then a guarded unscoped
  `relative_path ILIKE` read;
- wrong btree and partial GIN same-name path indexes while the other three
  lifecycle indexes are exact; 134 moves state to `not_built`, finalization
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

After the rebase and exact legacy-index admission change, the **complete**
`./internal/storage/postgres` package passed in the isolated Go 1.26.6
container with the final worktree and module cache bind-mounted read-only:
`go test ./internal/storage/postgres -count=1` took 2.603 s, exit 0. The
new opt-in `issue7033_rollout` disposable-Postgres matrix passed in 16.540 s,
exit 0, with the final split test-file layout. Its adoption case preserves
both exact legacy 130/131 receipts and the existing index OID/relfilenode
while tracked 133/134 complete and readiness stays `ready`. Negative cases
reject missing or wrong legacy receipts, a malformed same-name index, and a
134 receipt without 133 before target DDL; a 133-only retry completes 134.
The behavioral regression was first red with the exact legacy receipts and
index, failing at the untracked-index preflight, then green after the narrow
admission change. The host's earlier disk-quota linker failure is not treated
as a passing package test.

The opt-in `issue7033_rollout` test runner selects only the embedded, checksum-
matched migrations 133 and 134. It requires the `public` schema and verifies
the target system identifier, database, current schema, primary role,
prerequisite receipts and three exact existing GIN indexes in a read-only
preflight. It applies tracked migration 133,
checks the exact new index, then applies tracked migration 134 and checks the
four-index readiness contract. On a disposable PostgreSQL 18.6 database, the
runner applied those two migrations and retried idempotently. Wrong target,
incomplete index state, a mismatched prerequisite ledger receipt, and a
non-`public` search path all failed before target DDL. The cross-schema
regression was red first: with `search_path=custom,public`, the unguarded
runner created its index in `custom` before failing its `public` postcheck.
Separate live regressions prove both that migration 125 can remain unapplied
during this scoped rollout and that all unrelated migrations 125–129 can remain
unapplied while 133–134 run. Normal bootstrap subsequently applies 125–129
without replaying 133 or 134; the rescoped service index and content-index
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
