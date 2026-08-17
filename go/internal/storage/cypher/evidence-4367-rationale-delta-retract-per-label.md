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
(`TestRationaleRetractCoversEveryWriteTargetLabel`). The later #5998 provenance
repair keeps that target-label fan-out while combining the canonical and one
legacy rationale evidence source in each statement.

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

### Benchmark Evidence (#5998 provenance repair):

The #5998 provenance repair was also probed before implementation against the
pinned `eshu-nornicdb-pr290:3722b483c02c` image
(`sha256:c8141c6cd9ec270391562fabf1b047502a681e4d0036a15451b96967681930b9`).
The successful Go Bolt probe used
`rel.evidence_source IN $evidence_sources` with the exact bounded parameter
`[reducer/rationale, finalization/workloads]`:

| Scope | Statements | Relationships deleted | Query wall time |
| --- | ---: | ---: | ---: |
| Full repository | 1 | 2 | 2.481 ms |
| Changed path, seven target labels | 7 | 14 | 18.292 ms total |

The full probe preserved `custom/rationale` and `foreign/tool` edges in the
same repository. The delta probe preserved those sources on the changed path,
the legacy edge on an unchanged path, and all edges in another repository.
`PROBE_RESULT=PASS` and `GO_PROBE_RC=0`; the retained transcript has SHA-256
`1a9f7316cec43946202e830cc008ac6491a0fa3063e084eb46469af45908362a`.
An earlier Python-driver attempt was unusable because its driver rejected the
backend agent string; none of its timings are used here.

## No-Observability-Change (#5998 provenance repair):

The retract statements keep the `OperationCanonicalRetract` operation and flow
through the existing canonical retract spans and graph-write failure/retry
telemetry. Canonical rationale cleanup changes its evidence parameter from one
string to a bounded two-string list; custom sources retain the exact
single-source parameter. Sequential delta execution surfaces per-statement
errors through the same
`WrapRetryableNeo4jError` path. No metric name, span, log field, queue stage,
worker knob, or status field changes.
