# #7603 cross-repo dead-code `test_only_consumers`

## Scope

Owner decision (#7603): a caller in a test file counts as a caller, and the
answer says so in a separate field. A test that calls a function is a real
dependency, so liveness does not change. The new fact is "only tests call this",
so an admin can see that removing the function also means removing or rewriting
its tests.

Arbiter ruling: ship `test_only_consumers` (boolean, present only when true) on
the cross-repo rows of `find_cross_repo_dead_code` /
`POST /api/v0/code/dead-code/cross-repo`, in one PR, with one batched
primary-key read per request. The same-repository routes are deferred: they
project only `(entity, method)`, and `ApplyDeadCodeIncomingEdges` drops a
strong-caller candidate before a result row exists, so under "liveness
unchanged" there is never a row for the field to sit on.

Language scope (verified from source, correcting the issue body and an earlier
reading of the ruling that said a test-only consumer already yields `dead`).
Consumer snapshots are rooted at `content_entities` rows whose
`metadata->'dead_code_root_kinds'` is a non-empty array
(`listCodeReachabilityRootsSQL`); `cleanCodeReachabilityRoots` filters nothing by
kind, and `scanCrossRepoDeadCodeEvidence` reads `root_kinds` but never uses it,
and neither evidence statement filters on it. Test methods carry a root kind in
`csharpFunctionRootKinds` (`csharp.test_method`), `javaFrameworkMethodRootKinds`
(`java.junit_test_method`), the Kotlin and Scala root builders
(`kotlin.junit_test_method`, `scala.junit_test_method`,
`scala.scalatest_suite_class`), `rustDeadCodeRootKinds` (`rust.test_function`)
and the Swift helpers (`swift.xctest_method`, `swift.swift_testing_method`). A
test in one of those languages that calls a producer symbol therefore yields a
consumer row rooted at the test method: the symbol is `live_by_consumer` and,
when every consumer root is a test file, carries `test_only_consumers: true`.
That is where the flag really fires. Java is the proof case: the QA replica's
Java repository has many test-method roots under `test/` whose reachability rows
reach other entities (hundreds of rows per test file). Go, Python,
JavaScript and TypeScript define no test root kind (the `go.`, `python.` and
`javascript.`/`typescript.` kinds are framework, export and entry-point kinds
only): a test there is not a root, starts no reachability walk, and is never a consumer, so
the flag does not fire from it and a symbol only such tests call reads `dead`
(or unknown when consumer coverage is incomplete). It can appear in those
languages only if another root, such as a script entry point, sits in a test
path.

## Behavior

- A row in `live_by_consumer` gets `test_only_consumers: true` when it has one or
  more consumers and every consumer's root entity file is a test file by
  `codemodel.DeadCodeIsTestFile` (called, not copied) on the root's
  `content_entities.relative_path`.
- The key is absent on: dead rows, mixed roots (any non-test consumer), a row
  with a consumer hidden from the caller, every `unknown_needs_evidence` row,
  boundary-fallback rows, a consumer with no root entity id, and a root with no
  `content_entities` row. It is never `false`.
- Two further absences extend the arbiter's list on its own reasoning about
  hidden consumers: a request whose consumer coverage is incomplete, and a
  request that named `consumer_repo_ids`. Strong evidence outranks both for
  liveness (one consumer is enough), but "only tests call this" needs every
  consumer, and an incomplete snapshot or a selector-bound read can hide a
  production caller (the hidden-consumer probe does not run for a selector).
  The handler skips the root path read in both cases (nothing could set the
  flag), `TestCrossRepoDeadCodeTestOnlyConsumersOneBatchedRead` pins it. Absence
  is the safe default
  (`TestCrossRepoDeadCodeTestOnlyConsumersNeedsEveryConsumerVisible`, seen RED
  with `test_only_consumers = true` on both cases before the condition was added).
- Every existing field, bucket, count and the `analysis` block are identical with
  and without the flag
  (`TestCrossRepoDeadCodeTestOnlyConsumersLeavesLivenessUntouched`).
- `crossRepoDeadCodeConsumerRootPaths` collects the distinct consumer root ids
  and consumer repository ids from the evidence page, skipping needs-evidence
  items (the truncation marker among them), and reads them once through
  `ContentReader.CrossRepoDeadCodeConsumerRootPaths`. It reads nothing when no
  candidate has a consumer root, and a store without the method leaves the flag
  off. A read error fails the request through `WriteGraphReadError`, never "not
  test only".
- Statement (`CrossRepoDeadCodeConsumerRootPathsQuery`):
  `SELECT entity_id, relative_path FROM content_entities WHERE entity_id = ANY($1) AND repo_id = ANY($2)`.
  `code_reachability_rows` has no file column and `GetEntityContents` reads
  `source_cache`, so neither is used. At most 1,000 ids (the evidence page's own
  cap on distinct roots).

Example, flagged row (`live_by_consumer`, `evidence_detail` `handles`; trimmed,
synthetic ids):

```json
{"entity_id": "content-entity:e_7a3c10000000", "classification": "live_by_consumer",
 "confidence_label": "high", "consumer_evidence_count": 1,
 "consumer_evidence": [{"consumer_repo_id": "repository:r_consumer", "relationship_type": "CALLS",
   "evidence_family": "direct_code", "confidence_label": "high", "item_count": 1}],
 "test_only_consumers": true}
```

Unflagged row (a non-test consumer): the same object without the
`test_only_consumers` key.

## Proof

RED, seen before the change (`go test ./internal/query/codequery/deadcode/ -run TestCrossRepoDeadCodeTestOnlyConsumers`):
every flagged case failed with `test_only_consumers = <nil>, want true`, and the
read-count and failure tests failed with `root path reads = 0, want exactly 1`
and `status = 200, want a failed request when the root path read fails`. GREEN
after the change, same command. Cases: all consumer roots test (two consumers),
mixed, a `pkg/contest/` root (not a test), a `tests/fixtures` root and a Java
`src/integrationTest` root (tests by the single rule), a missing root row, a
consumer with no root id, a hidden consumer, a needs-evidence consumer, no
consumer, boundary fallback, full and handles detail, one batched read with
distinct ids, no read without a consumer root, and a failed read.

- `TestCrossRepoDeadCodeConsumerRootPathsOneKeyedRead`,
  `TestCrossRepoDeadCodeConsumerRootPathsSkipsEmptyInput` and
  `TestCrossRepoDeadCodeConsumerRootPathsCapsTheBatch` pin the statement text,
  the two array parameters, the 1,000-id cap and the empty-input short circuit.
- `TestCrossRepoDeadCodeConsumerRootPathsLive` (disposable PostgreSQL 18, full
  bootstrap schema, 30,000 filler rows) proves the row set (a missing root and a
  root in another repository are absent) and that the plan is a keyed
  `entity_id` lookup with no sequential or bitmap scan. Seeded violation: with the
  predicate changed to `lower(entity_id) = ANY($1)` the test fails on a Bitmap
  Heap Scan over `content_entities_repo_idx`; the shipped statement passes.
- `TestFindCrossRepoDeadCodeTestOnlyConsumersCalibratedBaseBar` (MCP, the #7168
  calibrated base, every evidence row live and flagged, default arguments):
  unflagged 175,044 bytes, flagged 176,388 bytes (67.3% of the 262,144-byte
  budget, both wire copies), the flag adds 1,344 bytes for 24 rows (56 per row,
  ceiling 80), structuredContent delivered. The existing
  `TestFindCrossRepoDeadCodeHandlesCalibratedBaseBars` is unchanged.

## Hand-check on real rows

ops-qa replica, 2026-10-05, read-only. No cross-repo reachability row exists on
the replica today (a join of `code_reachability_rows` with `content_entities` for
`entity.repo_id <> row.repository_id` returned 0 rows), so the row-level check
treats the producer entity's same-repository `code_reachability_rows` roots
(repository `repository:r_a09c7db8`, 106,003 entities, 2,462 distinct roots) as
its consumers: the same statement shape, the same paths, the same rule. Of 341
entities sampled, the rule flags 49 and leaves 292 unflagged. Twelve checked by
hand against the root paths. Real class and file names from the QA codebase are
replaced with synthetic ones that keep only what the rule reads (directory
segments and the file suffix); the outcomes are the real ones:

| Row | Root paths | Flag | Why |
| --- | --- | --- | --- |
| 1 | `test/.../integration/FooApiTest.java` | yes | `test/` prefix |
| 2 | `test/.../commands/BarGetTest.java` | yes | `test/` prefix |
| 3 | `test/.../services/BazServiceTest.java` | yes | `test/` prefix |
| 4 | `test/.../services/QuxMediaServiceTest.java` | yes | `test/` prefix |
| 5 | `test/.../account/SessionCreateTest.java` | yes | `test/` prefix |
| 6 | `test/.../util/pdf/SignatureTest.java` | yes | `test/` prefix |
| 7 | `src/main/.../Baz.java`; `src/test/java/.../SyncUtilTest.java` | no | mixed: one production root |
| 8 | `src/main/.../Baz.java`; `test/.../ImportSyncTest.java` | no | mixed |
| 9 | `src/main/.../Baz.java`; `test/.../BazProxyTest.java` | no | mixed |
| 10 | `src/generated/.../FormModel.java`; `src/main/.../Router.java` | no | no test root |
| 11 | `src/generated/.../Lookup.java` | no | `generated` is not a test path |
| 12 | `src/main/.../Activity.java` | no | production root |

All twelve agree with the manual reading. Of the 1,000 distinct root ids on the
page used for the timings, 983 had a `content_entities` row; the other 17 roots
have none (a re-indexed entity a reachability row still names), so a row whose
root is one of them gets no flag.

## Performance

Performance Evidence: QA replica, read-only, PostgreSQL 18.3, 2026-10-05,
`statement_timeout` 10 s, replay lag 0.2 to 2.1 s before and 0.2 to 2.1 s after
each run, `EXPLAIN` before `EXPLAIN (ANALYZE, BUFFERS)`, a distinct prepared
statement per timed run, the exact shipped statement. Measured, not fixture.
`content_entities` is about 3.36 million rows. Parameters: a page of 1,000
distinct consumer root ids from `code_reachability_rows` of the largest
repository (`repository:r_a09c7db8`, 106,003 entities) and that repository id,
the worst case for the repository bound. Plan class with bound values: Index
Scan using `content_entities_repo_entity_idx` on `(repo_id, entity_id)`, both
columns as index conditions, 379 index searches, 983 rows returned. Three runs:
execution 15.682, 12.496 and 12.724 ms; planning 2.0 to 2.2 ms; shared hits
2,112 to 2,548, reads 436 on the cold first run, zero after. Forced generic plan
(`plan_cache_mode = force_generic_plan`, the worst plan the driver could choose):
Index Scan using `content_entities_pkey` with `repo_id` as a filter, 923 index
searches, three runs 19.881, 19.451 and 19.311 ms of execution (19.968, 19.536
and 19.396 ms total with 1.7 ms planning), shared hits 4,677, no reads. Four
repositories (`r_8ed4bb37`, `r_33474efb`, `r_e3c0ae60`, `r_dbeaff35`), 1,000
roots, 873 rows, one run: primary-key Index Scan, 27.892 ms execution, shared
hits 4,161 and 598 reads on a cold read. A typical request names far fewer
roots (a default page is 25 candidates), and the statement is skipped when no
candidate has a consumer root. No per-candidate query is added. The statement
adds no index, table or write. Both observed plans are keyed lookups; neither
scans the repository. The live proof on a 30,000-row disposable table, 6,000 of them in the consumer
repository, plans the same way as the replica with bound values (Index Scan using
`content_entities_repo_entity_idx`, both columns as index conditions); the test
accepts the primary key or that index and rejects a sequential or bitmap scan.

Cost to the reply: 56 bytes per flagged row in the est2x metric (both wire
copies), 1,344 bytes for 24 flagged rows on the calibrated base, against a
45,875-byte evidence ceiling and a 262,144-byte budget.

Observability Evidence: the read runs under its own `postgres.query` span with
`db.operation=cross_repo_dead_code_consumer_root_paths`, `db.sql.table=content_entities`,
`db.request.consumer_root_ids` (ids sent) and `db.rows.consumer_root_paths` (paths
found). At 3 AM the gap between the two says how many consumer roots had no
content row, a failed read is a span error plus a 5xx or the retryable 503 from
`WriteGraphReadError`, and a request with no consumer root has no such span. The
flag itself is visible in the response (`test_only_consumers`).

## Known gaps

- Same-repository routes (`/api/v0/code/dead-code`, `/investigation`) do not
  carry the flag (deferred, see Scope).
- A consumer's root file is the evidence: a test caller that is not a
  reachability root for its language produces no consumer row, so it neither
  keeps a row live nor sets the flag.
- The flag is not exercised end to end on ops-qa: the replica has no cross-repo
  reachability rows today, so the real-data checks above use same-repository
  roots through the identical statement and rule.
