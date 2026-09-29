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

Observability Evidence: `eshu_dp_canonical_repository_retirements_total{outcome}` (closed set `clean`, `dropped_relationships`; constants in `go/internal/telemetry/instruments_repository_retirement.go`) and the `canonical repository retired` log. The log is WARN when relationships were deleted and INFO when clean or uncounted, with fields scope_id, generation_id, repo_id, path, nodes_deleted, relationships_deleted, deletes_counted and outcome. The GREEN run above shows both, on both executor shapes. Hermetic tests cover the atomic path under a simulated driver retry (last attempt counted, not summed), the phase-group clean path, sequential steady state (nothing recorded), a non-reporting executor (`deletes_counted=false`, no counter), a failed cleanup (nothing reported), and forwarding of every entry to a caller's collector. Each of the three `Write` call sites was mutation-checked: removing its report call fails its test.

## Not checked

- NornicDB. The new live file is pinned `backends: neo4j` in
  `specs/live-tests.v1.yaml`, because NornicDB summary counters are measured as
  unreliable and Neo4j is the supported graph backend. The extended survival
  legs in `repository_retry_live_test.go` still run on both backends in CI but
  were run locally only on Neo4j.
- Golden corpus (B-7) was not run. Projection output is unchanged (only a
  counter and a log were added).
