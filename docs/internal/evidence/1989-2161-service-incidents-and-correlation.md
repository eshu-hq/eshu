# Service Incidents Evidence Family And Durable Incident-Repository Correlation (#1989, #2161)

Moved from `go/internal/reducer/README.md` (issue #5786) to keep the
package README under the repository's 500-line cap. Content is
unchanged from the original section.

## Service incidents evidence family (#1989)

The incidents evidence family extends the same service materialization lineage
(#1943) without a new table, index, or reducer domain. Like the docs family it is
keyed by SERVICE id, not repository id. Its source is exact PagerDuty
incident-routing evidence: each correlated service's routing slots
(`intended_routing` / `applied_routing` / `live_routing`) become one
incidents-family `service_evidence_snapshots` row per slot in the same service
generation as ownership, deployment, runtime, dependencies, and docs. When both
`MaterializationWriter` and `IncidentEvidenceLoader` are wired,
`ServiceCatalogCorrelationHandler` loads the correlated services' routing evidence
in ONE bounded `GetIncidentEvidenceForServices` call.

The row identity is `ServiceIncidentEvidenceKey(service_id, identity)` =
`incidents:<service_id>:<provider>:<provider_incident_id>:<slot>:<evidence_kind>:<evidence_id>`.
`provider`, `provider_incident_id`, `slot`, and `evidence_kind` are durable. The
`evidence_id` is the source fact's generation-INDEPENDENT `StableFactKey` (for the
applied/live routing facts) or the durable content-entity id (for the intended
slot). It is deliberately NOT the routing graph row's `evidence_id`, which is the
envelope `FactID`: both `collector/terraformstate/pagerduty_applied.go` and
`collector/pagerduty/config_envelope.go` build `FactID =
StableID(..., {..., generation_id})`, and the collector envelope tests assert
`FactID` differs across generations while `StableFactKey` does not — so keying on
`FactID` would report 100% false churn. The applied fact's `state_generation_id`
(per-run metadata) is excluded from both the key and the hashed payload. A record
missing any durable identity component is dropped rather than keyed on a
generation-bearing or empty identity.

Production loader WIRED (correlation truth). The production
`IncidentEvidenceLoader` is wired in `cmd/reducer` through
`postgres.ServiceIncidentEvidenceLoader`. It resolves PagerDuty provider service
ids to Eshu catalog service ids only through durable reducer facts:
`reducer_service_catalog_correlation` maps catalog service id to repository id,
and `reducer_incident_repository_correlation` maps provider service id to
repository id. Both joins admit only active, non-tombstoned `exact` / `derived`
rows with `provenance_only=false`, and a repository must map to exactly one active
catalog service before any incident evidence is attached. Repositories with
multiple active catalog service ids fail closed, and name fingerprints or service
names are never used for attribution.

The loader returns applied and live routing slots from active
`incident_routing.applied_pagerduty_resource` and
`incident_routing.observed_pagerduty_service` facts whose provider object id
matches the incident's provider service id. It returns the source fact's
generation-independent `stable_fact_key` as the incidents `evidence_id`, not the
generation-bearing `fact_id`. The intended/declared routing slot remains absent
from production service evidence until it has a durable provider-service-id to
catalog-service-id attribution path; this preserves correlation truth rather than
promoting name-only declarations.

No-Regression Evidence: `go test ./internal/reducer -run
'ServiceIncident|BuildServiceIncident|ServiceMaterialization|ServiceCatalogHandler'
-count=1` proves incidents rows carry generation-stable durable-identity keys (no
embedded `fact_id`/`generation_id`/`state_generation_id`), the same durable
routing row keys identically across generations (anti-churn cross-generation
stability), a changed payload hash flips the generation while an identical
re-materialization is a no-op, dropped rows are tombstoned, records without a
complete durable identity are dropped, and the family is purely additive (no
loader leaves the generation without incidents rows and the prior families' tests
stay green). `go test ./internal/storage/postgres -run
'TestServiceIncidentEvidence|TestComputeServiceChangedSinceDelta' -count=1`
proves the production Postgres loader is bounded by requested service ids, active
generations, exact/derived non-provenance correlations, unambiguous repository
ownership, and StableFactKey evidence identity; the family-generic delta SQL
(grouped by `evidence_family`) classifies incidents
added/updated/unchanged/retired/superseded with bounded ordered samples, reports
no false incident deltas for an unchanged generation, and reports zero deltas for
the other families on an incidents-only change. The incidents rows reuse the
existing `service_evidence_snapshots_diff_idx` (`generation_id`, `evidence_family`,
`service_evidence_key`) for changed-since diffs, and the production loader adds
narrow partial read indexes for service-catalog service ids, incident repository
correlations, incident anchors, and applied/live provider-service routing facts.
Live-Postgres SQL is proven by the same fake `Queryer`/`Rows` harness Stage-1
uses; the live SQL gate is the Postgres integration suite in CI.

Observability Evidence: this slice adds no new metric instrument, label, queue
domain, lease, or runtime knob. The incidents family lands inside the existing
`service_catalog_correlation` reducer execution span/counter and the same service
materialization commit path Stage-1 already instruments; operators diagnose it
through reducer run spans, execution counters, `fact_work_items` status/failure
fields, the durable `service_materialization_generations`/
`service_evidence_snapshots` rows, and the `get_service_changed_since`
API/MCP/CLI read surface that now reports the incidents family alongside the prior
five.

## Durable incident → repository correlation (#2161)

`DomainIncidentRepositoryCorrelation` is the prerequisite durable edge for
scoped-token incident-context reads (#2144). It correlates a PagerDuty incident
to its owning config repository through a **structural** join, never the
PagerDuty service name. The chain is durable end to end:
`incident.Service.ID` → an `incident_routing.applied_pagerduty_resource` fact
whose `provider_object_id` equals that id → that fact's Terraform
`(backend_kind, locator_hash)` → the single owning config repository resolved by
the shared `tfstatebackend.Resolver`. The same backend-locator resolver the
config/state and cloud-runtime drift domains use anchors ownership, so every
backend-ownership consumer agrees.

The handler reuses the exact/derived/ambiguous/rejected discipline of
`service_catalog_correlation`. `BuildIncidentRepositoryCorrelations` groups
applied rows by provider service id and resolves each distinct backend locator
once (memoized). Only a single owning repository across an unambiguous backend
emits a durable edge: `exact` when the provider id matched by raw equality,
`derived` when it matched after normalization. A blank provider id
(name-fingerprint only) is `rejected`, the same provider id applied under two
distinct backend locators or claimed by more than one repo is `ambiguous`
(fork/mirror disagreement never false-merges), and a backend no Eshu repo owns is
`unresolved`. Every non-edge outcome stays `provenance_only` with no
`repository_id`, so the downstream #2144 scoped predicate is fail-closed: an
incident with no durable repository edge is not visible to a scoped token. The
writer persists `reducer_incident_repository_correlation` facts keyed
deterministically by `(scope, generation, provider, provider_service_id)`, so
retries and an `exact`→`ambiguous` flip converge on one row via
`ON CONFLICT (fact_id) DO UPDATE`. The domain is additive: it registers only when
both `AppliedPagerDutyServiceRoutingLoader` and
`IncidentRepositoryCorrelationWriter` are wired, and a nil
`BackendRepositoryResolver` leaves every correlation unresolved (no edge).

Performance Evidence: this is a hot-path reducer domain.
Baseline: no prior implementation (new additive domain; the prior #2144/#2161
investigation found no durable join and shipped nothing on this path).
After: `go test ./internal/reducer -run '^$' -bench
BenchmarkBuildIncidentRepositoryCorrelations -benchmem -count=1` on Apple M4 Pro
(go1.26, arm64) classifies 512 applied service rows fanned over 4 distinct
backend locators in ~168µs/op, 279KB/op, 2583 allocs/op. Input shape: 512 rows,
512 distinct provider service ids, 4 distinct `(backend_kind, locator_hash)`
keys. Terminal counts: 512 decisions, with the `stubBackendRepositoryResolver`
consulted exactly once per distinct backend locator (asserted by
`TestBuildIncidentRepositoryCorrelationsMemoizesResolver`), so resolver load —
the only cross-scope query in the path — scales with distinct backends, not with
incident count. The classification is O(rows) with map-bounded resolution; it
runs once per `incident_repository_correlation` intent inside the existing
reducer worker pool, adding no new lease, queue domain, or batch knob.

No-Regression Evidence: `go test ./internal/reducer ./cmd/reducer
./internal/storage/postgres -count=1` and `go test ./internal/reducer -race -run
'IncidentRepositoryCorrelation|BuildIncidentRepositoryCorrelations' -count=1`
prove exact/derived edges, name-only rejection, unresolved/ambiguous/fork-mirror
no-edge, resolver memoization, deterministic idempotent fact ids, loader null
handling, and the resolver adapter's no-owner/ambiguous-owner/single-owner
translation. No existing reducer projection path is modified, so the touched
hot-path packages' prior suites stay green.

Observability Evidence: `eshu_dp_incident_repository_correlations_total` is
labeled by bounded `domain` and `outcome`
(exact/derived/ambiguous/unresolved/rejected) only — never repo ids, provider
service ids, or backend locators. The handler `EvidenceSummary` records
`evaluated`, per-outcome counts, and `facts_written` so an operator can see at
3 AM how often a confident tenant-safe edge was found versus how often routing
stayed provenance-only, alongside the existing reducer run span,
`fact_work_items` status/failure fields, and the durable
`reducer_incident_repository_correlation` rows.
