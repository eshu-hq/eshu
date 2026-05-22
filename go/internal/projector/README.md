# Projector

## Purpose

`internal/projector` owns source-local projection. It reads committed fact
envelopes for one scope generation, builds canonical graph/content
materialization, publishes reducer readiness checkpoints, and enqueues
shared-domain reducer intents. Cross-source admission and relationship truth
belong to `internal/reducer`.

## Ownership boundary

Projector code owns the path from durable facts to source-local graph nodes,
content rows, and reducer intent rows. It does not open graph drivers directly
and does not decide AWS-to-Terraform, package-to-repository, or other
cross-source joins.

The runtime flow is:

```text
ProjectorWorkSource.Claim
  -> FactStore.LoadFacts
  -> Runtime.Project
  -> CanonicalWriter.Write
  -> PhasePublisher.PublishGraphProjectionPhases
  -> content.Writer.Write
  -> ReducerIntentWriter.Enqueue
  -> ProjectorWorkSink.Ack
```

`ProjectorWorkHeartbeater` may return `ErrWorkSuperseded` when a newer
same-scope generation replaces the claimed work. That path stops without acking
or failing the stale item.

## Exported surface

See `doc.go` and `go doc ./internal/projector` for the full godoc contract.
The main package contracts are:

- `Service` runs the poll-and-dispatch loop.
- `Runtime` implements projection for one claimed generation.
- `CanonicalWriter`, `ReducerIntentWriter`, `ProjectorWorkSource`,
  `ProjectorWorkSink`, and `ProjectorWorkHeartbeater` are the ports.
- `CanonicalMaterialization`, `Result`, `ReducerIntent`, and
  `ScopeGenerationWork` are the core data shapes.
- `ClassifyFailure`, `FailureClassification`, `StageError`,
  `InputValidationError`, and `ResourceExhaustedError` define durable failure
  metadata.
- `EntityTypeLabel` and `EntityTypeLabelMap` keep content entity labels aligned
  with graph labels and schema tests.

## Dependencies

- `internal/content` for content-store materialization.
- `internal/facts` for committed fact envelopes.
- `internal/queue` for durable failure records.
- `internal/reducer` for phase publication, repair, and intent domains.
- `internal/scope` for scope and generation identity.
- `internal/telemetry` for spans, metrics, and structured log helpers.

Graph writes route through `internal/storage/cypher.CanonicalNodeWriter` via the
`CanonicalWriter` interface. Terraform-state facts are projected as
`TerraformResource`, `TerraformModule`, and `TerraformOutput` nodes with
lineage, serial, provider, tag hash, and correlation-anchor evidence kept as
properties. OCI registry facts are projected as digest-addressed image
manifest/index/descriptor rows; tag facts remain weak mutable observations and
do not define image identity. The projector never calls a Neo4j or NornicDB
driver directly.
Package-registry facts are projected only for stable ecosystem identity and
package-native dependency truth: `PackageRegistryPackageRow`,
`PackageRegistryVersionRow`, and `PackageRegistryDependencyRow` create package,
version, and dependency nodes. `package_registry.source_hint` remains
provenance-only until reducer correlation proves ownership, publication, or
consumption.
When a generation contains source hints, `buildPackageSourceCorrelationReducerIntent`
emits one `package_source_correlation` reducer intent for the scope so the
reducer can classify all hints against active repository facts once.
AWS cloud facts follow the same source-local rule. The projector does not join
AWS resources to Terraform state; when a generation contains one or more
`aws_resource` facts, `buildAWSCloudRuntimeDriftReducerIntent` emits one
`aws_cloud_runtime_drift` reducer intent for the AWS scope/generation so the
reducer can run the bounded ARN join after source-local projection succeeds.
Container-image identity follows the same handoff rule: when a generation
contains OCI digest/tag facts, AWS image-reference facts, AWS container-image
relationships, or Git content-entity image references,
`buildContainerImageIdentityReducerIntent` emits one
`container_image_identity` reducer intent for that scope/generation. The
projector still does not join images to workloads or runtime evidence; the
reducer owns digest-first admission after source-local projection succeeds.
Service-catalog facts follow the same schema-gated handoff. When a generation
contains service-catalog entity, ownership, repository-link, dependency, API,
operational-link, scorecard, or warning facts,
`buildServiceCatalogCorrelationReducerIntent` emits one
`service_catalog_correlation` reducer intent for that scope/generation. The
projector rejects unsupported service-catalog schema versions during projection
so stale collector payloads cannot silently reach the reducer.

## Telemetry

Key metrics include `eshu_dp_projector_run_duration_seconds`,
`eshu_dp_projections_completed_total`,
`eshu_dp_projector_stage_duration_seconds`,
`eshu_dp_queue_claim_duration_seconds{queue="projector"}`,
`eshu_dp_canonical_writes_total`,
`eshu_dp_canonical_write_duration_seconds`,
`eshu_dp_reducer_intents_enqueued_total`, and
`eshu_dp_large_repo_semaphore_wait_seconds`.

Key spans are `projector.run`, `canonical.projection`, and
`reducer_intent.enqueue`. Structured logs use scope `projector` and phase
`projection`; failure paths must carry `failure_class`.

## Gotchas / invariants

- Projection must be idempotent. Retries, duplicate claims, and partial graph
  writes must converge on the same graph truth.
- `PublishGraphProjectionPhases` must succeed before ack. If the publish fails
  and a repair queue is wired, the projector enqueues repair work instead of
  hiding the failed checkpoint.
- Terraform-state warning-only generations still publish
  `canonical_nodes_committed` checkpoints for the Terraform resource and module
  keyspaces. Zero graph rows is still a durable completion signal.
- `Module` and `Parameter` entity types are excluded from generic entity
  extraction because they use different graph MERGE keys.
- File and entity paths are repo-qualified to avoid cross-repository MERGE
  collisions.
- OCI image identity is digest-backed. Tag observations are mutable evidence and
  must not mint canonical image identity.
- Package-registry `source_hint` values stay provenance-only until reducer
  correlation proves ownership, publication, or consumption.
- AWS facts stay source-local here; the projector only enqueues
  `aws_cloud_runtime_drift` for reducer-owned ARN matching.
- `ContentBeforeCanonical` is for degraded local profiles only, not production
  full-stack projection.
- Reducer intents are sorted by domain, entity key, and fact ID before enqueue.

## Verification

Use the smallest command that proves the changed contract:

```bash
cd go
go test ./internal/projector -count=1
go doc ./internal/projector
go run ./cmd/eshu docs verify ../go/internal/projector --limit 1000 \
  --fail-on contradicted,missing_evidence
```

If the change touches graph writes, queues, workers, batching, or Cypher, also
run the performance-evidence gate from the repo root:

```bash
scripts/test-verify-performance-evidence.sh
scripts/verify-performance-evidence.sh
```

## Related docs

- `docs/public/architecture.md`
- `docs/public/deployment/service-runtimes.md`
- `docs/public/reference/telemetry/index.md`
- `docs/public/reference/cypher-performance.md`
- `docs/public/reference/nornicdb-tuning.md`
