# AGENTS.md — internal/projector guidance for LLM assistants

## Read first

1. `go/internal/projector/README.md` — pipeline position, lifecycle, exported
   surface, and operational notes
2. `go/internal/projector/service.go` — `Service.Run`, the poll-and-dispatch
   loop; understand `processWork` before touching concurrency
3. `go/internal/projector/runtime.go` — `Runtime.Project`; the four write
   stages and their ordering
4. `go/internal/projector/runtime_phase.go` — canonical phase publication and
   repair enqueue behavior
5. `go/internal/projector/canonical.go` and `canonical_builder.go` — the
   `CanonicalMaterialization` shape and how it is built from facts. Read
   `tfstate_canonical.go` when touching Terraform-state projection and
   `oci_registry_canonical.go` when touching OCI registry projection.
6. `go/internal/projector/runtime_logging.go` — stage log attributes and
   operator-facing runtime stage messages
7. `go/internal/telemetry/instruments.go` and `contract.go` — metric and span
   names before adding new telemetry

## Invariants this package enforces

- **Idempotency** — every write path must converge on the same graph truth on
  retries. `doc.go` states this as a package invariant; `runtime_retry_test.go`
  tests it.
- **Phase publish before ack** — `publishCanonicalGraphPhases` in
  `runtime_phase.go` must succeed before the work item acks. If publish fails
  and `RepairQueue` is non-nil, a repair row is enqueued.
- **Module/Parameter exclusion from generic entity phase** — `Module` and
  `Parameter` labels are skipped in `extractEntities` because they use different
  graph MERGE keys. Enforced at `canonical_builder.go:227-229`.
- **Repo-qualified paths** — `FileRow.Path` and `EntityRow.FilePath` are set to
  `repoPath/relative_path` to avoid cross-repo MERGE collisions. Enforced in
  `extractFiles` and `extractEntities` via `qualifyPath`.
- **Terraform-state facts stay source-local** — `tfstate_canonical.go` projects
  committed Terraform-state facts into canonical resource/module/output rows
  without cloud joins. Cross-source AWS matching belongs in reducer domains
  after the Terraform-state readiness checkpoints publish.
- **OCI digest identity stays source-local** — `oci_registry_canonical.go`
  projects committed OCI registry facts into digest-keyed image rows. Tags are
  mutable weak evidence and must not become the canonical image key.
- **Typed-payload decode with per-fact quarantine** — the OCI canonical
  extractor (`oci_registry_canonical.go`) decodes each fact through the
  `sdk/go/factschema` seam (`oci_registry_factschema.go`), NOT raw
  `payloadString`. A fact missing a required identity field is QUARANTINED
  per-fact via `partitionProjectorDecodeFailures` (`factschema_quarantine.go`) and
  recorded as a visible `input_invalid` dead-letter
  (`eshu_dp_projector_input_invalid_facts_total` + a structured error log) by
  `recordProjectorQuarantinedFacts`, while every valid fact — OCI and non-OCI —
  still projects and the whole-repo build never fails. This is the FIRST
  factschema decode seam in the projector; the quarantine apparatus is generic
  (family-neutral) so the terraform_state extractor reuses it verbatim. NEVER
  make a malformed fact fail the whole `buildCanonicalMaterialization` — that
  would drop every valid file/entity/package for the repo. NEVER emit a
  zero-value row. A present-but-empty required field is a VALID decode the row
  builder's own identity gate still drops (byte-identical to pre-typing);
  only an ABSENT/null required key dead-letters.
- **Package identity stays source-local** — `package_registry_canonical.go`
  projects committed package, package-version, and package-dependency facts into
  package identity rows and package-native dependency rows. Source hints are
  provenance only; do not create repository ownership, publication, or
  consumption truth in the projector.
  `packagesource/correlation_intents.go` may enqueue the reducer classifier,
  but that intent is counter-only until reducer admission grows stronger
  provenance. The five consumed kinds (`package`, `.package_version`,
  `.package_dependency`, — since #5458 — `.package_artifact`, and — also
  since #5458 — `.registry_event`) decode through the `sdk/go/factschema`
  seam (`factschema_decode_packageregistry.go`), NOT raw `payloadString`/
  `payloadBoolPtr`/`payloadStringSlice`, reusing the family-neutral
  quarantine apparatus `oci_registry`/`terraform_state` introduced: a fact
  missing a required identity field (`package_id`, `version_id`, `version`,
  `dependency_package_id`, `artifact_key`, `event_key`, `event_type`) is
  quarantined per-fact via `partitionProjectorDecodeFailures` and recorded as a
  visible `input_invalid` dead-letter
  (`eshu_dp_projector_input_invalid_facts_total` under the
  `package_registry_canonical` stage). `.package_artifact` projects onto a
  `PackageArtifact` node carrying the per-artifact `hashes` (algorithm:digest
  pairs) the `PackageVersion` node's `checksum_algorithms` property drops.
  `hashes` accepts any string key, including one containing `:`, matching the
  v1 JSON Schema's unconstrained `hashes.additionalProperties` exactly —
  decode does not reject any key (an earlier version rejected a colon-bearing
  algorithm name, which silently narrowed the public v1 contract without a
  major bump; #5820 P2 review finding). The graph writer's `algorithm:digest`
  flattening (`packageRegistryHashPairs`,
  `go/internal/storage/cypher/package_registry_artifact_writer.go`) escapes
  both segments instead, keeping the split unambiguous for any input.
  `packageRegistryTrimmedStringMap` (shared with `.package_version`'s
  `checksums`) also sorts its input keys before iterating so a whitespace
  collision between two distinct original keys resolves deterministically —
  merging when they agree on the value, dead-lettering as `input_invalid`
  when they disagree — instead of depending on Go's randomized map iteration
  order (#5820 P2 review finding). `PackageRegistryArtifactRow` and its
  decode/row-building helper live in `package_registry_canonical_artifact.go`,
  split out of
  `package_registry_canonical.go` to stay under the 500-line file cap (mirrors
  `tfstate_canonical_types.go`'s split from `tfstate_canonical.go`).
  `.registry_event` projects onto a `RegistryEvent` node carrying the
  per-version publish/yank/unyank/deprecate/delete/unlist lifecycle timeline
  the epic names. `package_id` and `version_id` are schema-OPTIONAL on this
  kind (a registry can report an event scoped to no single version), so an
  absent value there is a VALID decode the row builder's own identity gate
  drops (this row exists to project a per-VERSION timeline, and a
  registry-wide event has nothing to attach a graph edge to), NOT a
  dead-letter — do not add `version_id` to the required-field list above.
  `PackageRegistryEventRow` and its decode/row-building helper live in
  `package_registry_canonical_event.go`, split out the same way. The four
  remaining typed-but-not-yet-consumed kinds (`.source_hint`,
  `.vulnerability_hint`, `.repository_hosting`, `.warning`) have no projector
  decode site; `.source_hint`'s payload is read only by the reducer's
  `package_source_correlation` domain via raw map access, a separate reducer
  family this projector wave did not convert.
- **AWS runtime drift stays reducer-owned** —
  `aws_cloud_runtime_drift_intents.go` may enqueue one reducer intent when an
  AWS generation contains `aws_resource` facts, but the projector must not join
  AWS resources to Terraform state or config. ARN matching, backend ownership,
  and orphan/unmanaged admission belong in `internal/reducer` and
  `internal/storage/postgres`.
- **Directory sort order** — `buildDirectoryChain` sorts by `Depth` ascending so
  parent directories exist before children during graph writes
  (`canonical_builder.go:191`).
- **ReducerIntent stable ordering** — `intents` are sorted by `Domain`,
  `EntityKey`, then `FactID` before enqueue. Do not remove this sort.
- **Intent family dependency direction** — extracted family packages must
  depend on `internal/projector/intent`, never on the root projector package.
  The intent package owns the immutable fact-lookup implementation. Root
  remains the sole one-per-generation constructor and lifetime owner; Azure,
  EC2, GCP, Kubernetes, RDS, S3, security, workload-cloud-relationship,
  incident-routing, AWS-relationship, AWS-cloud-image, IAM CAN_ASSUME, package-source-correlation, cloud-inventory-admission, code-taint-evidence, code-interproc-evidence, SBOM-attestation-attachment, service-catalog-correlation, and secrets-IAM-trust-chain family builders consume the lookup. Root owns ordered family assembly and the public `ReducerIntent`
  alias for callers. A family that needs a typed-payload decode (EC2's
  `USES_PROFILE` builder was the first; S3's `LOGS_TO` builder is the second;
  the IAM CAN_ASSUME builder in `iamcanassume/` is the third, and it took the
  root `factschema_decode_iam.go` wrapper with it because that builder was the
  wrapper's only caller)
  keeps its own local decode call against `sdk/go/factschema` rather than
  importing root's classified decode wrapper, matching how `internal/reducer`
  already keeps its own independent decode copies per package. The RDS,
  workload-cloud-relationship, incident-routing, AWS-relationship,
  AWS-cloud-image, package-source-correlation, cloud-inventory-admission,
  code-taint-evidence,
  code-interproc-evidence, SBOM-attestation-attachment,
  service-catalog-correlation, and secrets-IAM-trust-chain builders trigger on
  fact presence alone and carry no decode seam.
- **Observability-coverage-correlation family (#6057)** — the
  `observability_coverage_correlation` builder lives in `observabilitycoverage/`
  and consumes the lookup like the families above. It is a decode-seam-bearing
  family: its AWS branch decodes `aws_resource.resource_type` through its own
  `factschema_decode_aws.go` against `sdk/go/factschema` (the `ec2` pattern).
  Its `observabilityResourceTypes` set is a three-way mirror with root's
  materialization trigger
  (`observability_coverage_materialization_intents.go`) and the reducer's
  `observabilityResourceSignals`
  (`go/internal/reducer/observability_coverage_correlation_index.go`); a
  resource type added to one copy must be added to all three. The root
  `firstMatchingKindPredicate` forwarder was removed with this extraction —
  this family was its last root caller; remaining root builders use
  `firstOfKind`, `firstOfKindMatching`, and `firstAcrossKinds`, and the
  seam's per-distinct-kind evaluation proof now lives in
  `intent/fact_lookup_test.go`.
- **IAM instance-profile-role family (#6057)** — the
  `iam_instance_profile_role_materialization` builder lives in
  `iaminstanceprofile/` and consumes the lookup like the families above. It is
  a decode-seam-bearing family: its trigger predicate decodes
  `aws_resource.resource_type` through its own `factschema_decode_aws.go`
  against `sdk/go/factschema` (the `ec2` pattern) and matches
  `aws_iam_instance_profile`, so root's classified `decodeAWSResource` wrapper
  keeps observability-coverage materialization as its remaining root caller.
  A no-role instance profile still triggers — the reducer's retract pass must
  run in a generation whose profile dropped its roles — and the intent shares
  the `aws_resource_materialization:<scope>` entity key with the AWS node
  builders for the canonical-nodes readiness gate. The root fan-out fixture's
  profile-typed `aws_resource` helper (`iamInstanceProfileResourceFact`) moved
  into `scope_generation_intents_fanout_test.go` with the extraction.
- **CI/CD run-correlation family (#6057)** — the `ci_cd_run_correlation`
  builder lives in `cicdruncorrelation/` and consumes the lookup like the
  families above. It carries no decode seam: it triggers on a `ci.run` fact,
  else a `ci.artifact` fact — two independent `FirstOfKind` probes, with the
  run outranking the artifact whenever both are present in the same
  generation regardless of input order (#5710). The pre-extraction root
  `cicdRunCorrelationSourceSystem` helper had the identical two-tier body as
  `projectorintent.SourceSystem`, so it was dropped rather than moved. The
  root test file mixed builder-level assertions with `buildProjection`
  dispatcher assertions; all four cases actually exercise `buildProjection`,
  so the whole file stayed at root, renamed
  `ci_cd_run_correlation_projection_test.go`.
- **Container-image-identity family (#6057)** — the
  `container_image_identity` builder lives in `containerimageidentity/` and
  consumes the lookup like the families above. It is decode-seam-bearing:
  its `aws_relationship` branch decodes optional `TargetType` through its own
  `factschema_decode_aws.go`, triggering only on `"container_image"`; every
  other branch reads only envelope fields or a local `payloadString` copy.
  Root's `decodeAWSRelationship` wrapper had this trigger as its only caller
  and moved out entirely (the `iamcanassume` precedent), unlike
  `ec2`/`observabilitycoverage` where root keeps other callers. The root
  `containerImageIdentitySourceSystem` helper was byte-identical to
  `projectorintent.SourceSystem` and was dropped rather than moved. The four
  topic-split root test files kept one builder-only case (the dockerfile
  tombstone-removal trigger test), which moved into the child's own test
  file; every other case stayed at root, renamed `_projection_test.go`.
- **Crossplane-satisfied-by family (#6057)** — the
  `crossplane_satisfied_by_materialization` builder lives in
  `crossplanesatisfiedby/` and consumes the lookup like the families above.
  It carries no decode seam: it triggers on the earliest `content_entity`
  fact whose `entity_kind` (falling back to `entity_type`) is `K8sResource`
  or `CrossplaneXRD`. The root `crossplaneSatisfiedBySourceSystem` helper was
  byte-identical to `projectorintent.SourceSystem` and was dropped rather
  than moved. **This family is NOT covered by the root fan-out parity
  fixture** — `reducer.DomainCrossplaneSatisfiedByMaterialization` appears in
  neither `fanOutParityExpectations` nor `fanOutParityExpectedOrder` in
  `scope_generation_intents_fanout_parity_test.go`, and the shared fixture in
  `scope_generation_intents_fanout_test.go` carries no
  `K8sResource`/`CrossplaneXRD` content-entity fact, so the child package's
  own tests are the only coverage for its reason string, entity key, and
  source-system derivation. The root test file mixed builder-level
  assertions with `buildProjection` dispatcher assertions; all three cases
  actually exercise `buildProjection`, so the whole file stayed at root,
  renamed `crossplane_satisfied_by_materialization_projection_test.go`.
- **CanonicalWriter interface boundary** — no caller in this package calls a Neo4j
  or NornicDB driver directly. All canonical writes go through `CanonicalWriter`.
  Backend-specific logic belongs in `internal/storage/cypher` adapters.
- **Superseded work stops cleanly** — `Service.processWork` treats
  `ErrWorkSuperseded` from `ProjectorWorkHeartbeater` as expected cancellation,
  not a failed projection. The current worker must not ack or fail a generation
  once Postgres proves a newer same-scope generation replaced it.

## Common changes and how to scope them

- **Add a new entity type** → add to `entityTypeLabelMap` in `canonical.go`,
  add a schema constraint in the graph schema file, run
  `go test ./internal/projector -count=1`. Why: `EntityTypeLabel` and
  `extractEntities` both gate on this map; missing entries silently drop nodes.

- **Add a new projection stage write** → add to `Runtime.Project` in
  `runtime.go`; add `ProjectorStageDuration` recording with the new stage label
  in `runtime_stages.go`; add a span if the stage crosses a service boundary;
  add a test in `runtime_test.go`. Why: all stage telemetry is labeled and must
  appear in the telemetry contract at `go/internal/telemetry/contract.go`.

- **Change concurrency behavior** → touch `service.go` `runConcurrent`,
  `service_superseded.go`, and the large-generation semaphore; run
  `service_test.go` and `service_shutdown_test.go`; read
  `docs/public/reference/telemetry/index.md` for
  `eshu_dp_large_repo_semaphore_wait_seconds` guidance. Why: worker goroutines
  share a cancel context; wrong cancellation propagation causes silent dropped
  work or stale-generation graph writes.

- **Add a new reducer domain intent** → add the domain constant in
  `internal/reducer`, add intent construction in `buildReducerIntent` or a
  new `build*ReducerIntent` helper in `runtime.go` or `semantic_entity_intents.go`,
  add a test in `stage_relationships_test.go` or the semantic intents test files.
  Why: intent domain values must be parseable by `reducer.ParseDomain`.

- **Add a new typed canonical family** → besides wiring it into
  `buildCanonicalMaterialization`, add its fact-kind prefix to
  `quarantinedFactStagePrefixes` in `factschema_quarantine.go`. Why: a family
  with no matching prefix falls through to the `unknown_canonical` stage label,
  so its dead letters are reported under a placeholder instead of the owning
  stage and an operator cannot see that family's input_invalid rate at all. The
  table is ordered longest-prefix-first; the reasoning behind both the ordering
  and the distinct fallback label is on the symbols themselves.

## Failure modes and how to debug

- Symptom: `eshu_dp_projections_completed_total{status="failed"}` rising →
  likely cause: graph backend unavailable or fact validation error → check
  structured log `failure_class` field; `dependency_unavailable` is retryable,
  `projection_bug` needs code investigation.

- Symptom: `eshu_dp_projector_stage_duration_seconds{stage="canonical_write"}`
  elevated → likely cause: graph backend write contention or slow Cypher
  execution → check `eshu_dp_canonical_write_duration_seconds` and
  `eshu_dp_neo4j_query_duration_seconds`; inspect `telemetry.SpanCanonicalProjection`
  traces.

- Symptom: projector queue age (`eshu_dp_queue_oldest_age_seconds`) growing →
  likely cause: workers cannot keep up → check `eshu_dp_worker_pool_active`,
  consider raising `Service.Workers`; check `eshu_dp_large_repo_semaphore_wait_seconds`
  if large repos dominate.

- Symptom: one repository repeatedly shows a newer pending generation behind a
  live older projector row → likely cause: the running worker has not observed
  `ErrWorkSuperseded` yet or heartbeats are disabled → check structured logs for
  `projector work superseded by newer generation` and verify
  `ProjectorWorkHeartbeater` is wired.

- Symptom: phase state missing in `graph_projection_phase_state` → likely cause:
  `PhasePublisher.PublishGraphProjectionPhases` failing silently → check
  `projector runtime stage completed` logs for `stage=canonical_write` error
  fields; check repair queue depth.

- Symptom: entities missing from graph for a repository → likely cause: unmapped
  `entity_type` string dropped in `extractEntities` → add the type to
  `entityTypeLabelMap` and re-project; check `projector runtime stage completed`
  logs for `entity_count=0` on affected generations.

## Anti-patterns specific to this package

- **Branching on backend brand** — do not add `if backend == "nornicdb"` checks
  here. Backend dialect belongs in `internal/storage/cypher` adapters behind the
  `CanonicalWriter` interface.

- **Writing directly to Neo4j/NornicDB drivers** — all graph writes must go
  through `CanonicalWriter.Write`. Direct driver calls bypass instrumentation,
  retry policy, and the backend-neutral contract.

- **Setting `ContentBeforeCanonical` outside local-profile wiring** — this flag
  reverses write order for degraded-backend situations. Setting it in full-stack
  or production wiring breaks the `canonical_nodes_committed` gate that reducer
  edge domains depend on.

- **Adding entity types without schema constraints** — every new entry in
  `entityTypeLabelMap` must have a corresponding Neo4j constraint or index in
  the graph schema. Entries without schema support produce nodes that violate
  the conformance matrix.

## What NOT to change without an ADR

- `CanonicalWriter` interface shape — changing the signature breaks every caller
  and the backend-neutral contract; see
  `docs/public/reference/backend-conformance.md`.
- `graph_projection_phase_state` publish semantics — reducer edge domains gate
  on `canonical_nodes_committed`; removing or deferring the publish breaks
  shared projection ordering.
- `entityTypeLabelMap` entries once a label has graph schema constraints — label
  renames require coordinated graph migration; see
  `docs/public/reference/cypher-performance.md` for
  write-order constraints.

## Evidence notes

Historical no-regression and root-cause evidence for this package lives in
[`evidence-notes.md`](evidence-notes.md) alongside this file. It was split out
so `AGENTS.md` keeps headroom under the 500-line Markdown cap while the
remaining intent families move into child packages (#6057); the sibling
convention is `internal/reducer`, which carries its evidence the same way.
