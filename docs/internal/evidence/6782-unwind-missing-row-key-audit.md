# #6782: audit of UNWIND writers for the missing-row-key shape

The B-7 run on Neo4j found that NornicDB v1.3.3 stores the literal expression
text when an `UNWIND $rows AS row` statement reads `row.<key>` and the row map
has no such key. Neo4j reads the key as null. The repo-dependency writer was
fixed first; see [the divergence note](6782-b7-neo4j-divergences.md#2-mcpget_repo_context-lacked-source_tool_breakdown-on-neo4j).
This note covers every other production writer: which ones had the shape,
what changed, and the proof.

Source: branch `gate/6782-backend-differential-oracle`. The audit ran at
`128942891` and the fix is in `4bc72e9c0`. The live proof used the same pins as the divergence note:
`timothyswt/nornicdb-cpu-bge:v1.3.3@sha256:81cedbf48898f4c37d05c325fee76b6d797b43e290e3a8a4e9eea936f0ec827f`
and `neo4j:2026-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f`,
each in a fresh container with Eshu's schema applied by
`graph.EnsureSchemaWithBackend`.

## Method

1. Listed every non-test Go file with an `UNWIND $<param> AS <var>` statement
   (91 files). Only the `row`, `pair` and `candidate` bindings are lists of
   maps; the others (`uid`, `repo_id`, `entity_id`, `candidate_key`,
   `file_path`, and so on) are scalar lists with no keys, so the shape cannot
   occur there.
2. For each map binding, collected the `<var>.<key>` references in the
   statement text.
3. Found every place a referenced key is set conditionally. A scan listed each
   `m["key"] = ...` that sits inside an `if`, `else`, `case` or `for` block in
   `internal/storage`, `internal/reducer`, `internal/projector`,
   `internal/graph`, `internal/ifa` and `cmd`, where `key` is a referenced row
   key. Separate searches found no `delete(row, "key")`, no omitempty-to-map
   conversion, and no intent payload passed straight through as a row.
4. Writers whose rows are built in another package (most reducer-owned
   writers) were traced to their builder and checked the same way.
5. Traced production reachability for every hit through `cmd/reducer`.

## Findings

| Writer (statement) | Referenced keys that could be omitted | Omission site | Omitted-key risk | Reached on NornicDB |
| --- | --- | --- | --- | --- |
| `EdgeWriter` repo dependency: DEPENDS_ON, typed verbs, RUNS_ON (`canonical_relationships.go`) | `source_tool`, `evidence_type` | `edge_writer.go` `buildRowMap` | Yes. Fixed earlier in `3ccecbfea` | Yes: `cmd/reducer/main.go` `RepoDependencyEdgeWriter: edgeWriterForHandlers` |
| `EdgeWriter` EvidenceArtifact upsert (`canonical_relationships.go:275` `batchCanonicalRepoEvidenceArtifactUpsertCypher`, and its `WithEnvironment` variant) | `start_line`, `end_line`, `commit_sha`, `ref_value`, `ref_pinned` | `edge_writer_row_metadata.go:115-135` (`if sl > 0`, `if sha != ""`, `if refValue != ""`) | **Yes** | Yes: `edge_writer.go:196` routes artifacts inside the repo-dependency write |
| `EdgeWriter` code calls: CALLS (both templates), REFERENCES, INSTANTIATES (`canonical_code_call_edges.go:67`, `canonical_instantiates_edges.go:11`, `edge_writer_code_call_labels.go:153-178`) | `call_kind`; also `source_entity_id` and `target_entity_id` in the unlabelled CALLS `coalesce()` anchor | `edge_writer_code_call_labels.go:94` (`if callKind != ""`) | **Yes** for `call_kind`. The `coalesce()` keys are harmless today because `caller_entity_id` is always set; nil is sent anyway | Yes: `cmd/reducer/main.go:450` `CodeCallProjectionRunner` writes through the shared `EdgeWriter` |
| `EdgeWriter` PINS_SUBMODULE (`canonical_submodule_edges.go:29`) | `pinned_sha` | `canonical_submodule_edges.go:77` (`if pinnedSHA != ""`) | **Yes** | Yes: `cmd/reducer/main.go:295` `SubmodulePinEdgeWriter: edgeWriterForHandlers` |
| `CodeInterprocEvidenceWriter` TAINT_FLOWS_TO (`code_interproc_evidence_writer.go:25`) | `why_trail_json`, `why_trail_truncated` | `reducer/code/taint/interproc_evidence_rows.go:66-71` (`if len(in.WhyTrail) > 0`, `if in.WhyTrailTruncated`) | **Yes** | Yes: `cmd/reducer/canonical_graph_writers.go:110`, used by `wiring_handlers.go:247,255` |
| Semantic entity upserts (`semantic_entity_statements.go`, 14 statements) | 33 optional metadata keys | `semantic_entity_rows.go:185-287` | No: `semanticEntityEnsureNullableKeys` backfills every one with nil | n/a |
| Canonical node upserts (`canonical_node_cypher.go`) | none: optional metadata reaches the node through `SET n += row.props` | `canonical_node_writer_metadata.go:111-123` builds the props map | No: a missing key in a merged map writes nothing | n/a |
| Provenance PUBLISHES (`provenance_edge_writer.go:105,119`) | `package_id` or `version_id` | `reducer/packages/correlation/provenance_edges.go:59-61` | No: rows are routed by key presence to the statement that reads that key | n/a |
| Cloud-resource service anchor fields (`cloud_resource_node_writer.go`) | `workload_id`, `service_name`, `service_anchor_*` | `reducer/aws_resource_service_anchor.go:109-116` | No: `applyCloudResourceServiceAnchorAbsentFields` presets every key first | n/a |
| Single-row canonical code edge `BuildCanonicalCodeCallUpsert` (`canonical.go:126-148`), not an UNWIND writer | top-level `$call_kind` | `canonical_retract.go` (`if p.CallKind != ""`) | Yes on Neo4j, which rejects a missing `$param`. It now sends nil when absent; `TestBuildCanonicalCodeCallUpsertSendsEveryReferencedParam` guards every template | No: the builder has no non-test caller |
| Every other map-binding writer: `canonical.go`, `canonical_atlantis_edges.go`, `canonical_codeowners_edges.go`, `canonical_deployable_unit_edges.go`, `canonical_documentation_edges.go`, `canonical_flux_edges.go`, `canonical_gitlab_edges.go`, `canonical_handles_route_edges.go`, `canonical_helm_template_value_edges.go`, `canonical_implements_edges.go`, `canonical_inheritance_edges.go`, `canonical_invokes_cloud_action_edges.go`, `canonical_kustomize_edges.go`, `canonical_rationale_edges.go`, `canonical_runs_in_edges.go`, `edge_writer_inheritance_labels.go`, `edge_writer_shell_exec.go`, `edge_writer_sql.go`, `writer.go`, the cloud (AWS, GCP, Azure), container-image, crossplane, derived-from, EC2, IAM, incident-routing, Kubernetes, observability, OCI, package-registry, RDS, S3, secrets-IAM, security-group, code-taint, tfstate and workload-cloud writers in `internal/storage/cypher`, the reducer materializers (`workload_materializer.go`, `workload_materialization_repo_phase.go`, `workload_materializer_retract_instances.go`, `infrastructure_platform_materializer.go`), `crossplane_satisfied_by_edge_existence.go`, `cloud_sink_loader.go`, `internal/graph/batch.go`, and `cmd/{bootstrap-index,ingester,projector}/terraform_state_config_match.go` | all referenced keys | Row maps are map literals that always set every key, or a `cloneRowWith` overlay that sets its keys on every row | No | n/a |

The supply-chain writer, crossrepo intent payloads, and code-call intent rows
also set keys conditionally. They build Postgres JSON or intent payloads, not
graph rows, and the graph writer reads them through `payloadString` and
friends, which return a value for an absent key. They are out of scope.

## What each defect did on NornicDB

- EvidenceArtifact: every artifact that is not a GitHub Actions `@ref`
  (Helm, Argo CD, Kustomize and so on) got `ref_value = "row.ref_value"` and
  `ref_pinned = "row.ref_pinned"`. `repository/deployment_evidence.go`
  returns those properties, and `copyOptionalDeploymentEvidenceFields` copies
  any non-empty `ref_value` into the response along with
  `ref_pinned = BoolVal("row.ref_pinned")`, which is `false`. The API
  therefore reported a mutable, unpinned ref on evidence that has no ref.
  `commit_sha = "row.commit_sha"` was copied into the citation the same way.
  `start_line` and `end_line` were stored as junk but dropped on read, because
  `FirstPositiveInt` rejects a string.
- Code calls: every CALLS, REFERENCES or INSTANTIATES edge whose parser row
  had no `call_kind` got `call_kind = "row.call_kind"`. The code relationship
  and story reads return `rel.call_kind` as-is.
- PINS_SUBMODULE: a submodule without a gitlink got
  `pinned_sha = "row.pinned_sha"`.
- TAINT_FLOWS_TO: a finding without a why-trail got
  `why_trail_json = "row.why_trail_json"`. The relationship story's JSON
  decode fails on it and returns no trail, so the junk is stored but not
  shown.

## Fix

Each builder now sends every key its statement reads, with nil when there is
no value (`setOptionalRowString` or an explicit nil). The interproc writer
adds the two why-trail keys in the writer. That covers every caller,
including the value-flow fixpoint projector. nil leaves the property absent on
Neo4j and null-valued on NornicDB, so `IS NOT NULL` agrees on both. It is
never an empty string, which readers would treat as a value.

Re-projection repairs data already written. `SET x = null` over an existing
junk token clears it on NornicDB; the live test below proves this. A graph
projected before the fix keeps the junk until each element is re-projected.

## Guard

`assertUnwindRowsCarryReferencedKeys` (in
`go/internal/storage/cypher/unwind_row_keys_helper_test.go`) takes recorded
statements, finds each `UNWIND $<param> AS <var>` over a list of maps, and
fails if any row lacks a `<var>.<key>` the statement reads. Any writer test
that records its statements can call it. Its own seeded-violation pair is
`TestUnwindRowKeyViolationsSeeded`. It reports a planted row with no
`source_tool`, and it passes the same row with an explicit nil, a scalar
UNWIND, and a non-`row` binding.

`TestEdgeWriterSparseRowsCarryEveryReferencedKey` drives every domain that
`EdgeWriter.buildRowMap` routes with payloads that carry only the required
identity keys, and runs the guard on every statement. A new optional key on
any shared-edge route fails it unless the builder sends the key.

## Proof

RED/GREEN, unit (`go test ./internal/storage/cypher -count=1`):

- Before the fix, `TestEdgeWriterSparseRowsCarryEveryReferencedKey` failed on
  the repo-dependency artifact rows (5 keys), the code-call routes
  (`call_kind` on all six, plus the `coalesce()` keys) and PINS_SUBMODULE.
  `TestEdgeWriterSparseOptionalKeysAreNil` and
  `TestCodeInterprocEvidenceRowsCarryEveryReferencedKey` also failed.
  All pass after the fix, as does the whole package.
- `TestBuildSubmodulePinRowMapOmitsUnknownPinnedSHA` and
  `TestRepoEvidenceArtifactRowsFromIntentOmitsRefFieldsWhenAbsent` pinned the
  old omit-the-key contract. They now pin an explicit nil, and are renamed
  `...SendsNilForUnknownPinnedSHA` and `...NilsRefFieldsWhenAbsent`.

RED/GREEN, live (`TestLiveSparseWriterRowsLeaveOptionalPropertiesNull`, tag
`live_nornicdb_answer_truth`). The test drives the production `EdgeWriter`
and `CodeInterprocEvidenceWriter` and reads 9 optional properties back. One
CALLS edge is pre-seeded with `call_kind = 'row.call_kind'` to prove the
re-projection clears it.

- NornicDB with the four pre-fix writer files restored from `128942891`: all
  9 checks failed. Each property held its literal text, for example
  `call_kind = "row.call_kind"`, `ref_value = "row.ref_value"`,
  `pinned_sha = "row.pinned_sha"`, `why_trail_json = "row.why_trail_json"`.
- NornicDB with the fix: all 9 null, including the re-projected junk edge.
- Neo4j with the fix: all 9 null.
  `TestLiveRepoDependencyWithoutSourceToolStaysUnstamped` also still passes on
  both backends.

No-Regression Evidence: the change adds at most five map keys with nil
values to each row on the affected routes. It changes no Cypher text, batch
size, statement grouping, `MERGE` key, or transaction boundary. The write
cost per row does not change in kind, since the statement already evaluated
each `SET` for every row. No latency or throughput claim is made. The live
test above ran the same statements at the same row shape before and after.

No-Observability-Change: no metric, span, or log field changes. The writers
keep their existing statement summaries, grouped-write telemetry, and
`logSharedEdgeWrite` logging. Operators can find a stale junk token with
`MATCH ()-[r]->() WHERE r.call_kind STARTS WITH 'row.' RETURN count(r)`
(and the same predicate on the other properties); the count falls as elements
are re-projected.

## Open

- Graphs projected on NornicDB before this fix keep the junk values until each
  edge or artifact is re-projected. There is no one-shot repair.
- The unit guard covers the `EdgeWriter` domains and the interproc writer. The
  other writers were audited by hand. A writer test elsewhere can adopt
  `assertUnwindRowsCarryReferencedKeys` when it records statements. As a
  backstop, B-7 now fails when any node or edge in the projected corpus holds
  an unresolved `row.<key>` token (`unresolved_row_tokens`,
  `go/cmd/golden-corpus-gate/graph_row_tokens.go`). It matches a token when
  its key is one the Go write path reads, a snake_case key, or the
  property's own name. `go/internal/goldengate/row_keys_test.go` derives the
  plain keys from every non-test Go string literal under `go/internal`,
  `go/cmd`, and `go/pkg` and fails on drift, and it fails when any
  `UNWIND` binding other than `row` is dereferenced as a map outside a
  reviewed read-only list. So the check covers every `UNWIND ... AS row`
  writer in those trees, under any property name, for writers the corpus
  exercises. Cypher outside those Go literals is not covered. The first
  version (f9b6b189d) matched only a key equal to the property or a
  snake_case key, which missed plain keys stored under another name, such as
  `s3_internet_exposure_state = row.state` (review finding F-6).
