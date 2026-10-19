# #8168: canonical Module keyed on a constrained import_key

Issue #8168 reported that concurrent canonical writers create duplicate
`Module` nodes on Neo4j. This note records the cause, the change, and the
local proof. The design follows the binding arbiter ruling for #8168.

## Cause

The canonical writer ran `MERGE (m:Module {name: row.name, lang: row.language})`
with no uniqueness constraint behind the key. Neo4j documents that MERGE alone
guarantees the pattern exists, not that it is unique. Two repository
transactions that import the same module each create a node the other cannot
see yet. A `(name, lang)` constraint cannot be added: semantic Module entities
share the label, carry both properties, and may repeat them (live probe in the
diagnosis: `22N79 ... Both Node(702) and Node(703) have the label Module and
properties name, lang`).

## Change

| Surface | Before | After |
| --- | --- | --- |
| Module upsert | `MERGE (m:Module {name, lang}) SET m.evidence_source = ...` | `MERGE (m:Module {import_key: row.import_key}) ON CREATE SET m.name, m.lang, m.evidence_source` |
| Key | none | `import_key = name + "@" + lang`, computed in Go (`canonical.ModuleImportKey`), equal to the published IMPORTS `target_id` |
| Schema | `module_name_lookup` index only | adds `CREATE CONSTRAINT module_import_key ... REQUIRE m.import_key IS UNIQUE` (both backends) |
| Row order | Module rows in fact order; IMPORTS rows in fact order | Module rows sorted by `import_key`; IMPORTS rows sorted by `(module_import_key, file_path, line_number)` |
| IMPORTS target | `MATCH (m:Module {name, lang})` | `MATCH (m:Module {import_key: row.module_import_key})` |
| Write fence | `Module=name,lang` | `Module=import_key`, no compatible predecessor |
| Oversized-key guard | measured `name` | measures `import_key` (the widest Module index key) |
| Populated Neo4j graph | n/a | bootstrap-data-plane migration before the marker |

Semantic Module nodes never carry `import_key`, so the constraint never sees
them and the canonical MERGE no longer adopts them (ruling item 2). Modules
stay in the main atomic repository transaction (ruling item 4).

Two additions beyond the ruling text, both measured:

1. IMPORTS rows are sorted too. With sorted Module rows only, 8 writers still
   hit `DeadlockDetected` on `EXCLUSIVE NODE_RELATIONSHIP_GROUP_DELETE` of the
   shared Module nodes in 87 of 800 transactions. Sorting the edge rows by the
   same key removed them (theory proof below).
2. The migration keys and collapses each `(name, lang)` group in one step and
   runs after the DDL. Keying in a separate pass after the constraint would
   fail on any duplicate a racing old writer created; the per-group step folds
   it in. It also runs in the adoption path, because adoption writes the
   marker without the DDL pass. `TestAdoptionCannotMarkBeforeTheModuleMigration`
   proves adoption leaves the marker unset when the migration fails or leaves
   an unkeyed node; with the migration removed from the adoption gate it goes
   RED.

## Theory proof (before the production change)

Decision: build the ruled design only if it shows zero duplicates and zero
`DeadlockDetected` at 2 and 8 workers. Deadline 01:20 UTC; done 00:52 UTC.
Harness: throwaway test, explicit `BeginTransaction`/`Commit`, no driver retry,
barrier start, 4 shared keys per iteration, odd workers in reversed order,
Module upsert and IMPORTS edges in one transaction. Backend
`neo4j@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f`
(Neo4j 2026.08.1 community, the `docker-compose.neo4j.yml` pin), container
`x8168-neo4j`, `NEO4J_AUTH=none`.

| Shape | Workers | Iterations | Duplicate iterations | Tx errors | DeadlockDetected |
| --- | ---: | ---: | ---: | ---: | ---: |
| main (name, lang), no constraint, unsorted | 2 | 100 | 99 | 0 | 0 |
| main (name, lang), no constraint, unsorted | 8 | 100 | 100 | 46 | 46 |
| import_key + constraint, Module rows sorted, edges unsorted | 2 | 100 | 0 | 0 | 0 |
| import_key + constraint, Module rows sorted, edges unsorted | 8 | 100 | 0 | 87 | 87 |
| import_key + constraint, Module and edge rows sorted | 2 | 300 | 0 | 0 | 0 |
| import_key + constraint, Module and edge rows sorted | 8 | 300 | 0 | 0 | 0 |

## Regression test (RED then GREEN)

`TestLiveCanonicalModuleConcurrentWritersKeepOneNodePerKey`
(`go/internal/storage/cypher/module_concurrency_live_test.go`) runs the
production `buildModuleStatements` and `buildStructuralEdgeStatements` with the
same harness shape, 100 iterations each at 2 and 8 workers, after applying the
production Neo4j schema. It skips unless the backend reports the Neo4j kernel.

```text
ESHU_CYPHER_BOLT_DSN=bolt://127.0.0.1:17687 ESHU_CYPHER_BOLT_DATABASE=neo4j \
  go test -tags live_nornicdb_answer_truth ./internal/storage/cypher/ \
  -run TestLiveCanonicalModuleConcurrentWritersKeepOneNodePerKey -count=1 -v
```

Both new live tests carry the `live_nornicdb_answer_truth` build tag and are
`ci` rows (Neo4j leg) in `specs/live-tests.d/`, so the live-backend CI job
runs them on a fresh Neo4j per file; they skip on the NornicDB leg. The RED
row below was measured before the tag was added, with the same test body.

| Run | Workers | Duplicate iterations | Extra nodes | IMPORTS mismatches | Tx errors (deadlocks) | Exit |
| --- | ---: | ---: | ---: | ---: | --- | ---: |
| RED, origin/main code | 2 | 99 / 100 | 396 | 7 | 0 (0) | 1 |
| RED, origin/main code | 8 | 100 / 100 | 2,601 | 41 | 36 (36) | 1 |
| GREEN, this change | 2 | 0 / 100 | 0 | 0 | 0 (0) | 0 |
| GREEN, this change | 8 | 0 / 100 | 0 | 0 | 0 (0) | 0 |

Hermetic guards (run in CI without a backend): `module_identity_test.go` pins
the MERGE text (import_key key, ON CREATE only), the IMPORTS MATCH, the key
format, and both sorts; `schema_module_import_key_test.go` pins the constraint
on both backends, the `Module=import_key` contract line, and the empty
compatible list.

## Migration proof

`TestLiveModuleImportKeyMigrationCollapsesKeysAndRepoints`
(`go/cmd/bootstrap-data-plane/module_import_key_live_test.go`, opt-in with
`ESHU_MODULE_IMPORT_KEY_MIGRATION_LIVE=1` because the migration rewrites every
canonical Module in the database) drops the constraint, seeds a pre-#8168
graph, applies the DDL, and runs the migration twice:

| Seed | After the first run |
| --- | --- |
| 3 canonical duplicates of one key, IMPORTS from 3 files, one file importing 2 of them | 1 keyed node, 3 IMPORTS, per-file properties kept |
| lang-less node and empty-lang node of one name | 1 node keyed `name@`, 2 IMPORTS |
| uid-bearing node the old MERGE adopted, with IMPORTS and CONTAINS | IMPORTS moved to a new keyed canonical node with its properties; semantic node keeps CONTAINS, uid, no import_key |
| semantic node sharing a duplicate's (name, lang), with CONTAINS | unchanged (properties, CONTAINS, no import_key) |
| keyed node beside an unkeyed duplicate (racing writer) | folded into the keyed node, IMPORTS moved |

Constraint present after the run. The second run wrote zero counters (nodes,
relationships, properties, labels) and logged `keyed_groups=0`. A seeded
mutation (dropping `ON CREATE SET k = properties(r)`) turned the test RED on
the f2, f3 and f9 property checks.

First application on this container also collapsed leftovers of earlier test
runs: `keyed_groups=803 moved_imports=3795 deleted_duplicates=2928
duration_seconds=0.93` (the field was named `moved_imports` then; it is now
`imports_on_duplicates`, see below); the rerun logged zeros in 0.013 s.

Lock behavior: each statement is plain Cypher in `CALL { } IN TRANSACTIONS`
(500 groups or 1,000 edges per transaction), so locks last one batch. Writers
are NOT fenced while it runs; see "Writer precondition" below. A statement
that times out keeps its committed batches and the next bootstrap resumes. Rollback: `DROP CONSTRAINT module_import_key IF EXISTS` and
the previous release; `import_key` stays on the nodes and is harmless. The
collapse deletes duplicate canonical nodes after moving their edges; the
ruling is its arbiter review, scoped to canonical duplicates.

## Writer precondition and the marker-missing path (review round 1)

The first review (F1) found that this note and the bootstrap-data-plane
README claimed a writer fence that does not exist. The Helm schema Job is a
pre-upgrade hook, so the previous release's pods keep writing (name, lang)
Module nodes until an operator scales them down
([Deployment Contract](../../public/deployment/service-runtimes-bootstrap.md#deployment-contract)).
The fingerprint refuses an old writer only after the marker is written: at
its startup, or at the write path's next recheck
(`graphschemacompat.DefaultWriteFenceInterval`, 30 s). The reviewer ran 4
old-style writers during 3 migration runs; every run failed its verify (197,
15, 13 canonical nodes without `import_key`; 50, 13, 18 IMPORTS edges still on
semantic nodes). The failure is safe: the marker stayed unset and nothing was
lost. After the writers stopped, one run converged.

Fixes:

- The precondition is now stated in the README, the package AGENTS.md, and the
  incomplete error. The error carries both counts, tells the operator to stop
  or scale to zero every ingester, projector, resolution engine, and
  `eshu-bootstrap-index` from the previous release, then rerun bootstrap, and
  names the Deployment Contract; the ERROR log adds a `runbook` field.
  `TestModuleImportKeyIncompleteErrorNamesTheWriterPrecondition` pins it.
  Live re-run after the fix (container `x8168f-neo4j`, same pinned image, 4
  old-style writers minting new (name, lang) Modules, 3 migration runs, then
  one after the writers stopped): runs 1 to 3 refused with 267, 21 and 9
  unkeyed canonical nodes (0 IMPORTS on semantic nodes), each error naming the
  writers to stop and the Deployment Contract, each ERROR log carrying
  `runbook`; the run after the writers stopped returned nil. Throwaway harness,
  not committed.
- F2: `eshu-bootstrap-index` applies the DDL and writes the marker itself when
  the marker is missing. It never ran the migration, so on a populated graph
  that lost its marker it readmitted import_key writers beside unkeyed nodes.
  On Neo4j it now runs the migration's verify statement
  (`CanonicalModuleImportKeyPendingCypher`) after the DDL and before the
  marker, and refuses the marker, naming `eshu-bootstrap-data-plane`, while
  either count is above zero. It does not run the destructive migration
  itself: a one-shot seeder should not, and the ruling placed the migration in
  bootstrap-data-plane. `graph_schema_module_import_key_test.go` pins the
  order (DDL, check, marker), the refusal, fail-closed on an error, a
  malformed answer, or a missing reader, and the NornicDB skip. Both binaries
  parse the answer with `ParseCanonicalModuleImportKeyPending`, which fails on
  anything but one row of two integers.
- F3: ruling 6(b)'s zero-row invariant is now the required `graph/module_identity`
  check in the golden-corpus gate's graph phase on the Neo4j leg, beside
  `graph/anchor_census`. It runs the issue's duplicate query
  (`CanonicalModuleDuplicateKeysCypher`, zero rows) and counts canonical Module
  nodes without `import_key` (`CanonicalModuleUnkeyedCountCypher`, zero). The
  snapshot schema has no raw-query assertion type, so the check is code in
  the gate, the same shape as the anchor census; no new gate or registry row.
  Off Neo4j it records a non-required skip.
- F6: the collapse now sets the keeper's `lang` to the group's language, so a
  legacy node with no `lang` ends with `lang = ''`, the value the writer gives
  a new node for that key and the value the orphan sweep keys on.
- F7: the log field `moved_imports` counted the IMPORTS edges matched on the
  duplicates, not the edges created (the reviewer's hot group logged 95,000;
  5,000 survived). It is now `imports_on_duplicates`, documented as edges
  found on the deleted duplicates, each moved or folded into the keeper's own
  edge from that file.

## NornicDB

- DDL: single-property, passes `nornicDBSchemaConstraint` unchanged; pinned by
  `TestModuleImportKeyConstraintLandsOnBothBackends`. The NornicDB fingerprint
  moved to `443f528a…`.
- Live conformance on the pinned image
  (`nornicdb-amd64-cpu@sha256:74a8ed7b…`): `TestLiveBackendConformance` PASS;
  the canonical-import read now returns the uid-NULL row with no override, so
  the #6968 override on that case was removed. Neo4j conformance PASS.
- Hot-path trace: `testdata/nornicdb/eshu_exact_module_identity_trace_test.go`
  run inside a scratch copy of `github.com/orneryd/nornicdb@v1.0.45` (the
  module version in `go.mod`, not the source revision of the pinned fork image;
  that exact-revision run is NOT_CHECKED):

```text
canonicalNodeModuleUpsertCypher trace: {... UnwindSimpleMergeBatch:true UnwindMergeChainBatch:true ... MergeSchemaLookupUsed:true MergeScanFallbackUsed:false}
canonicalNodeImportEdgeCypher trace: {... UnwindMergeChainBatch:true ... MergeSchemaLookupUsed:true MergeScanFallbackUsed:false}
--- PASS: TestEshuExactModuleIdentityQueriesUseIndexedMergeHotPath
```

  The harness also asserts the 25 seeded same-(name, lang) semantic nodes keep
  no `import_key` and no `evidence_source`.
- Populated-store migration on NornicDB: not provided (not supported for
  deployment, per the ruling).

## Performance Evidence

Performance Evidence: baseline origin/main `15fcdcdcb` against this branch, on
the pinned Neo4j (`neo4j@sha256:eabfbb04…`, 2026.08.1) and NornicDB
(`orneryd/nornicdb@v1.0.45` in-process) backends. These are local proxies,
not comparable to the repo-scale contract.

NornicDB in-process MERGE, `GOMAXPROCS=1 -benchtime=300x -count=5`, median
ns/op, legacy `(name, lang)` statement vs the production `import_key`
statement, same store and schema:

| Same-name candidates | legacy (name, lang) | import_key |
| ---: | ---: | ---: |
| 1 | 44,094 | 38,674 |
| 25 | 45,347 | 39,198 |
| 200 | 41,618 | 42,354 |

Neo4j writer-level contention proxy (throwaway harness, production builders of
each tree, 8 workers, 320 repository transactions, each with 20 shared and 20
unique modules and 2 files importing all 40, managed `ExecuteWrite` retry like
production, same container, alternating base and branch):

| Run | origin/main seconds | retries | this change seconds | retries |
| ---: | ---: | ---: | ---: | ---: |
| 1 | 16.782 (16.8 s) | 33 | 5.485 (5.5 s) | 0 |
| 2 | 14.098 (14.1 s) | 30 | 3.313 (3.3 s) | 0 |
| 3 | 9.623 (9.6 s) | 21 | 3.379 (3.4 s) | 0 |

origin/main also wrote 6,560 Module nodes (want 6,420) and 112,680 IMPORTS
edges (want 25,600): its `(name, lang)` IMPORTS MATCH binds every duplicate,
so each edge fans out. This change wrote exactly 6,420 and 25,600.

Repo-scale measurement still required (ruling 6(c)), NOT_CHECKED here: same
corpus, profile, topology, clean volumes, pinned Neo4j, origin/main vs this
branch, 3 runs each. Primary boundary: the bootstrap-index log
`bootstrap projection complete` field `total_duration_seconds` (measured from
the projector start, `bootstrap_projector.go`), cross-checked with
`bootstrap phase complete` where `bootstrap_phase=projection`
(`phase_duration_seconds`) and `bootstrap pipeline complete`
(`total_seconds`). Also record deadlock and retry counts. A regression over
10% blocks.

Observability Evidence: no new metric. The existing Neo4j deadlock and retry
signals cover the writer path. The migration adds two log events,
`bootstrap.graph.module_import_key.migrated` and
`bootstrap.graph.module_import_key.incomplete` (ERROR), with
`keyed_groups`, `imports_on_duplicates`, `deleted_duplicates`,
`repointed_imports`, `unkeyed_remaining`, `adopted_imports_remaining`, and
`duration_seconds`; the incomplete event adds `runbook`.

## NOT_CHECKED

These gate the merge, not the push (review F5). Each needs CI or a remote
run:

- Repo-scale throughput (ruling 6(c)), as specified above: 3 runs each of
  origin/main and this branch, 8 workers, clean volumes, pinned Neo4j,
  `bootstrap projection complete`.`total_duration_seconds`; a regression over
  10% blocks. Also the cold-start `module_import_key` index-entry lock wait:
  over 10% of projection wall time opens a follow-up (ruling item 4). Record
  deadlock and retry counts.
- Ruling 6(d): `golden-corpus-differential` (job "differential nornicdb vs
  neo4j", IMPORTS parity) and `golden-corpus-gate-neo4j` green on the pushed
  head. The latter is also the first run of the new `graph/module_identity`
  check on the replayed corpus; locally it ran only against fakes.
- An upgrade run: index a corpus on origin/main, then run this branch's
  `eshu-bootstrap-data-plane` with the old writers stopped. Record the
  migration log counts and `duration_seconds`, and IMPORTS edge parity before
  and after. Per-group transaction size is bounded by group count, not edge
  fan-out (one 100,000-edge group took 2.39 s and 1.5 GiB RSS in review).
- The trace harness on the exact NornicDB fork revision of the pinned image.
- Helm schema-job ordering beyond the pre-install/pre-upgrade hook.
