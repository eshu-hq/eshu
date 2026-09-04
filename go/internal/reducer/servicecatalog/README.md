# servicecatalog

Correlates service-catalog entity declarations (Backstage-shaped
`catalog-info.yaml` facts) against canonical repository evidence without
letting catalog names create workloads, then commits the additive
per-service evidence-generation lineage that tracks what changed about a
service across ownership, deployment, runtime, dependencies, docs, incidents,
and vulnerabilities.

This package moved out of the flat `internal/reducer` root under issue #6061.
It is a domain family: it owns the `service_catalog_correlation` reducer
domain and the seven-family per-service materialization lineage built on top
of it, and nothing else in the reducer depends on its internals except
through the root's compatibility aliases.

## What it owns

| piece | file | what it does |
|---|---|---|
| `ServiceCatalogCorrelationHandler` | `service_catalog_correlation.go` | the reducer handler for `service_catalog_correlation` |
| `BuildServiceCatalogCorrelationDecisions` | `service_catalog_correlation.go` | joins catalog entity/ownership/repository-link facts into per-entity decisions |
| classification | `service_catalog_correlation_classify.go` | exact/derived/ambiguous/unresolved/stale/rejected outcome logic |
| guardrail counters | `service_catalog_correlation_guardrails.go` | candidate-fanout, dropped-ambiguous, and missing-anchor telemetry |
| `PostgresServiceCatalogCorrelationWriter` | `service_catalog_correlation_writer.go` | the Postgres correlation-decision writer |
| ownership evidence | `service_materialization.go` | `ServiceOwnershipEvidenceKey`, the shared payload-hash/normalization helpers, and `ServiceMaterializationGenerationID`'s deterministic fingerprint |
| deployment evidence | `service_materialization_deployment.go` | `ServiceDeploymentEvidenceKey` over the resolved deployment relationship's identity |
| dependencies evidence | `service_materialization_dependencies.go` | `ServiceDependencyEvidenceKey`, sharing deployment's resolved relationships |
| runtime evidence | `service_materialization_runtime.go` | `ServiceRuntimeEvidenceKey` over the durable platform/environment/workload identity |
| docs evidence | `service_materialization_docs.go` | `ServiceDocumentationEvidenceKey` over exact documentation-entity mentions |
| incidents evidence | `service_materialization_incidents.go` | `ServiceIncidentEvidenceKey` over exact PagerDuty routing evidence |
| vulnerabilities evidence | `service_materialization_vulnerabilities.go` | `ServiceVulnerabilityEvidenceKey` over supply-chain advisory findings |
| `PostgresServiceMaterializationWriter` | `service_materialization_writer.go` | commits the generation + snapshot rows for all seven families atomically |

The `service_materialization_*` files are **not** a separate family: their
handler methods (`attachServiceIncidentEvidence`,
`attachServiceVulnerabilityEvidence`, `attachServiceDocumentationEvidence`)
are declared on `ServiceCatalogCorrelationHandler` itself, and the
deployment/dependencies/runtime evidence builders feed the same handler
through its optional loader fields
(`DeploymentRelationshipLoader`/`RuntimeInstanceLoader`/etc.). The filename
prefix is a historical artifact of the flat root, not a package boundary.

## Exported surface

| symbol | what it is |
|---|---|
| `ServiceCatalogCorrelationDecision` / `Write` / `WriteResult` | the decision, publication input, and result records |
| `ServiceCatalogCorrelationHandler` / `Writer` | the reducer handler and its writer interface |
| `BuildServiceCatalogCorrelationDecisions` | the pure decision builder |
| `PostgresServiceCatalogCorrelationWriter` | the Postgres correlation-decision writer |
| `ServiceCatalogCorrelationOutcome` and its six constants (`Exact`/`Derived`/`Ambiguous`/`Unresolved`/`Stale`/`Rejected`) | the classification outcome vocabulary |
| `ServiceMaterializationWrite` / `WriteResult` / `Writer` | the per-service lineage commit input, result, and writer interface |
| `PostgresServiceMaterializationWriter` / `ServiceMaterializationTx` / `Row` / `Beginner` | the Postgres lineage writer and its narrow storage ports |
| `ServiceMaterializationStatusPending` / `Active` / `Superseded` | generation status values |
| `ServiceMaterializationGenerationID` | the deterministic evidence-fingerprint generation id, exported so the root's golden fixture test can assert distinctness/idempotency directly |
| `ServiceOwnershipEvidenceKey` / `ServiceEvidencePayloadHash` | the ownership stable-key and payload-hash helpers |
| `ServiceEvidenceFamilyOwnership`/`Deployment`/`Runtime`/`Dependencies`/`Docs`/`Incidents`/`Vulnerabilities` | the evidence-family label constants |
| `RepositoryScopedRuntimeInstanceLoader` / `ServiceRuntimeInstance` / `ServiceRuntimeEvidenceKey` | the runtime evidence loader contract and key derivation |
| `ServiceScopedDocumentationEvidenceLoader` / `ServiceDocumentationRecord` | the docs evidence loader contract |
| `ServiceScopedIncidentEvidenceLoader` / `ServiceIncidentRecord` | the incidents evidence loader contract |
| `ServiceVulnerabilityAdvisoryLoader` / `ServiceVulnerabilityRecord` | the vulnerabilities evidence loader contract |
| `RepositoryScopedResolvedRelationshipLoader` | the deployment/dependencies shared relationship loader contract (locally redeclared, see below) |

## Package boundary

Imports point strictly downward. This package reaches `reducer/contract`,
`reducer/factload`, `reducer/factdecode`, `reducer/factwrite`,
`reducer/payloadcore`, `reducer/schemadecode`, `reducer/packagesourcecore`,
`internal/facts`, `internal/relationships`, `internal/telemetry`,
`internal/truth`, and the factschema SDK, and it never imports the parent
`internal/reducer` package. The dependency runs the other way: the root keeps
compatibility aliases in `service_catalog_correlation_compat.go` so its own
callers, plus `cmd/reducer` and `internal/storage/postgres`, compile
unchanged.

`RepositoryScopedResolvedRelationshipLoader` is declared locally rather than
imported from the root, which owns the canonical version
(`workload_materialization_handler.go`) shared by several still-in-root
families. Go interfaces are structural, so the same concrete implementation
root wires in elsewhere satisfies this local declaration too, without
duplicating any logic — the established precedent is
`internal/reducer/codetaint/graph_ports.go`.

`exactPackageSourceURLMatch`/`normalizePackageSourceExactURL` moved to
`packagesourcecore` as `ExactURLMatch`/`NormalizeExactURL`, alongside the
sibling `CanonicalURLKey` canonicalizer that family already owned; the root
keeps a one-line forwarder for its own remaining caller in
`package_source_correlation.go`.

The service-catalog-correlation fact-kind string moved to
`contract.ServiceCatalogCorrelationFactKind` so both this package and the
still-in-root `supply_chain_impact` family can name it without either
importing the other.

## Telemetry

| stage | instrument | labels |
|---|---|---|
| correlation decisions | `eshu_dp_service_catalog_correlations_total` | `domain`, `outcome` |
| guardrail events | `eshu_dp_service_catalog_correlation_guardrails_total` | `domain`, `guardrail` |

Facts rejected for a malformed payload feed the shared
`eshu_dp_reducer_input_invalid_facts_total` counter through `factdecode`
instead of a family-specific one, and the reducer executions that run this
handler stay covered by `eshu_dp_reducer_executions_total` and
`eshu_dp_reducer_run_duration_seconds`. The service-materialization evidence
families (deployment, dependencies, runtime, docs, incidents,
vulnerabilities) register no instrument of their own.

No-Regression Evidence: #6061 relocates this family's production logic
without changing it. Every hunk inside the moved production files is
package-clause and import requalification: symbols the reducer root used to
supply as one-line forwarders or aliases (`Intent`, `Result`, `FactLoader`,
`workloadIdentityExecer`, `quarantinedFact`, the `reducerFact*`/
`reducerBatchInsert*` batch-insert family, `loadFactsForKinds`,
`recordQuarantinedFacts`, `compactStringSlice`, `uniqueSortedStrings`, the
`decodeServiceCatalog*` schema decoders, and `canonicalPackageSourceURLKey`)
are now imported from the leaf package that already owned them.
`RepositoryScopedResolvedRelationshipLoader` is locally redeclared, not
imported, for the reason above.
`exactPackageSourceURLMatch`/`normalizePackageSourceExactURL` (~19 lines of
real `net/url` normalization logic, not a forwarder) moved to
`packagesourcecore.ExactURLMatch`/`NormalizeExactURL`, with a root forwarder
left behind. `ServiceMaterializationGenerationID` was exported (from the
unexported `serviceMaterializationGenerationID`) because the root's own
golden fixture test names it directly; it keeps its exact fingerprint
algorithm unchanged. A Go import change, a structural interface
redeclaration, and an unexported symbol becoming exported add no indirection
or behavior change at runtime. Measured on this branch after the final edit:
`go build ./...` and `go vet ./...` both exit 0, and
`go test ./internal/reducer/... -count=1` passes, including this package.

No-Observability-Change: #6061 adds no queue domain, worker, lease, graph or
Postgres operation, runtime setting, metric instrument, metric label, span,
or log field. The two counters above and the reducer executions that wrap
them are the same before and after the move; only the file paths the
telemetry-coverage rows point at changed.

## Gotchas / invariants

- **Do not import the reducer root from here.** If this package needs a
  symbol the root defines, hoist it to a shared-core tier
  (`payloadcore`/`contract`/`packagesourcecore`/etc.) with a root forwarder,
  or — if the symbol is genuinely root-owned and shared by other still-in-root
  families — redeclare a structurally identical interface locally, the way
  `RepositoryScopedResolvedRelationshipLoader` does here.
- **The service-materialization files are this handler's own methods, not a
  sibling family.** Do not split them into a second package; they share
  `ServiceCatalogCorrelationHandler`'s optional loader fields and commit into
  the same generation as ownership.
- **A `_test.go` file's exports never cross a package boundary.** The root's
  own wiring tests (`defaults_cicd_test.go`,
  `service_runtime_instance_lookup_test.go`) that exercise this family's
  exported entry points end to end keep a trimmed local copy of the
  unexported test doubles in
  `service_catalog_correlation_root_test_doubles_test.go`; if you rename or
  reshape a fixture here that file's comment says it mirrors, check whether
  the root copy needs the same change.
- **`ServiceMaterializationGenerationID` is a durable fingerprint, not a
  display value.** Any change to which fields feed it, or their ordering,
  changes every service's generation id and defeats the idempotent
  re-materialization contract (`ON CONFLICT DO NOTHING` upserts by generation
  id).

## Related docs

- [Reducer package](../README.md)
- [Package restructure design](../../../../docs/internal/design/package-restructure.md)
- [Telemetry coverage](../../../../docs/public/observability/telemetry-coverage.md)
