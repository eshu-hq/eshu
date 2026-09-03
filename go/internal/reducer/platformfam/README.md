# platformfam

Owns the reducer's platform vocabulary and the `deployment_mapping` reduction
that turns a platform-binding intent into a canonical fact.

This package moved out of the flat `internal/reducer` root under issue #6061.

## What it owns

| piece | file | what it does |
|---|---|---|
| `TerraformRuntimeFamily` + registry | `platform_families.go` | the eight registered runtime families (ECS, EKS, Lambda, Cloudflare Workers, GKE, AKS, Cloud Run, Azure Container Apps) and their cluster/service module patterns |
| `RuntimeFamilies`, `LookupRuntimeFamily` | `platform_families.go` | enumerate the registry, resolve one family by normalized kind |
| `InferTerraformRuntimeFamilyKind`, `InferRuntimeFamilyKindFromIdentifiers`, `InferInfrastructureRuntimeFamilyKind` | `platform_families.go` | read a family kind out of Terraform content, repo identifiers, or a resource-type/module-source pair |
| `IsClusterResourceType`, `IsClusterModuleSource`, `MatchesServiceModuleSource` | `platform_families.go` | registry membership questions the root's infrastructure-platform extractor asks |
| `TerraformPlatformEvidenceKind`, `FormatPlatformKindLabel` | `platform_families.go` | stable evidence-kind strings and human labels |
| `PlatformMaterializationHandler` | `platform_materialization.go` | the `deployment_mapping` reducer handler |
| `PlatformMaterializationWriter`, `PlatformGraphLocker`, `CrossRepoRelationshipResolver`, `WorkloadMaterializationReplayer` | `platform_materialization.go` | the four seams the handler is wired through |
| `PostgresPlatformMaterializationWriter` | `platform_materialization_writer.go` | persists the canonical `reducer_platform_materialization` fact |

## Exported surface

`TerraformRuntimeFamily`, `RuntimeFamilies`, `LookupRuntimeFamily`,
`InferTerraformRuntimeFamilyKind`, `InferRuntimeFamilyKindFromIdentifiers`,
`InferInfrastructureRuntimeFamilyKind`, `IsClusterResourceType`,
`IsClusterModuleSource`, `MatchesServiceModuleSource`,
`TerraformPlatformEvidenceKind`, `FormatPlatformKindLabel`,
`PlatformMaterializationWrite`, `PlatformMaterializationWriteResult`,
`PlatformMaterializationWriter`, `PlatformGraphLocker`,
`CrossRepoRelationshipResolver`, `WorkloadMaterializationReplayer`,
`PlatformMaterializationHandler` (and its `Handle`),
`PostgresPlatformMaterializationWriter` (and its `WritePlatformMaterialization`).

## What it does not own

`PROVISIONS_PLATFORM` edges. The dedicated `platform_infra_materialization`
domain owns that verb and still lives in the reducer root, because it depends on
the root's `InfrastructurePlatform` extractor, row, result and materializer --
graph-write logic with consumers across the root, `cmd/ifa`, `cmd/reducer`,
`internal/ifa` and `internal/graphschemacompat`. Relocating that tier is its own
change, not a helper hoist. `PlatformMaterializationHandler.Handle` deliberately
does not write those edges either; it owns the `deployment_mapping` canonical
fact write and cross-repo resolution only.

## Package boundary

Imports point strictly downward. This package reaches `reducer/contract`,
`reducer/factwrite`, `reducer/gpphase`, `reducer/payloadcore`, `internal/facts`
and `pkg/log`, and it never imports the parent `internal/reducer` package. The
dependency runs the other way: the root keeps compatibility aliases in
`platform_compat.go` (plus `intent.go` for the fact-kind constant) so its own
callers, `cmd/reducer` and `internal/storage/postgres` compile unchanged.

`CrossRepoRelationshipResolver` is why that boundary holds. The handler needs
the root's `CrossRepoRelationshipHandler`, so it names the behaviour instead of
the type: one `Resolve` per scope generation, returning a canonical edge count.

## Gotchas / invariants

- **A nil concrete resolver is not a nil interface.** `CrossRepoResolver` is an
  interface, so assigning a nil `*CrossRepoRelationshipHandler` into it yields a
  non-nil interface holding a nil pointer, and `Handle`'s
  `h.CrossRepoResolver != nil` guard would then dereference it. The root's
  `defaults_domain_catalog.go` assigns the field only when the cross-repo
  dependencies were wired, and `defaults_test.go` pins that.
- **`input_ready` reflects input presence, not write count.** The platform
  writer runs unconditionally, so `canonicalWrites == 0` is genuine empty work
  rather than an ordering stall. `Handle` derives `input_ready` from the request
  entity keys, which `platformMaterializationWriteFromIntent` has already
  validated as non-empty.
- **Workload replay only follows real cross-repo writes.** The replayer is
  called once per related scope and only when cross-repo resolution wrote at
  least one edge; a resolver that returns zero must not trigger a replay storm.
- **The canonical id and the stable fact key are not the same string.** The id
  includes the source system, the fact key does not, so two source systems
  reporting the same platform binding converge on one durable row while keeping
  distinct canonical ids.
- **`RuntimeFamilies` returns a copy.** Callers may sort or filter the result
  without mutating the registry; the two membership predicates exist so hot
  extractor paths can ask a question without paying for that copy.
- **`LookupRuntimeFamily` returns a pointer into the registry.** Do not mutate
  what it returns.

## Telemetry

No-Observability-Change: this package emits no instrument of its own. Reducer
executions that run these handlers stay covered by
`eshu_dp_reducer_executions_total` and `eshu_dp_reducer_run_duration_seconds`,
and the canonical fact the writer publishes flows through
`Result.CanonicalWrites` into `eshu_dp_canonical_writes_total`.

`Handle` logs `deployment mapping materialization completed` with
`entity_key_count`, `related_scope_count`, `canonical_write_count`,
`cross_repo_write_count`, `workload_replay_count` and the per-stage wall times,
and returns those stages again in `Result.SubDurations` (`platform_write`,
`cross_repo_resolve`, `workload_replay`, `phase_publish`, `total`) alongside
`input_ready` and `written_rows` in `Result.SubSignals`. The reducer service
layer emits them as `sub_duration_<key>_seconds` and `sub_signal_<key>`.

No-Regression Evidence: behavior-preserving relocation, proven by
`go test ./internal/reducer/... -count=1` (clean) and `go vet ./...` (clean) on
this branch. The family's own tests moved with it and assert the same behaviour;
only the cross-repo double changed, from the root's concrete handler to a
resolver that records the scope generation it was asked about. One call path did
change shape: `CrossRepoResolver` is now an interface, so the handler dispatches
dynamically instead of through a concrete `*CrossRepoRelationshipHandler`. That
call happens at most once per `deployment_mapping` intent and follows a Postgres
canonical write, so it is not on a hot path. Every other relocated symbol keeps
its body unchanged, and every reducer-root caller reaches it through a type
alias or a one-line forwarder.

## Tests

`go test ./internal/reducer/platformfam/... -count=1`
