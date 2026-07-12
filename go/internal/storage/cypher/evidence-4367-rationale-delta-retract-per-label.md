# Evidence: rationale EXPLAINS delta retract fans out per target label (#4367, #5116 sibling)

## Theory probes (v1.1.11, before any code change)

Probed against a lean `timothyswt/nornicdb-cpu-bge:v1.1.11` container over the
Bolt HTTP endpoint, with a Function and a File node plus the production write
template:

| Shape | Result |
| --- | --- |
| Write: `UNWIND $rows AS row MATCH (target:Function\|...\|File {uid: row.target_entity_id}) ... MERGE ...-[rel:EXPLAINS]->...` | works — created 2/2 edges and both Rationale nodes |
| Delta retract: `MATCH (rationale:Rationale)-[rel:EXPLAINS]->(target:Function\|...\|File) WHERE target.path IN $file_paths ... DELETE rel` | broken — deleted 0, both edges survived |
| Delta retract per target label (`->(target:Function)`, then `->(target:File)`) | works — 0 edges remain |
| Whole-repo retract (`(rationale:Rationale)` single-label anchor, `WHERE rationale.repo_id IN`) | works — 0 edges remain |

Two theories died cheaply: the write template and the whole-repo retract are
NOT affected, so the fix is scoped to the delta (by-file) retract only. The
probes also refine the #5116 pitfall: the zero-row disjunction behavior applies
to bare `MATCH` + `WHERE` shapes on either end of the pattern, while the
row-driven `UNWIND` + inline-property-anchor disjunction matches correctly
(recorded in `docs/public/reference/nornicdb-pitfalls.md`).

## What changed

`BuildRetractRationaleEdgeStatementsByFilePath` replaces the single
disjunction-target statement with one statement per label in
`rationaleExplainsTargetLabels`, executed sequentially through the shared
`executeSequentialRetractStatements` path (the #5116 managed-transaction
under-apply forbids grouping). The write template's target disjunction is now
built from the same label list, so the write and retract sides cannot drift
(`TestRationaleRetractCoversEveryWriteTargetLabel`). The whole-repo retract is
untouched.

## Benchmark Evidence:

Failing-then-green live regression on the pinned production backend (behavior
change: the old delta retract returned wrong graph truth — stale EXPLAINS
edges — so the intended delta is proven, not identity with the broken output):

```bash
ESHU_REPLAY_TIER_LIVE=1 ESHU_GRAPH_BACKEND=nornicdb ESHU_NEO4J_DATABASE=nornic \
NEO4J_URI=bolt://localhost:17688 \
go test ./internal/replay/offlinetier/ -run TestReducerRationaleEdgeRetractGraphTruth -count=1
```

- BEFORE (disjunction-target statement): FAIL —
  `delta retract: changed-file Function EXPLAINS gone: count = 1, want 0`.
- AFTER (per-target-label sequential statements): ok, 3/3 runs (~0.9s package
  wall). The changed file's Function- and File-target edges retract to zero,
  the unchanged file's edge survives the delta retract and is then cleared by
  the whole-repo retract, and every node survives.

Cost shape: the delta retract is now 7 bounded statements in 7 auto-commit
transactions instead of 1 statement that deleted nothing — the same
fixed-fan-out class as the code-call, inheritance, and SQL retracts. There is
no correct faster baseline to regress against; the prior single statement was
buying its speed by not deleting anything.

## Observability Evidence:

No-Observability-Change. The retract statements keep the
`OperationCanonicalRetract` operation and the same parameters, and flow through
the existing canonical retract spans and graph-write failure/retry telemetry;
sequential execution surfaces per-statement errors through the same
`WrapRetryableNeo4jError` path. No metric name, span, log field, queue stage,
worker knob, or status field changes.
