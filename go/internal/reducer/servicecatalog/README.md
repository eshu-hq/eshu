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
| corpus-fenced relationship read | `service_catalog_correlation_corpus_fence.go` | `attachServiceRelationshipEvidence` and the `ServiceCatalogCorrelationResolutionNotReadyFailureClass` deferral (#7258) |
| runtime evidence | `service_materialization_runtime.go` | `ServiceRuntimeEvidenceKey` over the durable platform/environment/workload identity |
| runtime readiness fence | `service_catalog_correlation_runtime_fence.go` | `checkServiceRuntimeReadiness`, the `RuntimeProducerReadiness` port, and the `RuntimeNotReadyFailureClass` deferral (#7816) |
| docs evidence | `service_materialization_docs.go` | `ServiceDocumentationEvidenceKey` over exact documentation-entity mentions |
| incidents evidence | `service_materialization_incidents.go` | `ServiceIncidentEvidenceKey` over exact PagerDuty routing evidence |
| vulnerabilities evidence | `service_materialization_vulnerabilities.go` | `ServiceVulnerabilityEvidenceKey` over supply-chain advisory findings |
| `PostgresServiceMaterializationWriter` | `service_materialization_writer.go` | commits the generation + snapshot rows for all seven families atomically |

The lineage conflict key is `(ScopeID, ServiceID)` (#6475):
`ServiceMaterializationWrite.ScopeID` is required and carries the claimed
intent's ingestion scope (`Intent.ScopeID`), `ServiceMaterializationGenerationID`
folds it into the generation identity, and the writer's insert, supersede, and
activate statements are all scoped by it. Two ingestion scopes that correlate
the same service id therefore keep separate active generations; a changed write
in one scope supersedes only that scope's prior generation, and unattributed
legacy rows (`scope_id IS NULL`) are never superseded.

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
| `CorpusFencedResolvedRelationshipLoader` | the deployment/dependencies shared, corpus-fenced relationship loader contract (locally redeclared, see below) |
| `ServiceCatalogCorrelationResolutionNotReadyFailureClass` | the non-counting readiness class of the corpus-fence deferral (#7258) |
| `RuntimeProducerReadiness` / `RuntimeProducerState` / `RuntimeProducerDomains` | the runtime fence's producer readiness port, its per-repository state, and the enumerated producer domain set (#7816) |
| `RuntimeSharedIntentProducerDomains` | the shared-intent projection domains whose pending RUNS_ON intents also hold the fence: `repo_dependency` (#8169) |
| `RuntimeNotReadyFailureClass` | the non-counting readiness class of the runtime fence deferral (#7816) |
| `RuntimeReadinessReady` / `NoProducer` / `Deferred` / `BoundExceeded` | the closed outcome set of `eshu_dp_service_catalog_runtime_readiness_total` |

## Package boundary

Imports point strictly downward. This package reaches `reducer/contract`,
`reducer/factload`, `reducer/factdecode`, `reducer/factwrite`,
`reducer/payloadcore`, `reducer/schemadecode`, `reducer/packages/source`,
`reducer/crossscope` (the #7816 runtime fence's elapsed bound, and the
cycle anchor its log line reports), `internal/facts`, `internal/relationships`, `internal/telemetry`,
`internal/truth`, and the factschema SDK, and it never imports the parent
`internal/reducer` package. The dependency runs the other way: the root keeps
compatibility aliases in the service-catalog stanza of `compat_correlation.go` so its own
callers, plus `cmd/reducer` and `internal/storage/postgres`, compile
unchanged.

`CorpusFencedResolvedRelationshipLoader` is declared locally rather than
imported from the root, which owns the canonical version
(`correlated_workload_projection_input_loader.go`) shared by the workload and
deployable-unit families. Go interfaces are structural, so the same concrete implementation
root wires in elsewhere satisfies this local declaration too, without
duplicating any logic — the established precedent is
`internal/reducer/code/taint/graph_ports.go`.

`exactPackageSourceURLMatch`/`normalizePackageSourceExactURL` moved to
`packages/source` as `ExactURLMatch`/`NormalizeExactURL`, alongside the
sibling `CanonicalURLKey` canonicalizer that family already owned; the root
keeps a one-line forwarder for its own remaining caller in
`packages/correlation/source.go`.

The service-catalog-correlation fact-kind string moved to
`contract.ServiceCatalogCorrelationFactKind` so both this package and the
still-in-root `supply_chain_impact` family can name it without either
importing the other.

## Telemetry

| stage | instrument | labels |
|---|---|---|
| correlation decisions | `eshu_dp_service_catalog_correlations_total` | `domain`, `outcome` |
| guardrail events | `eshu_dp_service_catalog_correlation_guardrails_total` | `domain`, `guardrail` |
| runtime readiness fence (#7816) | `eshu_dp_service_catalog_runtime_readiness_total` | `domain`, `outcome` (`ready`, `no_producer`, `deferred`, `bound_exceeded`) |

The fence also logs, through the reducer's structured readiness logger
(`Logger`, wired from `DefaultHandlers.CrossScopeReadinessLogger`), each
deferral at info ("service catalog correlation
deferred: workload materialization in flight for a correlated repository")
and each bound overrun at warn, with `scope_id`, `generation_id`,
`failure_class`, `runtime_readiness_outcome`, `in_flight_repo_count`,
`in_flight_repo_ids` (first 10), `producer_pending`, `producer_retrying`,
`producer_claimed`, `producer_running`, `producer_elapsed` (since the oldest
in-flight repository's workable anchor; negative while that anchor lies in
the future, during a retry backoff or a live lease), `producer_max_wait`,
`producer_newest_claim_until` (the latest in-flight lease end, present only
while some producer row holds a lease), and the catalog intent's
`elapsed_since_cycle_start` (context only; it does not decide). It never logs
`attempt_count`, which the non-counting class freezes.

Since #8169 the fence also waits on pending `repo_dependency` RUNS_ON intents
(`RuntimeProducerState.RunsOnIntentsPending`), the second writer of the
runtime surface. Its bound counts from the newest pending intent's
`created_at`; a repository with both kinds in flight counts from the later
anchor. The defer and bound lines add `runs_on_intents_pending` and
`producer_kind`. A generation committed at `no_producer` or `bound_exceeded`
is repaired by the `workload_materialization` ACK, which reopens the
succeeded catalog rows whose decisions name its repository and flags any
claimed or running row at the active generation; see
[the design note](../../../../docs/internal/design/8169-service-catalog-runtime-retrigger.md).

The bound is per in-flight repository. Each repository's elapsed time runs
from its newest in-flight producer row's last-workable time:
`GREATEST(COALESCE(reopened_at, created_at), COALESCE(last_attempt_at,
created_at), COALESCE(visible_at, created_at), COALESCE(claim_until,
created_at))`, the latest of the row's cycle start, last claim, scheduled
visibility, and lease expiry. `claim_until` is non-NULL only on claimed and
running rows: a claim sets it, a heartbeat extends it, and retry, ack, fail,
and dead-letter set it NULL. The bound never runs from `updated_at` and never
from the catalog row's age (which also counts queue wait and corpus-fence
wait). A producer row that is polling a non-counting readiness class, such as
its own corpus fence (`workload_materialization_resolution_not_ready`), or
whose worker heartbeats, refreshes its anchor and never counts as exceeded.

`bound_exceeded` fires only when every in-flight producer row of every correlated repo has had no claim, no heartbeat, no scheduled visibility, and no cycle start for 30 min (starved pending/retrying row, or a dead worker's lease expired 30 min ago). A heartbeating producer never ages out; a handler that heartbeats forever without finishing holds the catalog, which is the reducer lease policy's gap, tracked in #8211.
The handler then commits what it reads, possibly an empty family. A reopened
producer row starts a fresh 30 minutes, and a second owner that reclaims an
expired lease defers the catalog again.

Facts rejected for a malformed payload feed the shared
`eshu_dp_reducer_input_invalid_facts_total` counter through `factdecode`
instead of a family-specific one, and the reducer executions that run this
handler stay covered by `eshu_dp_reducer_executions_total` and
`eshu_dp_reducer_run_duration_seconds`. The service-materialization evidence
families (deployment, dependencies, runtime, docs, incidents,
vulnerabilities) register no instrument of their own.

## Gotchas / invariants

- **Runtime evidence is read only behind the runtime fence (#7816).**
  `prepareServiceMaterializations` runs the corpus fence, then
  `checkServiceRuntimeReadiness`, then `attachServiceRuntimeEvidence`, all
  before any write. Never move the runtime read back after a write or ahead
  of the check: a check after the read lets a producer that commits in
  between pass the fence with an empty read in hand. The reducer default
  wiring (`serviceCatalogRuntimeInstanceLoader`) wires no runtime loader
  without the fence.
- **The producer set is enumerated from source.**
  `TestRuntimeProducerSetCoversEveryRuntimeWriter` fails when a new file
  writes a `WorkloadInstance` node or `RUNS_ON` edge and is neither a fenced
  producer domain nor classified with a reason. The repo_dependency shared
  projection's `RUNS_ON` writer (`storage/cypher/canonical_relationships.go`,
  evidence source `resolver/cross-repo`, queued on
  `shared_projection_intents`) is classified but not fenced: a catalog read
  while that intent is still pending can miss a platform row. Repairing a
  generation committed on partial input is #8169.

- **Do not import the reducer root from here.** If this package needs a
  symbol the root defines, hoist it to a shared-core tier
  (`payloadcore`/`contract`/`packages/source`/etc.) with a root forwarder,
  or — if the symbol is genuinely root-owned and shared by other still-in-root
  families — redeclare a structurally identical interface locally, the way
  `CorpusFencedResolvedRelationshipLoader` does here.
- **Service relationship evidence is read only through the corpus fence.**
  `Handle` runs the fused `GetResolvedRelationshipsForReposWithCorpusFence`
  read before any write; an incomplete verdict returns the
  `service_catalog_correlation_resolution_not_ready` deferral with nothing
  written (#7258). The unfenced by-repos read omits a retired-or-pending
  scope's rows, and a service generation built from it supersedes the prior
  one with spurious removed evidence that no producer reopens. Never wire an
  unfenced loader or move the read after a write.
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
