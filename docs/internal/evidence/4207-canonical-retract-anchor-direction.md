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
- Timings come from read-only `count(r)`/`elementId(r)` twins of the production
  statements. They share the match plan with the `DELETE` forms but do not
  include delete-commit cost.
- This is a query-shape proof plus a focused regression test. It has not yet been
  re-proven by a full-corpus drain on the built binary; that run is the remaining
  step before the dead-letter count can be claimed to be zero.
- The measurements were taken on a single idle host. The failing production run
  was under saturation (~15 of 16 cores busy). Saturation was not the cause —
  the defect reproduces at 500 ms per path on a completely idle backend — but
  contention would have made the same statement slower still.

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
