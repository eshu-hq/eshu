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
  provenance. The three consumed kinds (`package`, `.package_version`,
  `.package_dependency`) decode through the `sdk/go/factschema` seam
  (`factschema_decode_packageregistry.go`), NOT raw `payloadString`/
  `payloadBoolPtr`/`payloadStringSlice`, reusing the family-neutral quarantine
  apparatus `oci_registry`/`terraform_state` introduced: a fact missing a
  required identity field (`package_id`, `version_id`, `version`,
  `dependency_package_id`) is quarantined per-fact via
  `partitionProjectorDecodeFailures` and recorded as a visible `input_invalid`
  dead-letter (`eshu_dp_projector_input_invalid_facts_total` under the
  `package_registry_canonical` stage). The six typed-but-not-yet-consumed kinds
  (`.source_hint`, `.package_artifact`, `.vulnerability_hint`,
  `.registry_event`, `.repository_hosting`, `.warning`) have no projector
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

No-Regression Evidence: Wave 4b, terraform_state projector typed-payload decode.
`extractTerraformStateRows` now decodes `terraform_state_snapshot`, `_resource`,
`_module`, `_output`, and `_tag_observation` payloads through the
`sdk/go/factschema` seam (`factschema_decode_terraformstate.go`) instead of raw
`payloadString`/`payloadInt` map lookups, on the same cold,
once-per-scope-generation projection path and reusing the family-neutral
`partitionProjectorDecodeFailures` apparatus oci_registry introduced. A fact
missing a required identity key (resource address, module address,
tag-observation join key) is quarantined per-fact rather than producing a row
under an empty-string identity; identity fields are trimmed before the empty
check so a present-but-whitespace-only identity drops as non-materializable,
matching the pre-typing `payloadString` behavior. `go test ./internal/projector
-run 'TestExtractTerraformStateRowsQuarantinesMissingResourceAddress|TestExtractTerraformStateRowsWhitespaceAddressIsDroppedNotMaterialized|TestExtractTerraformStateRowsPresentButEmptyAddressIsDroppedNotQuarantined'
-count=1` failed before the conversion (a missing or blank address silently
produced no row with no operator signal) and passes after: the malformed fact is
recorded on the quarantine slice while valid facts still materialize
byte-identical rows. The B-7 golden-corpus gate is green (406 pass, 0
required-fail) with no B-12 snapshot change beyond the collector cassette
reconciliation, so valid facts produce byte-identical graph output and only
malformed→dead-letter is new. The conversion adds no hot per-edge loop and no
Cypher shape; the decoder is the same reflection field-plan walk with the
marshal-free `decode_map.go` fast path oci_registry measured, so the
per-generation cost stays in the same ~1.5 µs/fact cold-path band as
oci_registry (extractor shape and decoder are identical to that measured path,
so no separate benchmark run was warranted). Result class: Correctness win,
cold-path cost identical to the measured oci_registry path.

Observability Evidence: Wave 4b, terraform_state projector typed-payload decode.
The terraform_state extractor routes a malformed required field through the same
per-fact `eshu_dp_projector_input_invalid_facts_total` counter the oci_registry
migration introduced, under a new `stage`=`terraform_state_canonical` label value
(`quarantinedFactStage` in `factschema_quarantine.go`) alongside the existing
`fact_kind` label, so an operator sees the terraform_state dead-letter rate
distinctly from the oci one on the same "Projector input_invalid Facts (rate)"
panel. The `factschema_decode_terraformstate.go` decode wrappers emit no metric
of their own; they surface a decode failure through
`recordProjectorQuarantinedFacts` (the counter plus the structured
`projector input_invalid fact quarantined` error log carrying `fact_id` +
`missing_field`). The migration adds no route, graph query shape, queue table,
worker, lease, or runtime knob.

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

No-Regression Evidence (Wave 4c, package_registry projector typed-payload
decode): `extractPackageRegistryRows` and its three row builders
(`packageRegistryPackageRow`, `packageRegistryVersionRow`,
`packageRegistryDependencyRow`) now decode `package_registry.package`,
`.package_version`, and `.package_dependency` fact payloads through the
`sdk/go/factschema` seam (`factschema_decode_packageregistry.go`) instead of
raw `payloadString`/`payloadBoolPtr`/`payloadStringSlice` map lookups. This is
a cold, once-per-scope-generation projection path (not a hot per-edge loop). A
fact missing a required identity field (`package_id`/`version_id`/`version`/
`dependency_package_id`) is quarantined per-fact via
`partitionProjectorDecodeFailures` rather than silently producing a row under
an empty-string identity, or (for the dependency edge) failing only its own
`StableFactKey`-derived uid gate. `go test ./internal/projector -run
'TestExtractPackageRegistryRowsQuarantinesMissingPackageID|TestExtractPackageRegistryRowsPresentButEmptyPackageIDIsDroppedNotQuarantined|TestExtractPackageRegistryRowsWhitespacePackageIDIsDroppedNotMaterialized|TestExtractPackageRegistryRowsQuarantinesMissingDependencyJoinKey'
-count=1 -v` failed to even COMPILE against the pre-conversion code at
HEAD~ (the old `extractPackageRegistryRows` had a `void` signature — no
`[]quarantinedFact` return existed for the test to assert on, proving the
per-fact quarantine contract is genuinely new behavior, not a pre-existing
capability the test merely exercises), then passed after: the malformed fact
is recorded on the quarantine slice and the valid sibling package/version still
materializes its identity-keyed row, with no uid ever computed from an
empty-string or whitespace-only identity segment. Every existing
package_registry valid-path test
(`TestBuildCanonicalMaterializationExtractsPackageRegistryRows`,
`TestBuildCanonicalMaterializationExtractsPackageRegistryDependencies`,
`TestBuildCanonicalMaterializationSkipsUnstablePackageRegistryDependency`,
`TestBuildCanonicalMaterializationKeepsPackageSourceHintsProvenanceOnly`,
`TestRuntimeProjectRejectsUnknownPackageRegistrySchemaVersion`,
`TestRuntimeProjectLocksPackageRegistryIdentitiesAroundCanonicalWrite`,
`TestRuntimeProjectSkipsPackageRegistryIdentityLockWithoutPackageRows`, and
`TestBuildProjectionQueuesSecurityAlertReconciliationForPackageRegistryPackage`)
stays green unchanged, so valid facts produce byte-identical graph rows and
only malformed→dead-letter is new behavior. Measured with `go test
./internal/projector -run '^$' -bench 'BenchmarkExtractPackageRegistryRows'
-benchmem -count=6 -benchtime=200x` (darwin/arm64, Apple M1 Max; input: 1,000
synthetic packages × 3 package_registry fact kinds = 3,000 facts), each side
measured in a quiesced window after this machine's concurrent-agent load
average settled (an initial noisy sample under load average ~48 showed BEFORE
apparently slower than AFTER — a measurement artifact from system contention,
not a real signal; re-measuring after load settled reversed and stabilized the
comparison). BEFORE (raw `payloadString`/`payloadBoolPtr`/`payloadStringSlice`
map reads, pre-conversion at HEAD~2, measured in a throwaway detached
worktree) -> AFTER (typed decode): ~1.66 ms/op, 4.10 MB/op, 9,036 allocs/op ->
~3.4 ms/op, 5.28 MB/op, 32,036 allocs/op — approximately 1130 ns/fact typed
vs. 553 ns/fact raw-map, a ~580 ns/fact delta. (The status-flag fields
is_yanked/is_unlisted/is_deprecated/is_retracted on a version and
optional/excluded on a dependency are typed as OPTIONAL `*bool` — descriptive
flags, not required identity keys, so a persisted or older fact that omits them
still projects rather than quarantines — which adds ~6,000 pointer-box allocs
over the earlier required-non-pointer-bool draft; the per-fact cost stays flat.)
This is within the same accuracy-guarantee cost band the OCI (~1.5 µs/fact) and
terraform_state (~1.5 µs/fact) waves already accepted; package_registry's
~1.13 µs/fact typed cost is in fact lower than both precedents. A cold,
once-per-scope-generation
projection path: a real package-registry repository generation carries a
handful to low hundreds of packages, not the 1,000-package stress bound this
benchmark uses, so the per-generation wall-clock cost stays in the
low-single-digit-millisecond range. CPU and heap profiling
(`go tool pprof -top -alloc_objects`) attributed the added allocations to
`factschema.assignField`'s already-existing marshal-free fast path for
`*string`/`*bool`/`[]string`/`map[string]string` fields (each optional pointer
field heap-allocates its target via `reflect.ValueOf(&v)`) — no
`jsonRoundTripValue` fallback appears in the profile for any package_registry
field, so none of the three consumed structs' shapes trip the
`decode_map.go` json-round-trip perf trap the kubernetes_live wave hit (+59%
regression); the cost is the same reflection field-plan walk plus per-fact
struct allocations the incident/OCI/terraform_state waves already measured and
accepted. Result class: Correctness win with a bounded, measured cold-path
cost, no root-cause action required.

Observability Evidence (Wave 4c, package_registry projector typed-payload
decode): the migration routes a malformed `package_registry.package`,
`.package_version`, or `.package_dependency` fact through the same per-fact
`eshu_dp_projector_input_invalid_facts_total` counter the oci_registry and
terraform_state migrations introduced, under a new `stage`=
`package_registry_canonical` label value. `quarantinedFactStage`
(`factschema_quarantine.go`) is generalized from a 2-way
`terraform_state`-vs-`oci_registry` prefix check into an ORDERED
(longest-prefix-first) `quarantinedFactStagePrefixes` slice so a third (and any
future) typed family adds one entry instead of another `if`/`else` branch, and
so routing is deterministic rather than dependent on Go map iteration order. An
unmatched fact kind returns a distinct `unknown_canonical` stage — not another
family's label — so a NEW typed family wired into
`buildCanonicalMaterialization` without a matching prefix entry surfaces its
dead-letters honestly instead of misattributing them to `oci_registry`. An
operator sees the package_registry dead-letter rate distinctly from the oci and
terraform_state ones on the same "Projector input_invalid Facts (rate)" panel. The `factschema_decode_packageregistry.go` decode wrappers emit no
metric of their own; they surface a decode failure through
`recordProjectorQuarantinedFacts` (the counter plus the structured
`projector input_invalid fact quarantined` error log carrying `fact_id` +
`missing_field`). The migration adds no route, graph query shape, queue table,
worker, lease, or runtime knob.
