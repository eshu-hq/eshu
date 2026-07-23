# Crossplane Parser

This page tracks the checked-in Go parser contract in the current repository state.
Canonical implementation: `go/internal/parser/registry.go` plus the entrypoint and tests listed below.

## Parser Contract
- Language: `crossplane`
- Family: `iac`
- Parser: `DefaultEngine (yaml)`
- Entrypoint: `go/internal/parser/yaml_language.go`
- Fixture repo: `tests/fixtures/ecosystems/crossplane_comprehensive/`
- Unit test suite: `go/internal/parser/engine_yaml_semantics_test.go`
- Integration validation: compose-backed fixture verification (see [Local Testing Runbook](../reference/local-testing.md))

## Capability Checklist
| Capability | ID | Status | Extracted Bucket/Key | Required Fields | Graph Surface | Unit Coverage | Integration Coverage | Rationale |
|-----------|----|--------|------------------------|-----------------|---------------|---------------|----------------------|-----------|
| Composite Resource Definitions (XRDs) | `composite-resource-definitions-xrds` | supported | `crossplane_xrds` | `name, line_number` | `node:CrossplaneXRD` | `go/internal/parser/engine_yaml_semantics_crossplane_test.go::TestDefaultEngineParsePathYAMLCrossplaneResources` | Compose-backed fixture verification | - |
| XRD group, kind, version | `xrd-group-kind-version` | supported | `properties` | `name, line_number, kind, group, claim_kind` | `property:XRD.properties` | `go/internal/parser/engine_yaml_semantics_crossplane_test.go::TestDefaultEngineParsePathYAMLCrossplaneResources` | Compose-backed fixture verification | - |
| Compositions | `compositions` | supported | `crossplane_compositions` | `name, line_number` | `node:CrossplaneComposition` | `go/internal/parser/engine_yaml_semantics_crossplane_test.go::TestDefaultEngineParsePathYAMLCrossplaneResources` | Compose-backed fixture verification | - |
| Composition composite type ref | `composition-composite-type-ref` | supported | `crossplane_compositions` | `name, line_number, composite_api_version, composite_kind` | `property:Composition.composite_ref` | `go/internal/parser/engine_yaml_semantics_crossplane_test.go::TestDefaultEngineParsePathYAMLCrossplaneResources` | Compose-backed fixture verification | - |
| Composition resources list | `composition-resources-list` | supported | `crossplane_compositions` | `name, line_number, resource_count, resource_names` | `property:Composition.resources` | `go/internal/parser/engine_yaml_semantics_crossplane_test.go::TestDefaultEngineParsePathYAMLCrossplaneResources` | Compose-backed fixture verification | - |
| Claims | `claims` | supported | `k8s_resources` | `name, line_number, api_version, kind` | `node:K8sResource` + `edge:SATISFIED_BY -> CrossplaneXRD` | `go/internal/parser/engine_yaml_semantics_crossplane_test.go::TestDefaultEngineParsePathYAMLCrossplaneResources`, `go/internal/reducer/crossplane_satisfied_by_edge_rows_test.go` | Compose-backed fixture verification | A Claim is never parser-labeled (issue #5347): it parses as a generic K8sResource row and the reducer correlation layer classifies it by resolving (group, kind) against exactly one CrossplaneXRD's (spec.group, spec.claimNames.kind), materializing the SATISFIED_BY edge. |

## Framework And Library Support

Supported today:

- Crossplane is infrastructure evidence, not application-framework reachability.
- XRDs, Compositions, composition resource lists, and Claims are modeled as
  Crossplane configuration evidence.

Not claimed today:

- Composition patch transforms, Composition Function pipeline behavior, managed
  resource runtime state, and provider-specific semantics are not modeled.

## Known Limitations
- Composition patch transforms are not modeled as graph edges
- XRD validation schema details are not extracted
- Usage of Composition Functions (pipeline steps) is not captured as structured nodes
- A Claim is edge-only (issue #5347): it stays a `K8sResource` node — the
  `SATISFIED_BY` edge to its `CrossplaneXRD` IS the classification, never a
  `CrossplaneClaim` graph label. Relabeling would collide with the per-label
  generation-retract in the canonical node writer and risk deleting live
  `K8sResource` nodes, so no node ever carries the `CrossplaneClaim` label.
  `cypher.CrossplaneSatisfiedByEdgeWriter` MERGEs the edge when a
  `K8sResource` row's (group, kind) — derived from its `api_version`/`kind`,
  not a parse-time label — resolves against exactly one `CrossplaneXRD`'s
  (`spec.group`, `spec.claimNames.kind`); a zero-match row is an ordinary
  Kubernetes object and a 2+ match is ambiguous, and both produce no edge
  (see [Edge Source-Tool Provenance](../reference/edge-source-tool-provenance.md)).
  `POST /api/v0/impact/blast-radius` with `target_type: crossplane_xrd`
  reports `complete: true` with `SATISFIED_BY` listed as `materialized: true`
  in `coverage` (see
  [HTTP API reference](../reference/http-api/iac-content-infra.md)).
- Cross-scope XRD-lag false-negative window (closed, issue #5476): the
  SATISFIED_BY correlation is intentionally ungated within a repo (the Claim
  and any same-repo XRD are projector-canonical nodes committed before the
  correlation intent is enqueued, so no same-scope race exists), but a Claim
  resolving against an XRD from a *different* repo depends on that platform
  repo's XRD already being active by the time the Claim's own generation runs
  its correlation. If the XRD's repo is ingested for the first time *after*
  the Claim repo's latest generation, the Claim's own materialization pass
  finds zero XRD facts — a false negative (the edge is real but not yet
  observable), not a wrong answer. This no longer requires the Claim repo to
  produce a new generation to close: when a scope's generation carrying an
  active `CrossplaneXRD` activates,
  `postgres.CrossplaneSatisfiedByRedriveSweeper` durably discovers every OTHER
  scope with an active, unresolved `K8sResource` Claim matching that XRD's
  `(group, claimNames.kind)` and re-enqueues (or reopens) that scope's
  SATISFIED_BY materialization intent — without re-ingesting the Claim repo.
  The sweep is bounded (only scopes with a matching Claim, keyset-paginated,
  index-backed — see `fact_records_active_k8s_claim_redrive_idx`) and durably
  resumable (`crossplane_satisfied_by_redrive_state` records completion per
  XRD source-generation, so a crash mid-sweep retries safely and a completed
  generation is never re-swept); `cmd/projector`'s
  `runCrossplaneRedriveCatchUpLoop` periodically reclaims a sweep left
  incomplete by a transient error or a crashed process, since the live
  post-activation trigger alone cannot recover one. See
  `go/internal/storage/postgres/crossplane_satisfied_by_redrive_sweep.go` for
  the implementation and `go/internal/storage/postgres/README.md` for the
  fencing and trigger design. This does not contradict the blast-radius
  `coverage` fields: `materialized:true`/`complete` report that a writer
  produces the SATISFIED_BY edge type (the writer-existence contract from
  #5330), not that every possible edge instance is currently present — the
  same eventual-consistency property every cross-repo correlation edge (e.g.
  RUNS_IMAGE, which still carries this same unclosed gap) has, since no
  correlation edge can form until both endpoints are ingested.
