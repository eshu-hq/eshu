# Neo4j uniqueness constraints narrower than the canonical uid (#7095)

The canonical writer MERGEs every entity label on `uid`, and `uid` hashes repo,
relative path, entity type, name, and start line. It upserts `entities` before
`entity_retract` deletes the prior generation's nodes. On Neo4j,
`tf_module_unique (name, path)`, `helm_chart_unique (name, path)`,
`helm_values_unique (path)`, `kustomize_unique (path)` and
`tg_config_unique (path)` saw the moved block's new uid next to the old node and
failed the delta with `Neo.ClientError.Schema.ConstraintValidationFailed`. That
dead-lettered the repository on ops-qa.

The Neo4j schema now drops those five constraints before any other statement
(`DROP CONSTRAINT <name> IF EXISTS`). It adds non-unique `path` RANGE indexes
`kustomize_overlay_path`, `helm_values_path` and `terragrunt_config_path`.
Uniqueness for the five labels rests on `<label>_uid_unique`. NornicDB output is
byte-identical: its statement list is unchanged and its fingerprint stays
`f957752df4f6114440959c6a48162d7a192e98c724918abfde897b8fd67ecef4`. The Neo4j
fingerprint moves from `dc9d1cfb57e5…6b74` to `675dafc901ff…8d54`, and the
predecessor stays compatible.

## Proof

- Guard `TestNeo4jUniqueConstraintsDoNotNarrowCanonicalUIDIdentity`
  (`go/internal/graph/schema_uid_identity_test.go`). It is RED on the base with
  exactly the five labels and GREEN on this change. Its seeded-violation
  companion `TestNarrowUIDIdentityConstraintsSeededViolation` flags a planted
  `(name, path)` and `(path)` key and passes `{uid}` and superset keys.
- Live `TestLiveNeo4jMovedCanonicalBlockKeepsOneNode`
  (`go/internal/storage/cypher/canonical_moved_block_live_test.go`,
  neo4j:2026-community@sha256:eabfbb04, 2026.08.1). It seeds the legacy
  constraints and applies the real schema bootstrap. The real
  `CanonicalNodeWriter` then writes generation 1 with the block at line 726 and
  a delta that moves it to 755. On the base, the five constraints survive the
  bootstrap and both TerraformModule and HelmValues fail with
  `ConstraintValidationFailed`. On this change, the constraints are dropped and
  each label is left with exactly one node: the new uid, line 755, and
  generation 2.

## Performance

No-Regression Evidence: PROFILE of the canonical delta entity retract shape
`MATCH (n:<Label>) WHERE n.repo_id = $r AND n.evidence_source = 'projector/canonical' AND n.path IN [2 paths] AND n.generation_id <> $g`.
It ran on neo4j:2026-community 2026.08.1 with 5,000 nodes per label across 50
repos, first under the base schema and then under the new schema on the same
store.

| Label | Base plan / DB hits | New plan / DB hits |
| --- | --- | --- |
| TerraformModule | evidence_source index seek, 10103 | same, 10103 |
| HelmChart | label scan, 15103 | same, 15103 |
| HelmValues | `NodeUniqueIndexSeek helm_values_unique`, 10 | `NodeIndexSeek helm_values_path`, 10 |
| KustomizeOverlay | `NodeUniqueIndexSeek kustomize_unique`, 10 | `NodeIndexSeek kustomize_overlay_path`, 10 |
| TerragruntConfig | `NodeUniqueIndexSeek tg_config_unique`, 10 | `NodeIndexSeek terragrunt_config_path`, 10 |

The composite `(name, path)` constraints on TerraformModule and HelmChart
served no read in the base plans, and no query filters those labels on name
and path together, so they get no replacement index. The HelmChart delta
retract was a label scan before this change and still is. That is a
pre-existing gap, not a regression. Entity upserts MERGE on `uid` and keep
seeking `<label>_uid_unique`. TerraformModule and HelmChart writes now update
one fewer index. The three path-keyed labels swap a unique index for a
non-unique one, so their writes update the same number of indexes. Bootstrap adds five idempotent DROPs and three CREATE
INDEX statements.

No-Observability-Change: the schema bootstrap logs each new statement through
the existing `graph schema statement applying` and `graph schema statement applied`
lines. It uses the new `schema_phase` values `neo4j_retired_constraint_drops`
and `neo4j_retired_constraint_path_indexes`, plus the existing `statement_index`
and `statement_total`. The canonical writer's spans, metrics and logs are
unchanged.

## Rollback

An older release's bootstrap re-creates the dropped constraints.
`kustomize_unique`, `helm_values_unique` and `tg_config_unique` then fail with
`Neo.ClientError.Schema.IndexAlreadyExists` because the new `path` index covers
the same schema. That was observed while rerunning the live test before it
dropped the indexes in setup. The strict bootstrap stops, so drop the three
`path` indexes before rolling back past this change.

## Open: NornicDB

NornicDB drops only the composite `REQUIRE (...) IS UNIQUE` form, so it still
enforces the single-property `kustomize_unique`, `helm_values_unique` and
`tg_config_unique`. A throwaway probe ran the same two-generation write against
the pinned `nornicdb-amd64-cpu:fix-500-e022384c`. TerraformModule passed.
HelmValues, KustomizeOverlay and TerragruntConfig failed with
`Neo.TransientError.Transaction.Outdated (commit failed: constraint violation: UNIQUE on <Label>.[path])`
after retrying for 30s. This change leaves NornicDB untouched, and the owner
decides the follow-up.
