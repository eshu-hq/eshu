# internal/reducer/incident

## Purpose

Owns the PagerDuty incident-routing reducer family: exact graph-evidence
materialization for the declared/applied/observed PagerDuty routing of one
scope generation (issue #2161), and the durable incident-to-repository
correlation that resolves an applied PagerDuty service to its owning config
repository through the Terraform backend-locator join.

It exists as its own package (issue #6061, epic #6053) so the reducer root's
file count keeps shrinking toward the repo's dirgate cap. Every symbol the
family needed from outside was already a leaf-package forwarder before this
move: `derefString`/`anyToString`/`uniqueSortedStrings` resolve to
`payloadcore`, `PriorGenerationCheck`/`Intent`/`Result`/the `Domain*`
constants resolve to `contract`, the quarantine/decode helpers resolve to
`factdecode` and `schemadecode`, and the batch-insert writer primitives
resolve to `factwrite`. The two `DomainDefinition` builders
(`incidentRoutingMaterializationDomainDefinition`,
`incidentRepositoryCorrelationDomainDefinition`) stayed in the reducer root's
`registry_additive_domains.go`, matching every other additive family
(`configStateDriftDomainDefinition`, `eshuSearchDocumentDomainDefinition`):
the root registry owns catalog metadata regardless of where a family's
handler lives.

## Ownership boundary

**Owns:** `IncidentRoutingMaterializationHandler` and
`IncidentRepositoryCorrelationHandler` (the two intent handlers),
`IncidentRoutingEvidenceLoader`/`IncidentRoutingEvidenceWriter` and
`AppliedPagerDutyServiceRoutingLoader`/`BackendRepositoryResolver`/
`IncidentRepositoryCorrelationWriter` (their ports),
`BuildIncidentRepositoryCorrelations` (the pure classification builder),
`ExtractIncidentRoutingEvidenceRows` (the pure graph-row projector), the
typed fact-payload decode seam for the four incident/incident_routing fact
kinds, and `PostgresIncidentRepositoryCorrelationWriter` (the Postgres writer
for correlation decisions).

**Does not own:** the reducer registry (`DomainDefinition` catalog metadata
stays in the reducer root, see Purpose above), the `IncidentRoutingEvidence`
graph writer (`internal/storage/cypher.IncidentRoutingEvidenceWriter`), the
applied-routing Postgres loader or the backend-owner resolver adapter
(`internal/storage/postgres.PostgresAppliedPagerDutyServiceRoutingLoader` /
`BackendRepositoryResolverAdapter`), or the Terraform backend-locator
resolution itself (`internal/relationships/tfstatebackend.Resolver`). All are
ports this package calls through, not logic it duplicates.

## Exported surface

| symbol | what it is |
|---|---|
| `IncidentRoutingMaterializationHandler` | the reducer intent handler for `incident_routing_materialization` |
| `IncidentRoutingEvidenceLoader` / `IncidentRoutingEvidenceWriter` | ports: raw evidence load, graph write/retract |
| `IncidentRoutingRawEvidence` / `IncidentRoutingEvidenceInput` | the raw and decoded evidence shapes |
| `ExtractIncidentRoutingEvidenceRows` | pure projector: decoded evidence -> graph rows |
| `IncidentRepositoryCorrelationHandler` | the reducer intent handler for `incident_repository_correlation` |
| `AppliedPagerDutyServiceRoutingLoader` / `BackendRepositoryResolver` / `IncidentRepositoryCorrelationWriter` | ports: applied-routing load, backend-to-repo resolve, correlation write |
| `BuildIncidentRepositoryCorrelations` | pure classifier: applied routing rows -> correlation decisions |
| `PostgresIncidentRepositoryCorrelationWriter` | the Postgres implementation of the correlation writer port |

The reducer root wires `IncidentRoutingHandlers`' five fields to this
package's interfaces (`defaults_handlers.go`) and constructs
`IncidentRoutingMaterializationHandler` / `IncidentRepositoryCorrelationHandler`
directly in `defaults_additive_domains_incident_code.go`. `cmd/reducer`
constructs the concrete `PostgresIncidentRepositoryCorrelationWriter`
(`main_helpers.go`'s `incidentRepositoryCorrelationWiring`).

## Dependencies

`internal/facts` and the generated `sdk/go/factschema` / `incident/v1`
packages (fact-kind identity and typed payload decode),
`internal/reducer/contract` (the `Intent`/`Result`/`Domain` shapes and the
`PriorGenerationCheck` callback, aliased `reducercontract`),
`internal/reducer/factdecode` (per-fact quarantine classification and the
visible dead-letter emitter), `internal/reducer/schemadecode` (the four
incident/incident_routing typed fact decoders), `internal/reducer/factwrite`
(the batch-insert primitives), `internal/reducer/payloadcore` (pointer-deref
and dedup/sort helpers), `internal/telemetry` (the `Instruments` counters
below), and `internal/truth` (the source-layer vocabulary). No dependency on
the reducer root, and none of the root's other family subpackages.

## Telemetry

`eshu_dp_incident_routing_evidence_total` (labeled by domain, outcome, source
class, and slot kind — declared/applied/observed evidence projection
outcomes) and `eshu_dp_incident_repository_correlations_total` (labeled by
domain and outcome — durable incident-routing-to-repository correlation
decisions), both `metric.Int64Counter` fields on `telemetry.Instruments`
(`internal/telemetry/instruments.go`). Writer execution is covered by the
reducer's standard `eshu_dp_reducer_executions_total` /
`eshu_dp_reducer_run_duration_seconds`. Quarantined per-fact dead-letters
route through `factdecode.RecordQuarantinedFacts`
(`eshu_dp_reducer_input_invalid_facts_total`), unchanged by this move. Same
metric names, same emission sites, same instrument struct — only the package
that owns the handler code moved.

## Gotchas / invariants

- **The graph contract is narrower than the incident-context read model.**
  Only full declared/applied/observed exact convergence, or exact live-only
  no-IaC evidence, is materialized as a graph row; every other combination
  (drifted, stale, permission-hidden, ambiguous, unresolved, rejected,
  derived, missing) stays provenance-only and is counted in
  `incidentRoutingProjectionTally`, never silently dropped.
- **Correlation identity deliberately excludes the resolved repository id.**
  `incidentRepositoryCorrelationIdentity` keys on
  (scope, generation, provider, provider_service_id) so an exact -> ambiguous
  outcome flip for the same provider service id updates the same durable row
  via `ON CONFLICT DO UPDATE` rather than appending a stale duplicate.
- **The backend resolver is memoized per distinct (backend_kind,
  locator_hash), not per row or per provider service id.**
  `resolveDistinctBackends` consults `BackendRepositoryResolver` exactly once
  per distinct backend locator even when many provider services share it —
  do not reintroduce a per-row resolve.
- **A provider id applied under two disagreeing backend locators is itself
  ambiguous.** `groupAppliedRoutingByProviderService` blanks the candidate's
  locator on disagreement so classification forces `ambiguous` rather than
  picking one of the two backends arbitrarily (the fork/mirror no-false-merge
  case).
- **This package's test doubles for the shared batch-insert primitives are a
  scoped, package-local copy**
  (`incident_repository_correlation_writer_batch_test_helpers_test.go`), not
  the reducer root's `reducer_fact_batch_insert_test_helpers_test.go` /
  `fakeWorkloadIdentityExecer` (`workload_identity_writer_test.go`). Those
  root helpers are still shared by other families that have not moved out of
  the root yet; duplicating rather than sharing avoided touching a file
  several other in-flight #6061 moves also depend on. Keep the two shapes
  structurally identical if the wire shape (`factwrite.BatchInsertQuery`'s
  argument order/count) changes.
- **No import of the reducer root, ever.** This package is a leaf below
  `internal/reducer`: the root imports it for `IncidentRoutingHandlers`
  wiring and handler construction, never the reverse.

No-Regression Evidence: #6061 relocates
`incident_repository_correlation.go`, `incident_repository_correlation_build.go`,
`incident_repository_correlation_writer.go`, `incident_routing_evidence_decode.go`,
`incident_routing_evidence_rows.go`, `incident_routing_materialization.go`, and
`incident_routing_types.go` (plus their four existing test files) from the
reducer root into this package with no logic change: every function body is
byte-identical except for qualifying the now-external `payloadcore`,
`reducercontract`, `factdecode`, `schemadecode`, and `factwrite` symbols the
root previously exposed as unqualified forwarders or aliases. A pure
package-boundary move with unchanged bodies cannot regress runtime behavior.
All of the following exited 0 against the moved code:

- `go build ./...`
- `go vet ./...`
- `go test ./internal/reducer/... -count=1`
- `go test ./cmd/reducer ./internal/storage/postgres ./internal/query -count=1`

The existing handler, builder, and writer test suites
(`TestIncidentRoutingMaterializationHandlerWritesAndRetracts`,
`TestBuildIncidentRepositoryCorrelations*`,
`TestPostgresIncidentRepositoryCorrelationWriterPersistsBatchedFacts`, and
the input-invalid quarantine regressions) moved with the code unmodified and
still pass.

No-Observability-Change: #6061 adds no queue domain, worker, lease, graph or
Postgres operation, runtime setting, metric instrument, metric label, span,
log field, or status surface. `eshu_dp_incident_routing_evidence_total` and
`eshu_dp_incident_repository_correlations_total` keep their names, labels,
and emission sites on the unchanged `telemetry.Instruments` struct; only the
Go package that owns the emitting code moved.

## Related docs

- `go/internal/reducer/README.md` — the root package and its subpackage inventory
- `docs/internal/design/package-restructure.md` — the #6061 restructure
- `docs/public/observability/telemetry-coverage.md` — the coverage rows for these domains
