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
  `package_source_correlation_intents.go` may enqueue the reducer classifier,
  but that intent is counter-only until reducer admission grows stronger
  provenance.
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

No-Regression Evidence (Wave 4b, oci_registry projector typed-payload decode):
the OCI canonical extractor (`extractOCIRegistryRows` and its six row builders)
now decodes `oci_registry.repository`, `.image_manifest`, `.image_index`,
`.image_descriptor`, `.image_tag_observation`, and `.image_referrer` fact
payloads through the `sdk/go/factschema` seam
(`oci_registry_factschema.go`) instead of raw `payloadString`/`payloadInt`/
`payloadBoolPtr` map lookups. This is a cold, once-per-scope-generation
projection path (not a hot per-edge loop). A fact missing a required identity
field (`repository_id`/`digest`/`tag`/`resolved_digest`/`subject_digest`/
`referrer_digest`) is quarantined per-fact via `partitionProjectorDecodeFailures`
rather than silently producing a row under an empty-string descriptor uid.
`go test ./internal/projector -run
'TestExtractOCIRegistryRowsQuarantinesMissingManifestDigest|TestExtractOCIRegistryRowsPresentButEmptyDigestIsDroppedNotQuarantined'
-count=1 -v` failed before the conversion (the API returned no quarantined
facts; a missing-digest manifest silently produced no row with no operator
signal), then passed after: the malformed fact is recorded on the quarantine
slice and the valid sibling manifest still materializes its digest-keyed row,
with no uid ever computed from the empty-string identity segment. Every existing
OCI valid-path test (`TestBuildCanonicalMaterializationExtractsOCIRegistryRows`,
`TestBuildCanonicalMaterializationSkipsTagOnlyOCIIdentity`,
`TestRuntimeProjectRejectsUnknownOCIRegistrySchemaVersion`, and the OCI
container-image-identity / SBOM / supply-chain intent tests) stays green
unchanged, so valid facts produce byte-identical graph rows and only
malformed→dead-letter is new behavior. Measured with `go test
./internal/projector -run '^$' -bench 'BenchmarkExtractOCIRegistryRows'
-benchmem -count=4 -benchtime=100x` (darwin/arm64, Apple M-series; input: 1,000
synthetic repositories × 6 OCI fact kinds = 6,000 facts). BEFORE (raw
`payloadString` map reads, pre-conversion at HEAD~2, measured in a throwaway
detached worktree) -> AFTER (typed decode): ~6.6 ms/op, 9.37 MB/op, 70,083
allocs/op -> ~9.0 ms/op, 10.79 MB/op, 101,086 allocs/op. This is a cold,
once-per-scope-generation projection path (not a hot per-edge loop): a real OCI
repository generation carries a handful of manifests, so the per-generation
cost is microseconds; the 6,000-fact corpus is a stress bound. The residual
cost is the typed-struct decode (reflection field-plan walk + per-fact struct
allocations) over raw map lookups — the same accuracy-guarantee cost the
incident wave accepted at ~1.2 µs/fact; here it is ~1.5 µs/fact. The nested
descriptor fields (manifest `config`/`layers`, index `manifests`) were an
avoidable first-pass regression — decoded through the `sdk/go/factschema`
`decode_map.go` json round-trip fallback — until `assignField` gained a
marshal-free fast path for pointer-to-struct, slice-of-struct, and
`map[string]string` fields (an additive shared-decoder improvement that also
speeds `awsv1.EC2InstancePosture`'s `[]BlockDevice`, with the existing
`BenchmarkDecodeAWSResource` at ~1180 ns/17 allocs and
`BenchmarkExtractCloudResourceNodeRows` at ~16.4 ms unchanged, proving no
existing-family regression). Result class: Correctness win with a bounded,
measured cold-path cost.

Observability Evidence (Wave 4b, oci_registry projector typed-payload decode):
the migration introduces the projector's FIRST per-fact input_invalid signal,
`eshu_dp_projector_input_invalid_facts_total` (labeled `stage`=
`oci_registry_canonical`, `fact_kind`), the projector-side counterpart to the
reducer's `eshu_dp_reducer_input_invalid_facts_total`. A malformed OCI fact
surfaces through `recordProjectorQuarantinedFacts` — the counter plus the
structured `projector input_invalid fact quarantined` error log carrying
`fact_id` + `missing_field` — instead of the pre-typing silent drop. The new
instrument is registered in `go/internal/telemetry/instruments.go`, documented
in the X1 contract doc (`docs/public/observability/telemetry-coverage.md`) and
the operator reference (`docs/public/reference/telemetry/index.md`), and charted
on the operator dashboard's "Projector input_invalid Facts (rate)" panel. The
migration adds no route, graph query shape, queue table, worker, lease, or
runtime knob; it reuses the existing projector build path and per-fact
isolation model.
