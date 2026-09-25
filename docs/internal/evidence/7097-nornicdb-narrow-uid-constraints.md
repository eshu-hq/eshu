# NornicDB uniqueness constraints narrower than the canonical uid (#7097)

The #7095 class also existed on NornicDB. The canonical writer MERGEs every
entity label on `uid`, which hashes repo, relative path, entity type, name and
start line, and it upserts `entities` before `entity_retract` deletes the prior
generation's nodes. NornicDB never created the composite `tf_module_unique` and
`helm_chart_unique` (`nornicDBSchemaConstraint` drops the composite form), but
it still created and enforced the single-property `kustomize_unique`,
`helm_values_unique` and `tg_config_unique` on `path`. A moved
KustomizeOverlay, HelmValues or TerragruntConfig block collided with its own
previous node.

NornicDB reports the violation as `Neo.TransientError.Transaction.Outdated`, so
the work item retried instead of dead-lettering. It shows up as a stuck,
repeatedly retrying projection, not as a dead letter.

The NornicDB schema now drops those three constraints before any other
statement (`DROP CONSTRAINT <name> IF EXISTS`), through
`nornicDBRetiredUniqueConstraints`. Uniqueness for the three labels rests on
`<label>_uid_unique`, as it already did for TerraformModule and HelmChart.

## Proof

All proof ran against `ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-500-e022384c`
(digest `sha256:74a8ed7b…`, the repo pin) in a container started with the
`docker-compose.live-backend-nornicdb.yml` environment.

- Live regression `TestLiveNornicDBMovedCanonicalBlockKeepsOneNode`
  (`go/internal/storage/cypher/canonical_moved_block_nornicdb_live_test.go`). It
  seeds the three legacy constraints, applies the real schema bootstrap, then
  drives the real `CanonicalNodeWriter` through generation 1 (block at line 726)
  and a delta that moves it to line 755, for each of TerraformModule, HelmChart,
  HelmValues, KustomizeOverlay and TerragruntConfig.
  - Base: the bootstrap leaves all three constraints in place, and HelmValues,
    KustomizeOverlay and TerragruntConfig fail after 30 s of retries with
    `Neo.TransientError.Transaction.Outdated (commit failed: constraint
    violation: Constraint violation (UNIQUE on <Label>.[path]): Node with
    path=… already exists)`. TerraformModule and HelmChart pass.
  - This change: the three constraints are dropped and every label is left with
    exactly one node, the new uid, line 755, generation 2 (fresh store 9.5 s,
    warm 2.0 s).
- Guard `TestUniqueConstraintsDoNotNarrowCanonicalUIDIdentity`
  (`go/internal/graph/schema_uid_identity_test.go`), now run for both backends.
  It is RED on the NornicDB base with exactly `helm_values_unique(HelmValues)`,
  `kustomize_unique(KustomizeOverlay)` and `tg_config_unique(TerragruntConfig)`,
  and GREEN here. `TestNarrowUIDIdentityConstraintsSeededViolation` still
  proves the checker flags a planted narrow key.
- `DROP CONSTRAINT helm_values_unique IF EXISTS` on that image removes the
  constraint: `SHOW CONSTRAINTS` listed `helm_values_unique` (UNIQUE, NODE,
  `HelmValues`, `path`) before and an empty result after. A second drop of the
  same name and a drop of a name that never existed both succeed.
- Adoption regressions in `go/cmd/bootstrap-data-plane`:
  `TestRunDoesNotAdoptNornicDBSchemaThatStillHasRetiredConstraints` and
  `TestNornicDBRetiredConstraintDropsReachTheBackendOnPartialAdoption`. The
  second proves the missing-object filter forwards the DROPs and skips every
  CREATE the store already has.

## Performance

No-Regression Evidence: no path index is added, and the drops cost nothing on the
write path. The delta entity retract shape
`MATCH (n:HelmValues) WHERE n.repo_id = $r AND n.evidence_source = 'projector/canonical' AND n.path IN [2 paths] AND n.generation_id <> $g`
ran on the pinned NornicDB with 50,000 HelmValues nodes across 2,000 repos, 15
distinct parameter sets per state (parameters varied so the result cache cannot
answer; a repeat of identical parameters returned in 0.9 ms, which is why every
figure below uses fresh ones).

| State | Median |
| --- | --- |
| `helm_values_unique` present, no path index | 234 ms |
| Constraint dropped, no path index | 205-207 ms |
| Constraint dropped, `helm_values_path` index present | 208-210 ms |
| `path IN [1]` alone, no index / with index | 203 ms / 203 ms |
| `path = $p` alone, with index | 208 ms |
| `uid = $u` (existing `helm_values_uid_lookup`) | 0.8 ms |

NornicDB does not seek a `path` index or a `path` uniqueness constraint for
this filter in any state: every variant is a label scan, and only the `uid`
index is seeked. So dropping the constraint changes no plan (the retract was a
label scan before and still is; that is a pre-existing gap, not a regression),
and a replacement path index would add write cost and a backfill without
serving a read. No query in the repo anchors HelmValues, KustomizeOverlay or
TerragruntConfig on `path` other than this retract. The Neo4j fix added path
indexes because Neo4j did seek them (#7095); that reasoning does not carry
over. Entity upserts MERGE on `uid` and keep seeking `<label>_uid_unique`
and the `*_uid_lookup` index. The container ran the amd64
image under emulation on an arm64 host, so absolute times are inflated; the
comparison across states is what matters.

Observability Evidence: the schema bootstrap logs each new statement through
the existing `graph schema statement applying` and `graph schema statement applied`
lines with the new `schema_phase` value `nornicdb_retired_constraint_drops`,
plus the existing `statement_index` and `statement_total`. The canonical
writer's spans, metrics and logs are unchanged.

## Rollout

The NornicDB fingerprint moves from `f957752d…` to `8dfe89b1…`. The bump is
additive: it drops constraints and adds no statement, no MERGE or MATCH
identity changes, and the previous fingerprint stays in the marker's compatible
list. A writer still on the previous release therefore keeps writing during a
rolling upgrade, and writes the same graph.

The first `eshu-bootstrap-data-plane` run against an existing store sees a
marker that no longer matches and falls to existing-schema adoption. With the
default opportunistic adoption for NornicDB, adoption finds the retired
constraints still present, declines to adopt, and the missing-object filter
forwards only the three DROPs. Every other object already exists and is skipped
before it reaches the backend, so no property index is rebuilt. With
`ESHU_GRAPH_SCHEMA_ADOPT_EXISTING=false` the whole DDL pass re-runs, and every
`CREATE INDEX IF NOT EXISTS` re-runs on the populated graph, which can
re-backfill (see `nornicdb-pitfalls.md`); leave the variable unset for this
upgrade.

A rollback to the previous release inside the compatible window skips graph
DDL, so the three constraints stay dropped. Forcing the older DDL
(`ESHU_GRAPH_SCHEMA_FORCE_REAPPLY`, a missing marker, or a release older than
the window) would re-create them, and creating a unique constraint on a
populated NornicDB graph refreshes unique values; do not do it as part of a
rollback.
