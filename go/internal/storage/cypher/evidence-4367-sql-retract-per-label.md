# Evidence: SQL relationship retract covers the full write matrix with no unlabeled fallback (#4367, SQL sibling of #5116)

## Baseline and scope

PR #5128 fixed the NornicDB v1.1.11 managed-transaction under-apply by running
the six label-specific SQL retract statements sequentially (see
`evidence-5116-sql-retract.md`), and deliberately left two gaps:

1. The non-GroupExecutor path still fell through to a single unlabeled
   `MATCH (source)-[rel:...]->()` scan
   (`retractSQLRelationshipEdgesCypher`), which silently drops some source
   labels on v1.1.11 (documented in
   `docs/public/reference/nornicdb-pitfalls.md`).
2. The retract covered six fixed (source label, relationship type) pairs while
   the write path (`buildSQLRelationshipRowMap` /
   `labelScopedSQLRelationshipCypher`) accepts any of 7 source labels x 5
   relationship types. An edge written from a pair outside the six — possible
   the moment a reducer emission changes — would never be retracted, and
   nothing guarded the two sets against drifting apart.

This change closes both: the retract is now built per write-capable source
label (all 7, each statement carrying the full relationship-type disjunction,
still sequential, still the #4708 inline-anchor UNWIND shape), the unlabeled
fallback builder and constant are removed, every executor type routes through
the same per-label statements, and two guards pin the coverage to the write
side as the independent source of truth:

- `TestSQLRelationshipRetractCoversEveryWriteEndpointLabel`
  (retract labels contain `sqlRelationshipEntityLabels`), and
- `TestSQLRelationshipRetractCoversEveryWriteRelationshipType`
  (retract rel-type disjunction contains `sqlRelationshipWriteReasons`, the
  new production map both write templates gate on; its reason strings are
  byte-identical to the switch literals it replaces, so graph output is
  unchanged).

## No-Regression Evidence:

On everything the reducer emits today the deleted edge set is unchanged: the
seven per-label statements bind the same evidence source
(`reducer/sql-relationships`) and the same inline scope anchors as the six
fixed statements, and only this domain's writer produces edges with that
evidence source from these labels. The broadened matrix deletes precisely the
write-capable pairs the old set would have stranded. Live proof on the pinned
`timothyswt/nornicdb-cpu-bge:v1.1.11`:

```bash
bash scripts/verify-replay-tier.sh
```

`TestReducerSQLRelationshipRetractGraphTruth` (from #5128, unchanged) drives
the production `EdgeWriter.WriteEdges`/`RetractEdges` paths against this
builder on a real v1.1.11 backend: repository and delta-file scopes, all five
relationship types, scope and wrong-evidence controls, endpoint-node survival,
and idempotent re-retract all pass. Statement cardinality moves from six to
seven bounded statements per retract, the same fixed-fan-out class as the
code-call (six) and inheritance retracts; the removed fallback path was not a
performance path but a correctness hazard (unlabeled scan, wrong results on
v1.1.11).

Shape guards (no backend):

```bash
cd go && go test ./internal/storage/cypher -run 'TestEdgeWriterRetractEdgesSQLRelationship(DeltaScopesToFilePaths|RejectsDeltaWithoutFilePaths|Dispatch|RunsPerLabelStatementsSequentially)|TestSQLRelationshipRetractCoversEveryWrite(EndpointLabel|RelationshipType)|TestSQLRelationshipRetractStatementsUseSingleSourceLabel|TestBuildRetractSQLRelationshipEdgeStatementsUsesSharedParameters' -count=1
```

## Observability Evidence:

No-Observability-Change. The retract statements keep the
`OperationCanonicalRetract` operation, the same parameters, and flow through
the existing canonical retract spans and graph-write failure/retry telemetry;
sequential execution surfaces per-statement errors through the same
`WrapRetryableNeo4jError` path. No metric name, span, log field, queue stage,
worker knob, or status field changes.
