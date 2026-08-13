# 4207 — canonical retract scanned the Directory label once per input path

## The problem

A full-corpus run ended with 11 `projector`/`source_local` work items dead-lettered
at `attempt_count: 3`, every one with `failure_class: graph_write_timeout`:

```
phase-group retract statement 3/7 part 1/1 (duration=2m0.000413523s,
first_statement="UNWIND $file_paths AS file_path | MATCH (f:File {path: file_path})")
neo4j execute timed out after 2m0s
```

Three things about those failures ruled out the obvious explanations:

- **The work item's size did not predict failure.** Failing scopes ranged from 57
  to 59,717 facts. One repository with 15 files projected in 0.37 s on its first
  pass and then blew the full 120 s budget on its second.
- **Chunking was already in place and did not help.** Statements carry
  `part K/P`; the failing chunks carried at most 25 file paths.
- **Only the second generation failed.** The first pass logs
  `canonical retract skipped for first generation`. Retract only runs once the
  graph is populated.

Together those say the cost came from the graph being scanned, not from the
payload. It did.

## Root cause

Two retract statements walked a `CONTAINS` edge *into* a node they had already
anchored by indexed path, writing the pattern from the unbound side:

```cypher
MATCH (f:File {path: file_path})
MATCH (:Directory)-[r:CONTAINS]->(f)          -- statement 3
```

```cypher
MATCH (p:Directory)-[r:CONTAINS]->(d:Directory {path: row.path})   -- statement 5
```

NornicDB picks the expansion start from the pattern's **left** node only. In
`pkg/cypher/match_multi.go` (`executeChainedMatch`), `boundStartNode` selects
`traverseFromNode`; `boundEndNode` is computed but used solely as a post-filter,
so a bound variable on the right buys nothing. With `""` on the left the query
takes `traverseGraph`, which does `GetNodesByLabel("Directory")` — a full label
scan that deep-copies every node — then expands each Directory's outgoing
`CONTAINS` and discards everything except the one path whose endpoint is `f`.

Because a plain (non-`DETACH`) `DELETE` matches no UNWIND batch template, the
statement re-runs that whole sweep **once per input path**. Twenty-five paths
meant twenty-five full sweeps of the Directory population.

`File(path)` was never the problem: it already carries both a uniqueness
constraint and a NornicDB lookup index, and the anchor `MATCH` alone measures
1.2 ms for 25 paths.

## The fix

Write both patterns from the anchored side, so the traversal reads that node's
own adjacency:

```cypher
MATCH (f)<-[r:CONTAINS]-(:Directory)
MATCH (d:Directory {path: row.path})<-[r:CONTAINS]-(p:Directory)
```

This is the rewrite already applied to the workload catalog queries for #3466
and #1731, for the same backend reason. It puts the anchored node in
`StartNode`, which additionally lets the first statement take NornicDB's
bound-relationship-delete fast path (`GetIncomingEdges`) and skip the generic
multi-match executor entirely.

Statement 5 had the identical defect and sits directly behind statement 3 in the
same phase. Because execution stops at the first failure, the run never reached
it — fixing only statement 3 would have moved the timeout, not removed it.

## Performance Evidence:

Measured against the pinned NornicDB build (Compose source pin
`3722b483c02c`, image `eshu-nornicdb-pr290:3722b483c02c`), single container, no
auth, embeddings/BM25/vector disabled, on an otherwise idle 16-vCPU / 123 GiB
Linux x86_64 host (load average below 1.5 throughout). Eshu graph schema applied
first, including the `File(path)` and `Directory(path)` constraints and the
NornicDB lookup indexes. Timings are the best of 3 runs via the Bolt driver
against read-only `count(r)` twins of the production statements, so repeated
measurement does not mutate the graph.

**Graph population varied, parameter list held at 25 paths:**

| File nodes | Directory nodes | Current shape | Rewritten shape |
| ---: | ---: | ---: | ---: |
| 2,000 | 200 | 1,345.2 ms | 4.4 ms |
| 10,000 | 1,000 | 7,704.1 ms | 3.6 ms |
| 15,000 | 1,500 | 12,664.3 ms | 4.3 ms |

The current shape grows with the graph. The rewritten shape does not move.

**Parameter list varied, population held at 15,000 Files / 1,500 Directories:**

| Paths | Current | Current per path | Rewritten | Rewritten per path |
| ---: | ---: | ---: | ---: | ---: |
| 1 | 528.4 ms | 528.4 ms | 0.7 ms | 0.70 ms |
| 5 | 2,533.9 ms | 506.8 ms | 1.2 ms | 0.24 ms |
| 25 | 12,664.3 ms | 506.6 ms | 4.3 ms | 0.17 ms |
| 100 | 51,902.6 ms | 519.0 ms | 18.9 ms | 0.19 ms |

Per-path cost is flat in list length. That is why chunking could not fix this:
the 120 s budget was already gone at 25 paths, and smaller chunks carry the same
cost per path.

**Which population term dominates** — File count (15,000) and `CONTAINS` edge
count (15,025) held fixed while only bare Directory nodes were added, at 5 paths:

| Directory nodes | Current | Rewritten |
| ---: | ---: | ---: |
| 1,550 | 2,643.9 ms | 1.3 ms |
| 16,550 | 10,003.8 ms | 1.3 ms |
| 31,550 | 17,821.8 ms | 1.3 ms |

Directory population alone drives it, confirming the label scan. Fitting the two
linear terms over these points gives roughly 0.098 ms per Directory node and
0.025 ms per Directory-outgoing `CONTAINS` edge, **per input path**. The
rewritten shape is unaffected by either.

**Exactness.** Both pairs were compared as edge-identity sets, not counts:
`elementId(r)` collected and sorted, then fingerprinted, at 15,000 Files /
1,550 Directories / 25 paths.

| Statement | Before | After | Edges matched | Edge-set fingerprint |
| --- | ---: | ---: | ---: | --- |
| Directory→File refresh | 13,092.3 ms | 4.5 ms | 25 | `7c10c373c422880b` both sides |
| Directory parent refresh | 12,903.8 ms | 5.9 ms | 25 | `a608611cbff9ab99` both sides |

Identical edge sets, so this is a pure query-shape change: 2,909x and 2,187x on
the same input.

**Full corpus, built binary, real retract path.** The numbers above are query
shape. They do not by themselves show the eleven dead letters stop, so the
production path was run on the 896-repository corpus, twice, on the remote
16-vCPU / 123 GiB Linux x86_64 host, same NornicDB image
(`eshu-nornicdb-pr290:3722b483c02c`), same Compose topology, same knob profile,
same 120 s `ESHU_CANONICAL_WRITE_TIMEOUT`, clean volumes both times.

| | before | after |
| --- | --- | --- |
| commit | `435c76f63` (`origin/main`) | `f06bfd934` (this branch) |
| run | `4207-full-corpus-drain-r2-20260812T205108Z` | `4207-full-corpus-drain-branch-20260813T042531Z` |
| `projector`/`source_local` | 885 succeeded, **11 dead_letter** | **896 succeeded, 0 dead_letter** |
| whole queue | 14,961 of 14,972 succeeded | **14,427 of 14,427 succeeded** |
| `pending`/`in_flight`/`retrying`/`failed` | 0 / 0 / 0 / 0 | 0 / 0 / 0 / 0 |
| terminal | time box, 4207 s (1h10m07s) | open-zero stable, 2305 s (38m25s) |

The `after` totals are lower because reducer `workload_materialization` fanned
out to 2,289 items rather than 2,834. That is unrelated to this change and the
two totals are not a speedup comparison.

**The clean drain does not on its own prove anything about retract.** Retract
only runs from the second generation onward, and a healthy drain never reaches
one: the `after` run logged 897 `canonical retract skipped for first generation`
against 896 completed writes, and `fact_work_items` held zero rows with
`reopened_at` set. Every write in that run skipped the retract entirely. In the
`before` run, exactly eleven generations were re-driven —
`recoverWedgedActiveGenerationsQuery` re-enqueues `source_local` for a generation
whose shared-projection intents are still incomplete past the activation
deadline — and all eleven of those re-projections died in retract. The `after`
run drained cleanly and fast enough that nothing ever looked wedged, so the
sweep never fired.

So the second generation was driven deliberately, against the populated `after`
graph (42,498 `Directory`, 137,420 `File`, 1,255,037 `CONTAINS`), by applying
that sweep's own `ON CONFLICT DO UPDATE` clause by hand to the same eleven scope
IDs that dead-lettered in the `before` run. Everything past that point is the
production path: the same built binary, the same `PhaseGroupExecutor`, the same
statements, the same budget.

| Scope | Retract statements | Before | After |
| --- | ---: | ---: | ---: |
| `r_0a682efa` | 7 | 120.012 s timeout | **53.645 s** |
| `r_225deaee` | 7 | 120.018 s timeout | **53.622 s** |
| `r_3ab2a45c` | 7 | 120.006 s timeout | **53.645 s** |
| `r_481e6111` | 7 | 120.017 s timeout | **53.300 s** |
| `r_7d847de0` | 6 | 120.022 s timeout | **52.938 s** |
| `r_879aeab2` | 8 | 120.026 s timeout | **52.952 s** |
| `r_b0d5beed` | 123 | 122.775 s timeout | **1036.950 s (17m16.950s)** |
| `r_da7154c8` | 6 | 120.023 s timeout | **52.936 s** |
| `r_db17141a` | 6 | 120.025 s timeout | **47.306 s** |
| `r_e4d34c93` | 6 | 120.015 s timeout | **47.343 s** |
| `r_f48003a5` | 6 | 120.029 s timeout | **47.051 s** |

Eleven of eleven retract phases completed, and all eleven work items went on to
`succeeded`. Zero `phase-group retract statement … timed out` lines, against 44
in the `before` run's projector log, and zero `canonical phase failed`. The
`before` run completed no retract phase at all, on any scope, at any point. With
the re-projections folded in, the queue ended at 14,471 of 14,471 succeeded and
no row in any other state.

**What that leaves.** Clearing statement 3 exposes what was behind it. The
remaining ~47–53 s on a six-statement retract is roughly two statements at
~26 s each, and the shape is familiar:

```cypher
MATCH (d:Directory) WHERE d.repo_id = $repo_id AND d.generation_id <> $generation_id
MATCH (n:CloudFormationExport) WHERE n.repo_id = $repo_id AND …
```

Each is a full label scan filtered afterwards, each drained 0 rows, and each
costs ~26–28 s against this graph. `r_b0d5beed` runs 123 of them, which is the
whole 17 minutes. No single statement exceeds the 120 s per-statement budget, so
none of this dead-letters — it is slow, not broken. It is the same defect family
as the one fixed here at a different set of statements, and it is not addressed
by this change.

**Regression test.** `TestCanonicalRetractStatementsAnchorTraversalsOnIndexedSide`
runs the statements the production writer actually builds — full-refresh, entity
and delta branches — through `FindUnanchoredRetractTraversals`. Against the
pre-fix Cypher it fails on both offending statements:

```
retract statement expands into its indexed anchor "f" from an unbound label: (:Directory)-[r:CONTAINS]->(f)
retract statement expands into its indexed anchor "d" from an unbound label: (p:Directory)-[r:CONTAINS]->(d:Directory {path: row.path})
```

and passes after. The same helper backs the negative test, so weakening the rule
breaks that test too.

## No-Observability-Change:

No metric, span, log or status field changes. The path was already diagnosable
and that is how it was attributed: the existing failure message from
`PhaseGroupExecutor.executeSequentialRetractPhase` carries the statement index,
chunk index, elapsed duration and a two-line statement summary, which is what
identified the exact statement, and `canonical phase failed` records the phase,
mode, statement count and duration per scope. Both remain unchanged.

## Limits of this claim

- The probe graph contained only `Repository`, `Directory`, `File`, `Module` and
  (for the isolation round) `Function` nodes. A real corpus graph carries many
  more labels and nodes, so the absolute per-node constants above should be read
  as the shape of the cost, not as a predictor of a specific corpus.
- The isolated timings come from read-only `count(r)`/`elementId(r)` twins of the
  production statements. They share the match plan with the `DELETE` forms but do
  not include delete-commit cost. The corpus run above does include it.
- The corpus comparison is one run per arm, not a distribution. What makes it
  worth reading is that the before arm failed 11 of 11 and the after arm passed
  11 of 11 on the same scopes, not that any single duration is repeatable.
- The `before` arm ran at `435c76f63`, seven commits behind this branch's merge
  base. Those seven touch `reportbundle`, `report_cmd`, `ci_cd_run_correlation`,
  two query live tests, and CI gate scripts; none touch the projector, the
  canonical writer, or any retract statement.
- The second generation in the after arm was driven by hand, using the recovery
  sweep's own upsert clause, because a clean drain never wedges a generation and
  so never triggers the sweep. The re-projection it produced is the production
  path; how the work item got back to `pending` is not.
- `bootstrap-index` exited 1 in the after arm. One repository (`r_dc9bd67e`,
  158,615 facts) hit the 120 s budget once on a `structural_edges` chunk —
  `MATCH (c:Class {name: row.class_name, path: row.file_path…})`, a different
  phase and a different statement — and succeeded on retry. The queue ended
  clean; the non-zero exit is `bootstrap-index` reporting the state it saw at its
  own exit, before the projector service finished the retry.
- The isolated measurements were taken on a single idle host. The failing
  production run was under saturation (~15 of 16 cores busy). Saturation was not
  the cause — the defect reproduces at 500 ms per path on a completely idle
  backend — but contention would have made the same statement slower still.

## Related, not fixed here

The reducer's recurring `repo_dependency_projection_lease_quarantined` failure
(`repo dependency retract evidence_artifacts: context deadline exceeded`) is a
different execution path: `EdgeWriter.executeRepoDependencyRetractStatements`
runs three sequential auto-commit statements under one 45 s cycle budget, with no
phase-group chunking and no drain loop, and its failures never reach
`fact_work_items` at all — `shared_projection_intents` has no status or
attempt-count column, so a failed cycle is indistinguishable from one never
attempted.

The statement that reports the deadline, `retractRepoEvidenceArtifactsCypher`,
does **not** have the anchor defect; it expands outward from
`(source_repo:Repository {id: ...})`. The statement *before* it in the same
budget, `retractRepoRunsOnRelationshipsCypher`, does:

```cypher
MATCH (repo:Repository {id: repo_id})-[:DEFINES]->(w:Workload)
MATCH (i:WorkloadInstance)-[:INSTANCE_OF]->(w)
```

`w` is already bound, and the second `MATCH` starts from the unbound
`(i:WorkloadInstance)` — the same shape proven above. That makes it a plausible
consumer of the budget the last statement then runs out of, but this was not
separately measured and the fix is not a free arrow flip: the comment above
`canonicalRunsOnUpsertCypher` records that on NornicDB v1.1.11 a chained
multi-hop pattern with a direction reversal matched zero rows, so any rewrite
there needs its own live equivalence proof against the pinned build before it
can land.
