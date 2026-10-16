# #7324 Path-Conflict Repository Retirement: Guard, Telemetry, Live Proof

Design and ruling: `docs/internal/design/7324-cross-scope-writer-rearm.md`.
Base: origin/main 182a31124. Graph backend for every live number here:
`neo4j:2026-community` (Neo4j 2026.09.0), a dedicated local container with
`NEO4J_AUTH=none`, the production schema applied by the harness
(`repository_id` and `repository_path` unique constraints present).

## What was already true

Same-id Repository edge loss does not reproduce on current main. #7304
removed the by-id `DETACH DELETE`, and `TestLiveRepositoryRetryKeepsReducerEdges`
(`go/internal/reducer/repository_retry_live_test.go`) passes on main. This
change does not fix an edge loss. It enforces contract A statically, makes the
one remaining Repository delete observable, and extends the survival matrix.

## Static guard

`TestRepositoryIncomingEdgesAreDeletedOnlyByTheirOwners`
(`go/internal/storage/cypher/canonical_repository_incoming_edge_guard_test.go`)
scans string constant expressions under go/internal and go/cmd, folding
same-package constant concatenations. The fold was needed: the two typed
incoming-family retracts, `RetractRepoRelationshipEdgesCypher` and
`RetractSingleRepoRelationshipEdgesCypher`, are built from
`RepoDependencyRelationshipEdgeTypes` by concatenation, and a
literal-at-a-time scan saw only fragments of them.

Reach on the clean tree: 227 Repository Cypher strings, 22 with DELETE, 1
Repository node delete (the path-conflict constant), and 6 incoming-Repository
relationship deletes. All six are scoped by `rel.evidence_source =`
(repo_dependency DEPENDS_ON, CORRELATES_DEPLOYABLE_UNIT, PINS_SUBMODULE,
BUILT_FROM, and the two folded typed retracts).

Seeded-violation RED: two constants were planted temporarily in
`canonical_submodule_edges.go` and removed afterwards. One was a by-id
`MATCH (r:Repository {id: $repo_id}) DETACH DELETE r`. The other was an
unscoped `DEPLOYMENT_SOURCE` delete into a Repository, built by concatenation:

```text
--- FAIL: TestRepositoryIncomingEdgesAreDeletedOnlyByTheirOwners (1.15s)
    scanned 229 Repository Cypher strings: 24 with DELETE, 2 Repository node deletes, 7 incoming-Repository relationship deletes
        internal/storage/cypher/canonical_submodule_edges.go: rule 1: Repository node r deleted outside canonicalNodeRepositoryPathCleanupCypher
        internal/storage/cypher/canonical_submodule_edges.go: rule 2: relationship rel into a Repository deleted without rel.evidence_source =
seeded rc=1
ok  github.com/eshu-hq/eshu/go/internal/storage/cypher  0.966s
clean rc=0
```

`TestRepositoryDeleteViolationsDetectsSeededDeletes` feeds 19 seeded strings
through the same scanner. The existing per-write check
`repositoryIDDetachDelete` in `canonical_node_writer_repository_test.go` is
kept, not superseded: it inspects the statements `Write` actually emits, which
a literal scan cannot see.

## Retirement telemetry: live RED then GREEN

`TestLiveRepositoryPathConflictRetirementReportsDroppedIncomingEdges`
(`go/internal/reducer/repository_path_conflict_live_test.go`) runs these steps:

1. Project an old-id Repository at path P (first generation).
2. Seed one incoming edge from each of the 11 ruled families.
3. Project a new id at P with `FirstGeneration=false, DeltaProjection=false`.
4. Assert the retirement is reported, under both production executor shapes.

The test asserts `relationships_deleted` equals the retired node's full degree,
with the degree pinned as 11 incoming plus 9 projector edges, so an undercount
cannot pass.

RED on unmodified production code (only test files changed):

```text
=== RUN   TestLiveRepositoryPathConflictRetirementReportsDroppedIncomingEdges/atomic_group
    old-id Repository before retirement: incoming=11 total degree=20 (outgoing=9)
    `canonical repository retired` log lines = 0, want 1 (the retirement is silent)
=== RUN   TestLiveRepositoryPathConflictRetirementReportsDroppedIncomingEdges/phase_group
    old-id Repository before retirement: incoming=11 total degree=20 (outgoing=9)
    `canonical repository retired` log lines = 0, want 1 (the retirement is silent)
--- FAIL: TestLiveRepositoryPathConflictRetirementReportsDroppedIncomingEdges (0.92s)
```

In the RED run the graph assertions before the log check passed. The old id
was retired, the new id held P, and the 11 seeded incoming edges were gone
from the graph. So the loss was real, and silent.

GREEN after the change:

```text
    old-id Repository before retirement: incoming=11 projector outgoing=9 total degree=20
    retirement log: map[deletes_counted:true generation_id:gen-2 level:WARN msg:canonical repository retired nodes_deleted:1 outcome:dropped_relationships path:/eshu-7285/<nonce>/payments relationships_deleted:20 repo_id:repository:<nonce>-payments scope_id:git-repository-scope:repository:<nonce>-payments]
--- PASS: TestLiveRepositoryPathConflictRetirementReportsDroppedIncomingEdges (6.69s)
```

In each shape, `eshu_dp_canonical_repository_retirements_total{outcome=dropped_relationships}`
was 1 and `{outcome=clean}` was 0. A second, steady-state write of the same
generation left the counter at 1 and logged nothing.

Why `dropped_relationships` and not the ruling's `dropped_incoming_edges`: the
backend count covers both directions, and the retired node's 9 projector edges
are part of the 20. See "Label refinement on measured evidence" in the design
note.

## Survival legs added to TestLiveRepositoryRetryKeepsReducerEdges

- P4, stub adoption: before the first projection, the real
  `BatchCanonicalSubmodulePinEdgeCypher` creates a path-less stub Repository
  with a path-keyed PINS_SUBMODULE into it. The first projection keeps the
  stub's element id, and the pin survives the whole retry, generation 2 and
  delta chain.
- P3, delta generation: generation 3 is a delta (`DeltaProjection=true`, one
  changed file). Every reducer family count, the element id and REPO_CONTAINS
  are unchanged. The hermetic
  `TestCanonicalNodeWriterDoesNotReportRetirementWithoutTheCleanupPhase` pins
  that a delta builds no `repository_cleanup` statement.

These legs pass on main and on this change. They are survival guards, not
regressions.

## Stub re-creation after retirement (observed)

After the retirement, the real repo_dependency upsert and the submodule_pin
edge were run with the retired id as target. They produced:

```text
OBSERVED stub recreation after retirement: 1 Repository node(s) under the retired id repository:<nonce>-payments-rekeyed: [map[evidence_source:resolver/cross-repo incoming:[PINS_SUBMODULE DEPENDS_ON] path:<nil>]]
```

The result was identical under both executor shapes. The stub-MERGE writers
re-create a path-less Repository under the retired id. With no path, the
path-conflict retirement never matches it again. This is recorded as observed
behaviour, not asserted as correct.

## PROFILE (Neo4j 2026.09.0, Cypher 25, slotted runtime)

Steady state (no Repository at the path besides the projected id):

```text
+DetachDelete                 | r
+Filter                       | NOT r.id = $autostring_1
+NodeUniqueIndexSeek(Locking) | UNIQUE r:Repository(path) WHERE path = $autostring_0   rows 0, db hits 1
Total database accesses: 1
```

Retiring a degree-20 node (11 incoming, 9 outgoing):

```text
+DetachDelete                 | r                                                      rows 1, db hits 21
+Filter                       | NOT r.id = $autostring_1                               rows 1, db hits 1
+NodeUniqueIndexSeek(Locking) | UNIQUE r:Repository(path) WHERE path = $autostring_0   rows 1, db hits 1
Total database accesses: 23
Deleted 1 node, deleted 20 relationships
```

The statement is unchanged by this PR. Reporting reads the summary the
executor already consumed and adds no query.

No-Regression Evidence: retry-shape `CanonicalNodeWriter.Write` timing on neo4j:2026-community 2026.09.0. Two test binaries were built from the same tree, identical except for the production sources. The before binary used origin/main 182a31124 `canonical_node_writer.go`, `canonical_node_writer_retract.go` and `write_counts.go`, and `go tool nm` showed no retirement symbols in it. Each run did 5 warm-up and 40 timed writes of the same generation (FirstGeneration=false, 6 files, 3 directories, reducer edges seeded). There were 7 interleaved pairs at load average 14-16 on a shared host.

| pair | atomic before ms | atomic after ms | phase before ms | phase after ms |
| --- | --- | --- | --- | --- |
| 1 | 106.181 | 37.498 | 111.392 | 36.454 |
| 2 | 27.214 | 29.634 | 37.620 | 41.156 |
| 3 | 29.815 | 30.339 | 31.565 | 31.240 |
| 4 | 28.874 | 28.630 | 34.773 | 34.299 |
| 5 | 28.278 | 29.020 | 34.223 | 355.858 |
| 6 | 34.474 | 30.547 | 42.334 | 34.009 |
| 7 | 36.396 | 30.944 | 37.164 | 37.556 |

Median of per-pair medians: atomic_group 29.815 ms before vs 30.339 ms after. phase_group 37.164 ms before vs 36.454 ms after. Median paired delta: -0.2 ms (atomic) and -0.3 ms (phase). The single-pair outliers (pair 1 before, pair 5 phase after) fall in opposite arms and match host contention, so no regression is measurable.

Observability Evidence: `eshu_dp_canonical_repository_retirements_total{outcome}` (closed set `clean`, `dropped_relationships`; constants in `go/internal/telemetry/instruments_repository_retirement.go`) and the `canonical repository retired` log. The log is WARN when relationships were deleted and INFO when clean, with fields scope_id, generation_id, repo_id, path, nodes_deleted, relationships_deleted, deletes_counted and outcome. The GREEN run above shows both, on both executor shapes. Hermetic tests cover the atomic path under a simulated driver retry (last attempt counted, not summed), the phase-group clean path, sequential steady state (nothing recorded), a non-reporting executor (no retired line, only a distinct DEBUG line with `deletes_counted=false`, no counter), a failed phase-group cleanup and a rolled-back atomic group (nothing reported), and forwarding of every entry to a caller's collector. Review finding F1 added write-count reporting to the ingester's Bolt executor (`cmd/ingester/wiring_neo4j_executor.go`), whose in-process projector runs its own canonical writer. `TestIngesterNeo4jExecutorReportsWriteCountsOnEveryStatementPath` and `TestIngesterCanonicalWriterReportsPathConflictRetirement` (the real ingester executor chain with a fake Bolt driver, on both backend shapes) pin it. Each of the three `Write` call sites was mutation-checked: removing its report call fails its test.

## Review fixes: mutation proof

Each mutant below was applied, run, and restored, and `cmp` confirmed each
file byte-identical afterwards.

- F1: removing the ingester's `ReportWriteCounts` call on each path fails that
  path's test.
  - `Execute`: fails `.../Execute` and the NornicDB chain case.
  - File-group probe: fails `.../ExecuteGroup_file-group_probe`.
  - Plain `ExecuteGroup`: fails `.../ExecuteGroup` and the Neo4j chain case.
- F3: moving the atomic path's report ahead of the `ExecuteGroup` error check
  fails exactly `TestCanonicalNodeWriterDoesNotReportARolledBackAtomicRetirement`.
  Before that test existed, this mutant survived every test.

## Not checked

- NornicDB. The new live file is pinned `backends: neo4j` in
  `specs/live-tests.v1.yaml`, because NornicDB summary counters are measured as
  unreliable and Neo4j is the supported graph backend. The extended survival
  legs in `repository_retry_live_test.go` still run on both backends in CI but
  were run locally only on Neo4j.
- Golden corpus (B-7) was not run. Projection output is unchanged (only a
  counter and a log were added).

## Per-direction/per-type split of dropped relationships (item)

Base: origin/main 88beb12d0 (measurement tree; branch rebased onto 261ca79b8
with identical cumulative diff — `git range-diff` all `=`, patch-id
021c4b1d3a956ee613c30ecd8ee8a978893196f6 both sides). Graph backend for every
live number here:
`neo4j:2026-community` at the same pinned digest (`eabfbb04...`), a dedicated
local container with `NEO4J_AUTH=none`, the production schema applied by the
harness (`repository_id` and `repository_path` unique constraints present).
The NornicDB probe below ran against the pinned NornicDB image
(`ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-500-e022384c@sha256:74a8ed7b...`) in
an isolated Compose stack (`docker-compose.live-backend-nornicdb.yml`,
unique project name and host ports).

### Correctness-first reduction to a no-regression check (stated explicitly)

This item adds no new query, transaction, retry, or lock domain: the
retirement stays one statement in the same `repository_cleanup` phase
position with the same unique-index anchor and the same already-held
locks; retries stay idempotent (a retry matches nothing, so the collection
is empty); rollback suppression is unchanged (the report still reads only
after commit); the `Executor` interfaces are unchanged (records are
consumed at the existing Bolt seams). The only new work per write is the
same-statement `OPTIONAL MATCH` plus `collect` over the retired node's own
adjacency, one record consume on the retirement statement only, and
report-side aggregation only when a retirement deleted a node.
Performance proof therefore reduces to a no-regression check on the same
retry shape plus a bound on the report-side tail — not a full benchmark.

### Guard RED then GREEN

Rewriting `canonicalNodeRepositoryPathCleanupCypher` without the guard
update fails Rule 1 on the seeded reflowed case, exactly as designed:

```text
--- FAIL: TestRepositoryDeleteViolationsDetectsSeededDeletes (0.00s)
    canonical_repository_incoming_edge_guard_test.go:375: the path cleanup, reflowed: violations = [rule 1: Repository node r deleted outside canonicalNodeRepositoryPathCleanupCypher], want []
```

Updating the seeded reflowed expectation to the rewritten text in the same
commit returns the guard to green. Reach on the changed tree: 231
Repository Cypher strings, 22 with DELETE, 1 Repository node delete (the
rewritten constant), 6 incoming-Repository relationship deletes — every
floor still holds. The `repositoryIDDetachDelete` per-write check in
`canonical_node_writer_repository_test.go` is unchanged and passing: the
rewritten statement still anchors by path, never by id.

### No-Regression Evidence:

Two test binaries were built from the same tree, identical except for the
production sources (before = base-commit sources; the scratch benchmark
compiles on both trees). The scratch Bolt executor mirrors production: it
consumes the `RETURN dropped` record at the driver level whenever the
statement returns one and reports write summaries, so both binaries pay
the record round-trip. Each timed iteration projects an old-id first
generation (6 files, 3 directories), seeds the 11 incoming edges, then
times one retry-shape write of a new id at the same path (which retires
the old node and drops 20 relationships), and cleans its nonce. 5 warm-up
plus 40 timed writes per run, 7 interleaved before/after pairs per shape,
median of per-pair medians on `neo4j:2026-community`:

| pair | atomic before ms | atomic after ms | phase before ms | phase after ms |
| --- | --- | --- | --- | --- |
| 1 | 22.671 | 27.350 | 38.900 | 38.742 |
| 2 | 25.145 | 25.153 | 39.348 | 39.472 |
| 3 | 25.627 | 25.312 | 40.718 | 42.661 |
| 4 | 27.784 | 23.998 | 41.218 | 41.498 |
| 5 | 24.481 | 24.680 | 41.279 | 40.982 |
| 6 | 23.744 | 23.843 | 41.995 | 42.530 |
| 7 | 24.103 | 23.794 | 45.099 | 43.479 |

Median of per-pair medians: atomic_group 24.481 ms before vs 24.680 ms
after; phase_group 41.218 ms before vs 41.498 ms after. Median paired
delta: +0.008 ms (atomic) and +0.124 ms (phase), with pair outliers in
opposite arms matching host contention. No measurable regression.

Report-side tail microbench (after tree only): `summarizeDroppedEdges`
over a 20-edge split costs ~3.9 microseconds per retirement, about 0.01%
of a ~30 ms write — bounded negligible, and it runs only when a
retirement deleted a node.

### PROFILE both

Steady state (no Repository at the path besides the projected id):

```text
OLD: ProduceResults -> EmptyResult -> DetachDelete -> Filter -> NodeUniqueIndexSeek(Locking)
     total db hits 2, deleted 0 nodes 0 relationships
NEW: ProduceResults -> DetachDelete -> Eager -> EagerAggregation -> OptionalExpand(All) -> Filter -> NodeUniqueIndexSeek(Locking)
     total db hits 2, deleted 0 nodes 0 relationships
```

The anchor is unchanged (`NodeUniqueIndexSeek(Locking)` on
`repository_path`) and the steady-state db-hit total is identical.

Retiring a degree-20 node (11 incoming, 9 outgoing):

```text
OLD: ProduceResults -> EmptyResult -> DetachDelete (rows 1, db hits 21) -> Filter -> NodeUniqueIndexSeek(Locking)
     total db hits 23, deleted 1 node 20 relationships
NEW: ProduceResults -> DetachDelete (rows 1, db hits 21) -> Eager -> EagerAggregation (db hits 40) -> OptionalExpand(All) (rows 20, db hits 21) -> Filter -> NodeUniqueIndexSeek(Locking)
     total db hits 84, deleted 1 node 20 relationships
```

The deleted set is identical (1 node, 20 relationships); the collection
costs +61 db hits on a retiring write only — a write that happens at most
once per re-keyed path, dominated by the ~25-40 ms multi-statement write
the benchmark above measures end to end.

### NornicDB probe outcome

- CORRECTION (#8173): the claim below holds only when a real conflict
  exists. In steady state (same id, same path) the rewritten statement
  DETACH DELETEs the live node on NornicDB: the `OPTIONAL MATCH` + `WITH
  collect` composition drops the WHERE guard there. Full probe matrix,
  interleaving, and the backend partition fix are in
  `7324-retirement-nornicdb-steady-state.md`. NornicDB writers now emit
  the bare shape; the attributed shape is Neo4j-only.
- The rewritten statement deletes correctly on the pinned NornicDB: the
  old-id node is gone and the backend reports `relationships_deleted=20`
  on the production phase-group shape. `RETURN`-after-`DETACH DELETE` and
  multi-label incident-edge visibility both work.
- `RETURN dropped` on NornicDB yields the literal string `"dropped"`,
  not the collected list (same family as the `RETURN DISTINCT` literal
  pitfall in `docs/public/reference/nornicdb-pitfalls.md`). It parses to
  nil, so the entry stays unattributed by construction:
  `attribution=false`, only
  `eshu_dp_canonical_repository_retirements_total` moves. No NornicDB
  attribution is claimed anywhere; the production `PhaseGroupExecutor`
  path stays explicitly unattributed until a backend change makes the
  record readable.
- Out-of-scope observations, recorded not repaired: on NornicDB the
  atomic-shape fixture writes leave 0 outgoing projector edges (a
  non-production shape — production NornicDB never uses atomic groups for
  canonical writes), and the phase-group steady-state retry double-counted
  in the harness. Both concern statements this diff never touches (the
  fixture file/directory MERGEs and the unchanged last-entry-wins
  counting), so both predate it. Live proof stays `backends: neo4j` per
  the ledger.

### Live proof matrix (Neo4j, `backends: neo4j` pin kept)

- Positive
  (`TestLiveRepositoryPathConflictRetirementReportsDroppedIncomingEdges`,
  both shapes): exact 11 incoming + 9 outgoing = 20 with
  `sum(split) == relationships_deleted == 20`;
  `eshu_dp_canonical_repository_retired_relationships_total{direction=incoming,class=reducer}`
  11 and `{direction=outgoing,class=projector}` 9 with every other bucket
  dark and the bucket sum 20; the capped per-type map names 10 types with
  `overflow_n=3`; a steady-state retry records nothing on either counter.
  Direct graph checks pin the composition before the log is read.
- Negative
  (`TestLiveRepositoryPathConflictEdgelessRetirementReportsCleanSplit`,
  both shapes): a hand-built edgeless old-id node retires clean at INFO
  with `attribution=true`, zero direction totals, the outcome counter only.
- Ambiguous
  (`TestLiveRepositoryPathConflictConcurrentWritersBoundsSplit`, both
  shapes): four writers race the retirement with five distinct-type edges
  each; the floor holds on every path (`dropped_incoming >= 11`,
  `relationships_deleted >= 20`) while the sum stays unasserted — measured
  runs show the collection undercounting the delete on every shape
  (atomic 13 + 9 vs 24 and 14 + 9 vs 23; phase-group 14 + 9 vs 28),
  because a racing edge can commit between the statement's pre-delete
  collection and its delete under read-committed. Sum-exactness is a
  quiescence property, pinned by the positive scenario; under hammering
  every shape (atomic, phase-group, and the sequential fallback, whose
  retirement is likewise its own transaction) is floor-only.

### Observability Evidence:

- New counter
  `eshu_dp_canonical_repository_retired_relationships_total{direction=incoming|outgoing,class=reducer|projector|other}`:
  2 x 3 = 6 series max. Class follows `rel.evidence_source`
  (`projector/*` is projector, any other present source is reducer,
  missing is other). The raw relationship type never becomes a label.
- Extended WARN keeps every old field and gains `attribution=true` plus
  `dropped_incoming`/`dropped_outgoing` and the capped per-type map (top
  10 direction-qualified `incoming:TYPE` keys plus `overflow_n`). No
  attribution logs `attribution=false` and moves only the outcome
  counter; a non-reporting executor logs the distinct DEBUG line with
  `attribution=false` and moves nothing.
- Hermetic proof: canned 11 + 9 split with last-attempt-wins across driver
  retries, rollback suppression carrying no split, the unattributed and
  no-summary paths moving only (or nothing of) the old counter, the
  attributed edgeless clean case, and unit tests for the class/direction
  closing, the top-10 cap with deterministic tie-break, and the driver
  value parser (malformed rows skipped, empty list stays attributed).
