# Evidence: retract-depth backfill for IMPORTS, HELM_VALUE_REFERENCE, ATLANTIS_DEPENDS_ON, USES_WORKFLOW, MANAGES, USES, USES_PROFILE (#4367)

## Owner/writer probe

| Type | Writer | Retract dispatch (before) | Status |
| --- | --- | --- | --- |
| `IMPORTS` | `CanonicalNodeWriter` "retract" phase (`canonicalNodeRefreshCurrentFileImportEdgesCypher`, `canonical_node_cypher.go`) | Whole "retract" phase is homogeneous `OperationCanonicalRetract`, so the NornicDB phase-group executor (`cmd/ingester/wiring_nornicdb_phase_group.go`, `executeSequentialRetractPhase`) already runs every statement as sequential per-statement autocommit `Execute`, never `ExecuteGroup` | Already safe. Live-claimed, no production fix. |
| `HELM_VALUE_REFERENCE` | `CanonicalNodeWriter` structural_edges phase (`canonical_helm_template_value_edges.go`) | Retract statement already `Drain: true` (fixed under #4476) | Already safe. Live-claimed, no production fix. |
| `MANAGES` | `CanonicalNodeWriter` structural_edges phase (`canonical_atlantis_edges.go`, `atlantisEdgeStatements`) | Retract statement NOT `Drain`-marked: ran grouped inside the mixed structural_edges `ExecuteGroup` transaction alongside the sibling MANAGES/DEPENDS_ON/USES_WORKFLOW MERGE upserts | **Bug.** Fixed: `Drain: true`. |
| `ATLANTIS_DEPENDS_ON` | same file, same function | same defect | **Bug.** Fixed: `Drain: true`. |
| `USES_WORKFLOW` | same file, same function | same defect | **Bug.** Fixed: `Drain: true`. |
| `USES` | `WorkloadCloudRelationshipWriter` (`workload_cloud_relationship_writer.go`), wired into the reducer canonical writers in `cmd/reducer/canonical_graph_writers.go` and `go/internal/reducer/workload_cloud_relationship_materialization.go` | `RetractWorkloadCloudRelationshipEdges` routed through `dispatch()`, the same grouping path the MERGE-shaped write uses | **Bug (#5152).** Fixed: added `dispatchRetract` (sequential `Execute`, never `ExecuteGroup`), mirroring `CodeInterprocEvidenceWriter.dispatchRetract`. |
| `USES_PROFILE` | `EC2UsesProfileEdgeWriter` (`ec2_uses_profile_edge_writer.go`), wired into the reducer's EC2 uses-profile edge projection | `RetractEC2UsesProfileEdges` routed through `dispatch()`, same grouping path as write | **Bug (#5152).** Fixed: added `dispatchRetract`, same fix shape as `USES`. |

All seven types are written by real production paths (no governance-gated or
unwired writer among them); all seven are claimed.

## Problems (measured on the pinned v1.1.11 before the fix)

1. **Atlantis structural retracts under-applied.** `atlantisEdgeStatements`
   built the MANAGES/ATLANTIS_DEPENDS_ON/USES_WORKFLOW retract statements
   without `Drain: true`, unlike the sibling Helm and GitLab structural edges
   (`retractHelmTemplateValueReferenceEdgesCypher`,
   `retractGitlabDefinesJobEdgesCypher`/`retractGitlabNeedsEdgesCypher`),
   which are already Drain-marked under #4476. Because the retract statements
   ran in the same mixed `structural_edges` phase as the sibling MERGE
   upserts, the NornicDB phase-group executor
   (`executeGroupedChunksWithDrain`) grouped them into one managed
   `ExecuteWrite` transaction, and an UNWIND relationship `DELETE` inside that
   transaction silently no-ops on commit (#4476). Live regression:
   `MANAGES gen2: stale "a"-targeted edges retracted: count = 1, want 0`.
2. **USES and USES_PROFILE retracts were no-ops even as single statements.**
   `WorkloadCloudRelationshipWriter.RetractWorkloadCloudRelationshipEdges` and
   `EC2UsesProfileEdgeWriter.RetractEC2UsesProfileEdges` both dispatched their
   single retract `DELETE` through `dispatch()`, which groups via
   `ExecuteGroup` whenever the executor implements `GroupExecutor` — the same
   path the writer's MERGE-shaped write correctly uses for throughput.
   Probed: on v1.1.11 a `DELETE` inside a managed transaction can under-apply
   even as a single statement (the same shape measured for `TAINT_FLOWS_TO`,
   the SQL-relationship retract, and the repo-dependency retract,
   #4367/#5128/#5146). Live regression:
   `retract: in-scope USES_PROFILE gone: count = 1, want 0`.
3. **IMPORTS and HELM_VALUE_REFERENCE were already safe.** No production
   change was needed for either: IMPORTS lives entirely inside the
   CanonicalNodeWriter all-retract "retract" phase, which the NornicDB
   phase-group executor already dispatches sequentially and autocommit
   (`executeSequentialRetractPhase`); HELM_VALUE_REFERENCE was already
   Drain-marked under #4476.

## Fixes

- `canonical_atlantis_edges.go`: `retractAtlantisManagesEdgesCypher`,
  `retractAtlantisDependsOnEdgesCypher`, and
  `retractAtlantisUsesWorkflowEdgesCypher` now carry `Drain: true`, so the
  NornicDB phase-group executor runs each as a standalone autocommit
  statement before the grouped MERGE upserts in the same
  `structural_edges` phase — the same shape already proven for the Helm and
  GitLab structural retracts.
- `ec2_uses_profile_edge_writer.go` and `workload_cloud_relationship_writer.go`:
  added `dispatchRetract` (sequential `Execute`, never `ExecuteGroup`) and
  routed `RetractEC2UsesProfileEdges`/`RetractWorkloadCloudRelationshipEdges`
  through it. The MERGE-shaped write path (`dispatch`) is unchanged and still
  groups for throughput.

## Review follow-up: all-retract phase with empty DrainVar crashed the drain loop (#5155)

Raised in review on #5155 after the Atlantis Drain fix landed: when a later
Atlantis generation keeps the project nodes but removes EVERY
dir/depends_on/workflow relationship, `atlantisEdgeStatements` emits ONLY the
three Drain-marked retract statements and no sibling MERGE upserts. The
structural_edges phase is then homogeneous `OperationCanonicalRetract`, so the
NornicDB phase-group executor routes it to `executeSequentialRetractPhase` —
not the mixed-phase `executeGroupedChunksWithDrain` path the Drain marking was
designed against. There, `Drain=true` with the production `drainReader` wired
entered `executeDrainLoop`, whose `BuildBoundedRetractDrainCypher` rejects the
empty `DrainVar` these bounded relationship retracts intentionally carry.
Failing-test error, verbatim:

```text
ExecutePhaseGroup() error = phase-group retract statement 1/3
(first_statement="UNWIND $source_uids AS uid | MATCH (p:AtlantisProject
{uid: uid})-[r:MANAGES]->(:Directory)"): build drain cypher: drainVar must
not be empty, want nil
```

The whole canonical write failed instead of retracting.

**Helm/GitLab audit:** every Drain-marked structural retract carries an empty
`DrainVar` — Atlantis (x3), Helm (`canonical_helm_template_value_edges.go`,
x2), and GitLab (`canonical_gitlab_edges.go`, x2) — so all three families
shared the latent bug. GitLab can even produce the all-retract phase on its
own: a `.gitlab-ci.yml` with GitlabJob entities, no GitlabPipeline entity,
and no `needs` emits only the Drain-marked NEEDS retract. Helm cannot alone
(its retracts are only emitted alongside the MERGE upsert), but any
combination that leaves the phase all-retract would hit it.

**Fix layer: the executor.** In
`cmd/ingester/wiring_nornicdb_phase_group.go`, both `executeDrainLoop` call
sites (`executeSequentialRetractPhase` and `executeEntityPhaseGroup`) now
route a `Drain=true` statement with an empty `DrainVar` through
`executeAutocommitRetract` — one plain autocommit statement, exactly the
semantics Drain buys in the mixed-phase path — and only DrainVar-carrying
statements (unbounded full-refresh `DETACH DELETE`) enter the bounded LIMIT
drain loop. The executor fix makes empty-DrainVar semantically valid
everywhere and repairs all three statement families at once; a statement-side
fix (adding DrainVars or un-marking Drain in all-retract phases) would have
had to touch three builders and would have left the executor contract
inconsistent between the mixed and all-retract paths. The bootstrap-index
executor (`cmd/bootstrap-index/nornicdb_wiring.go`) was audited and is not
affected: its `executeSequentialRetractPhase` has no drain loop — every
retract already runs as chunked autocommit `Execute`.

Regression tests (`go/cmd/ingester/wiring_nornicdb_all_retract_drain_test.go`):

- `TestNornicDBPhaseGroupExecutorAllRetractPhaseRunsEmptyDrainVarAutocommit`
  (the failing-then-green repro above; asserts one autocommit RunWrite per
  retract, zero ExecuteGroup calls)
- `TestNornicDBPhaseGroupExecutorAllRetractPhaseEmptyDrainVarNilReader`
  (nil drainReader still runs each retract ungrouped via inner Execute)
- `TestNornicDBPhaseGroupExecutorAllRetractPhaseKeepsDrainLoopForDrainVar`
  (a DrainVar-carrying full-refresh retract still takes the bounded drain
  loop, not the single-shot route)

Both live tests re-proven GREEN 3/3 against a fresh v1.1.11 container after
the executor fix, and the full `verify-replay-tier.sh` gate PASSED.

## Benchmark Evidence:

Failing-then-green live regressions on the pinned production backend
(behavior change — the old paths returned wrong graph truth):

```bash
cd go
ESHU_REPLAY_TIER_LIVE=1 ESHU_GRAPH_BACKEND=nornicdb ESHU_NEO4J_DATABASE=nornic \
NEO4J_URI=bolt://localhost:17696 NEO4J_USERNAME=neo4j NEO4J_PASSWORD=change-me \
go test ./internal/replay/offlinetier/ -run 'TestReducerCanonicalGovernanceEdgeRetractGraphTruth|TestReducerWorkloadUsesEdgeRetractGraphTruth' -count=1 -v
```

- RED (Atlantis, `canonical_atlantis_edges.go` reverted to the unmarked
  statements): `MANAGES gen2: stale "a"-targeted edges retracted: count = 1,
  want 0`.
- RED (workload/uses, both writers reverted to `dispatch()`):
  `retract: in-scope USES_PROFILE gone: count = 1, want 0`.
- GREEN: `ok` 3/3 runs (~0.9-1.1s package wall per run) after all fixes, for
  both live tests. `TestReducerCanonicalGovernanceEdgeRetractGraphTruth`
  writes and retracts IMPORTS, HELM_VALUE_REFERENCE, MANAGES,
  ATLANTIS_DEPENDS_ON, and USES_WORKFLOW across two generations for an
  in-scope repository (old target gone, new target present, both old and new
  endpoint nodes survive) plus an out-of-scope repository written once and
  never revisited (its edges survive untouched).
  `TestReducerWorkloadUsesEdgeRetractGraphTruth` writes and retracts USES and
  USES_PROFILE for an in-scope evidence scope plus an out-of-scope survivor
  scope, and asserts every endpoint node (CloudResource, Workload,
  WorkloadInstance) survives the retract.

Unit-level regressions (no backend required):

```bash
cd go
go test ./internal/storage/cypher/ -run 'TestAtlantisEdgeStatementsRetractsStaleEdgesBeforeMerge|TestEC2UsesProfileEdgeWriterRetractRoutesThroughAutocommitExecute|TestWorkloadCloudRelationshipWriterRetractRoutesThroughAutocommitExecute|TestEC2UsesProfileEdgeWriterWriteRoutesThroughExecuteGroup|TestWorkloadCloudRelationshipWriterWriteRoutesThroughExecuteGroup' -count=1 -v
```

Cost shape: every retract stays a bounded, fixed number of statements per
write (three Atlantis statements, one USES statement, one USES_PROFILE
statement); the prior grouped paths were buying their single transaction by
not deleting edges, so there is no correct faster baseline to regress
against. The write paths (`dispatch`, the MERGE-shaped upserts) are
unchanged and still batch/group for throughput; only the retract dispatch
moved to per-statement autocommit.

## Observability Evidence:

No-Observability-Change. The retract statements keep their operations,
parameters, and the existing canonical/reducer write spans and graph-write
failure/retry telemetry (`WrapRetryableNeo4jError` still wraps every
dispatch/dispatchRetract error path). No metric name, span, log field, queue
stage, worker knob, or status field changes.

## Exclusions

None. All seven requested types (`IMPORTS`, `HELM_VALUE_REFERENCE`,
`ATLANTIS_DEPENDS_ON`, `USES_WORKFLOW`, `MANAGES`, `USES`, `USES_PROFILE`) are
written by real production writers wired into the reducer/projector runtime
(none are governance-gated or unwired), so all seven are claimed in
`specs/replay-coverage-manifest.v1.yaml`.
