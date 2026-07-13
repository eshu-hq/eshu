# Evidence: IAM privilege edges, secrets/IAM workload edge, and Variable node retract-depth backfill (#4367 C-14, final slice)

This is the last slice of the C-14 #4367 retract-depth backfill: the IAM
privilege edges (`CAN_ASSUME`, `CAN_ESCALATE_TO`, `CAN_PERFORM`), the
secrets/IAM workload edge (`SECRETS_IAM_USES_SERVICE_ACCOUNT`), and the last
uncovered `retractable_type`, the `Variable` node.

## Governance decision: secrets/IAM writer-level exercise (ADR #1314)

**Decision: proceed with a writer-level live test. No production activation.**

`go/cmd/reducer/secrets_iam_graph_wiring.go` gates
`ESHU_REDUCER_SECRETS_IAM_GRAPH_PROJECTION_ENABLED` at exactly one point:
`secretsIAMGraphProjectionWriter` returns `nil` when the flag is unset, which
keeps `DomainSecretsIAMGraphProjection` unregistered in `cmd/reducer`'s intent
dispatch. The flag has no awareness inside `cypher.SecretsIAMGraphWriter`
itself -- the type is fully functional and ungated. Calling
`cypher.NewSecretsIAMGraphWriter(exec, 0)` directly, as this slice's
`TestReducerSecretsIAMEdgeRetractGraphTruth` does, never touches
`cmd/reducer`'s domain registry, the intent-dispatch loop, or the flag itself:
it is mechanically identical to constructing any other unwired `cypher.*Writer`
type in a unit or live test. This is not production activation, and it does
not change any default. It mirrors the pre-existing ADR #1314 Section 11
writer-level conformance proof
(`go/internal/storage/cypher/secrets_iam_graph_live_test.go`,
`TestSecretsIAMGraphWriterLiveConformance`, gated on a separate
`ESHU_SECRETS_IAM_GRAPH_LIVE` env var), which this slice's offlinetier-package
test complements rather than duplicates: the existing test proves full
node+edge conformance for all four SecretsIAM* families; this slice's test is
scoped to the one uncovered manifest surface
(`SECRETS_IAM_USES_SERVICE_ACCOUNT`) and is wired into
`scripts/verify-replay-tier.sh`'s `-run` list and the manifest's `replay-tier`
`proof_gate`, which the pre-existing cypher-package test is not.

The B-12 golden-corpus snapshot's zero secrets/IAM counts are untouched: they
assert the *reducer projection's* output over the 20-repo corpus, which stays
gated OFF, and this slice never runs `cmd/reducer` at all.

## Problem: IAM privilege edges

`IAMCanAssumeEdgeWriter.RetractIAMCanAssumeEdges`,
`IAMEscalationEdgeWriter.RetractIAMEscalationEdges`, and
`IAMCanPerformEdgeWriter.RetractIAMCanPerformEdges` each dispatched their
single retract DELETE statement through the shared `dispatch()` helper:

```go
func (w *Writer) dispatch(ctx context.Context, stmts []Statement) error {
	if ge, ok := w.executor.(GroupExecutor); ok {
		return WrapRetryableNeo4jError(ge.ExecuteGroup(ctx, stmts))
	}
	for _, stmt := range stmts {
		if err := w.executor.Execute(ctx, stmt); err != nil {
			return WrapRetryableNeo4jError(err)
		}
	}
	return nil
}
```

In production, `cmd/reducer`'s `reducerNeo4jExecutor.ExecuteGroup`
(`cmd/reducer/reducer_executor_adapters.go`) is wired unconditionally for
every graph backend including NornicDB. On the pinned NornicDB v1.1.11, a
DELETE dispatched through `ExecuteGroup` under-applies even for a single
statement (`docs/public/reference/nornicdb-pitfalls.md`, "Node-Label
Disjunction" pitfall and its managed-transaction-DELETE refinement) -- exactly
the bug class already fixed for the five cloud-correlation writers in
`evidence-4367-cloud-edge-retract.md` and tracked for nine more writers in
issue #5152. These three IAM privilege-edge writers were not on #5152's
original list; they were found while auditing the C-14 #4367 retract-depth
backlog for this final slice.

### Fix

Each of the three writers now routes its retract statement through a
`dispatchRetract` helper (sequential `Execute`, never `ExecuteGroup`):

- `IAMCanAssumeEdgeWriter.dispatchRetract` (new) --
  `RetractIAMCanAssumeEdges` now calls it instead of `dispatch`.
- `IAMEscalationEdgeWriter.dispatchRetract` (new) --
  `RetractIAMEscalationEdges`.
- `IAMCanPerformEdgeWriter.dispatchRetract` (new) --
  `RetractIAMCanPerformEdges`.

The write paths (`WriteIAM*Edges`) are unchanged: they still dispatch through
`dispatch`, so a `GroupExecutor`-capable executor still batches multi-row
MATCH-MATCH-MERGE writes atomically. Only the retract paths were re-routed.

Each fixed writer carries a no-group unit guard built on
`sqlSequentialRecordingExecutor` (`edge_writer_sql_retract_test.go`), asserting
zero `ExecuteGroup` calls plus the expected sequential retract statement shape
(`iam_edge_retract_dispatch_test.go`):

- `TestIAMCanAssumeEdgeWriterRetractNeverGroups`,
  `TestIAMEscalationEdgeWriterRetractNeverGroups`,
  `TestIAMCanPerformEdgeWriterRetractNeverGroups` (new file).
- `TestIAMEscalationEdgeRetractScopesByEvidenceSource` and
  `TestIAMCanPerformEdgeRetractScopesByEvidenceSource`
  (`iam_escalation_edge_writer_test.go`, `iam_can_perform_edge_writer_test.go`)
  previously used `recordingGroupExecutor` and asserted the statement landed
  in `groupCalls` -- i.e. they asserted the *buggy* grouped-dispatch shape.
  Converted to `sqlSequentialRecordingExecutor` with a `len(groupCalls) == 0`
  assertion, mirroring `TestSecurityGroupReachabilityRetractScopesByEvidenceAndScope`'s
  conversion in the cloud-edge slice. `TestIAMCanAssumeEdgeWriter*` tests
  already used the plain `recordingExecutor` (no `GroupExecutor`), so they were
  unaffected and needed no change.

All four guards verified to fail when their writer's retract is reverted to
`dispatch()` (confirmed locally before restoring the fix).

## Problem: secrets/IAM workload edge

`SecretsIAMGraphWriter.RetractScope` was already correct: it calls
`w.dispatchSequential(ctx, stmts)` unconditionally, never checking for
`GroupExecutor`. This was true from the writer's original commit -- unlike the
other writers audited in this backfill, no fix was needed here. This is a
missing-coverage-proof slice only. (See the #5152 comment cross-reference
below: the issue's original list included this file, but the code on `main`
already carries the safe pattern.)

## Investigation: Variable node (retractable_type, NOT closed this slice)

`Variable` is the last uncovered `retractable_type`
(`retractable_node:Variable`). The task's working assumption was that the
`multi-generation-tombstone.json` delta cassette simply lacks a `variable`
`content_entity` fact, the same gap every other now-covered label had. That
assumption does not hold for `Variable`; the actual cause is a real,
previously-undiscovered production write-path bug, out of scope for a
retract-depth backfill to fix unilaterally. Filed as a follow-up
(`#5156` / see the spawned session) rather than silently redesigned
here.

### Why the cassette-extension route (used for every other label) doesn't work

`go/internal/projector/canonical_builder.go` (~line 188) explicitly skips
writing `"Variable"` through the generic canonical entity phase that every
other content-entity label (including `Class`, `Function`, `Annotation`, etc.)
uses:

> Plain Variable rows remain in the content store/search surface. The
> reducer-owned semantic entity path writes the much smaller graph subset for
> module attributes and TSX component assertions.

The offline delta-tier pipeline that drives `multi-generation-tombstone.json`
(`go/internal/replay/offlinetier`) only exercises the generic canonical entity
phase -- it never calls `cypher.SemanticEntityWriter.WriteSemanticEntities`,
the "reducer-owned semantic entity path" the comment refers to. Adding a
`variable` `content_entity` fact to the cassette, mirroring the `class`/
`function`/etc. entries, would be silently dropped by `canonical_builder.go`'s
explicit skip and would never reach any writer through this pipeline --
`TestDeltaEntityRetractGraphTruth`'s gen1 assertion (`count = 1`) would fail
immediately, not even reach the retract assertion. Confirmed by reading the
skip and the offlinetier package (`rg -n "SemanticEntity" go/internal/replay/offlinetier/*.go`
returns nothing).

### Why the "reducer-owned semantic entity path" also doesn't create the node on NornicDB

Further tracing `cypher.SemanticEntityWriter` (the only writer that can create
a `:Variable` node -- `rg -n ":Variable\b" go/internal --type=go`, excluding
`TerraformVariable`/`TypeVariable` matches, returns exactly one `MERGE`
template, in `semantic_entity_statements.go`):

1. `go/cmd/reducer/neo4j_wiring.go`'s `semanticEntityWriterForGraphBackend`
   constructs the writer for NornicDB (the default backend) via
   `NewSemanticEntityWriterWithCanonicalNodeRows(executor, batchSize).WithLabelScopedRetract()`
   -- the only mode NornicDB production ever uses, unconditionally.
2. `"Variable"` is listed in `semanticEntityCanonicalNodeClearProperties`
   (`semantic_entity_statements.go` ~line 425), marking it "canonical-node-owned"
   -- the design assumes some OTHER writer already created the base node by
   `uid`, and this writer only enriches it.
3. `semanticEntityCanonicalNodeRowsUpsertCypher` rewrites the upsert's
   `MERGE (n:Variable {uid: row.entity_id})` to
   `MATCH (n:Variable {uid: row.entity_id})` for every canonical-node-owned
   label (~line 312).
4. No other writer anywhere in the codebase creates a bare `:Variable` node.
   For `Function` (also canonical-node-owned) this design is sound: the
   generic canonical entity phase DOES write `Function` nodes (it is not in
   `canonical_builder.go`'s skip list), so the semantic-entity `MATCH` finds
   them. For `Variable`, which IS skipped there, nothing ever creates the
   base node.

### Empirical proof (throwaway probe, not part of the committed suite)

Ran a scratch test against a lean NornicDB v1.1.11 container: seeded a `File`
node, then called
`cypher.NewSemanticEntityWriterWithCanonicalNodeRows(exec, 0).WithLabelScopedRetract().WriteSemanticEntities(...)`
with one `Variable` row shaped as a real Elixir module attribute
(`language: "elixir"`, `entity_metadata.attribute_kind: "module_attribute"`,
matching `isElixirModuleAttributeSemanticEntity`'s predicate exactly).

```
write result: {CanonicalWrites:1}
Variable node count after CanonicalNodeRows write (MATCH-not-MERGE expected if canonical-node-owned) = 0
```

The writer's return value claims one canonical write (`CanonicalWrites: 1` is
just the count of rows the writer attempted, not a verified write count); the
graph-level count proves nothing was actually created. This means: **on the
default NornicDB backend, Elixir module-attribute and TSX
component-type-assertion facts are silently dropped from the graph today.**

### Why this is not fixed in this slice

This is a write-path accuracy bug, not a retract-dispatch bug -- a different
class than everything else in the C-14 #4367 backlog, which assumes existing
write paths work and audits only retract correctness. A fix requires an
architecture decision among at least three options (make `Variable`
non-canonical-node-owned and MERGE it directly; stop skipping `Variable` in
`canonical_builder.go`'s generic phase; or add a dedicated minimal
node-creation phase for `Variable`, the way `Module`/`Parameter` have their
own phases F/G), each with unknown interaction with the content-search
surface and the `code_search_index` fulltext index (which already includes
`Variable`). Per this repo's golden rule to fix root cause, not symptoms, and
to ask rather than assume architecture/design ownership, this was filed as a
follow-up rather than driven through unilaterally inside a retract-coverage
task.

### Manifest disposition

`retractable_node:Variable` is left **uncovered** in
`specs/replay-coverage-manifest.v1.yaml` -- neither claimed (would be false;
the write silently no-ops) nor exempted (an exemption asserts the surface
genuinely cannot have a scenario for a structural reason an operator would
accept; this is a live bug blocking coverage, not a structural non-requirement,
and marking it exempt would misrepresent the bug as an accepted gap).
`retractable_type` coverage stays at 86/87. Confirmed this gap is advisory,
not blocking: `bash scripts/verify-replay-coverage-gate.sh --blocking` passes
with `retractable_type` and `retractable_edge_type` gaps counted as
"advisory-warn," not "required-fail" (see Gate Evidence below) -- the same
posture the C-14 backfill has carried across every prior PR that didn't reach
100%.

## Benchmark Evidence:

Failing-then-green live regression on the pinned production backend for the
IAM privilege edges (behavior change -- the old dispatch path silently left
stale edges after retract):

```bash
cd go && ESHU_REPLAY_TIER_LIVE=1 ESHU_GRAPH_BACKEND=nornicdb ESHU_NEO4J_DATABASE=nornic \
NEO4J_URI=bolt://localhost:17697 NEO4J_USERNAME=neo4j NEO4J_PASSWORD=change-me \
go test ./internal/replay/offlinetier/ -run 'TestReducerIAMEdgeRetractGraphTruth' -count=1 -v
```

- RED (retract statements temporarily reverted to `w.dispatch` to reproduce
  the pre-fix executor shape):
  - `delta_tier_reducer_iam_edge_retract_live_test.go:173: retract: in-scope CAN_ASSUME gone: count = 1, want 0`

  This one verbatim line covers CAN_ASSUME because the test fails fast at its
  first post-retract assertion; the remaining two writers' pre-fix
  under-application follows from the identical `dispatch()`-through-
  `ExecuteGroup` mechanism (same helper, same revert), confirmed independently
  by each writer's own no-group unit guard failing the same way when reverted
  (see the IAM privilege edges section above).
- GREEN: `ok  github.com/eshu-hq/eshu/go/internal/replay/offlinetier` 3/3 runs
  (~0.2s each) after restoring `dispatchRetract`. The test writes and retracts
  CAN_ASSUME, CAN_ESCALATE_TO, and CAN_PERFORM. Out-of-scope controls (a
  different `scope_id`, same `evidence_source`) survive every retract, and
  every endpoint `CloudResource` node survives.

For `SECRETS_IAM_USES_SERVICE_ACCOUNT`, no RED was possible or needed --
`RetractScope` was already correct. GREEN 3/3 on the same command with
`-run 'TestReducerSecretsIAMEdgeRetractGraphTruth'` proves the writer-level
write+retract, an out-of-scope survivor, and retained `KubernetesWorkload`
endpoint survival.

Full backend gate (`scripts/verify-replay-tier.sh`, both new tests plus every
existing offlinetier live test) PASSED end-to-end against a fresh v1.1.11
container:

```bash
ESHU_REPLAY_TIER_HTTP_PORT=18483 ESHU_REPLAY_TIER_BOLT_PORT=18698 bash scripts/verify-replay-tier.sh
```

Cost shape: retract statements now run as one bounded auto-commit transaction
per IAM writer instead of one managed transaction -- the same fixed-fan-out
class as every prior #4367/#5152 fix. The prior grouped path bought its single
transaction by silently not deleting the edge, so there is no correct faster
baseline to regress against.

## Coverage claim scope

Claimed in `specs/replay-coverage-manifest.v1.yaml`: `CAN_ASSUME`,
`CAN_ESCALATE_TO`, `CAN_PERFORM`, `SECRETS_IAM_USES_SERVICE_ACCOUNT` -- all
four proven live+retract above. `retractable_edge_type` coverage moves from
41/52 to 45/52 (`bash scripts/verify-replay-coverage-gate.sh --blocking`).
`retractable_type` (which includes `retractable_node` entries) stays at
86/87; `Variable` remains the sole gap (see Investigation above).

`retractable_edge:ATLANTIS_DEPENDS_ON`, `HELM_VALUE_REFERENCE`, `IMPORTS`,
`MANAGES`, `USES`, `USES_PROFILE`, `USES_WORKFLOW` remain **NOT claimed** --
out of this slice's assigned scope (CAN_ASSUME / CAN_ESCALATE_TO / CAN_PERFORM
/ SECRETS_IAM_USES_SERVICE_ACCOUNT / Variable), owned by different writers.

Issue #5152 comment posted noting: (a) `secrets_iam_graph_writer.go`'s entry
on that issue's list is stale against current `main` (already
`dispatchSequential`, no fix needed, live-proven here), so its remaining count
shrinks to eight; (b) the three IAM privilege-edge writers fixed in this slice
were not originally on #5152's list -- same bug class, found auditing the
C-14 #4367 backlog, now fixed and proven.

## Observability Evidence:

No-Observability-Change for signal names. Every retract keeps the existing
`WrapRetryableNeo4jError` classification and statement metadata
(`StatementMetadataPhaseKey` / `StatementMetadataEntityLabelKey` /
`StatementMetadataSummaryKey`); only the transaction-dispatch mechanism
changed for the three IAM writers (`ExecuteGroup` -> sequential `Execute`).
`SecretsIAMGraphWriter` had no code change. No metric name, span, log field,
queue stage, worker knob, or status field changes.
