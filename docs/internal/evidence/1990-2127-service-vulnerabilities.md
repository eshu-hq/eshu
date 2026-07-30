# Service Vulnerabilities Evidence Family (#1990, #2127)

Moved from `go/internal/reducer/README.md` (issue #5786) to keep the
package README under the repository's 500-line cap. Content is
unchanged from the original section.

## Service vulnerabilities evidence family (#1990, #2127)

The vulnerabilities evidence family extends the same service materialization
lineage (#1943) without a new table, index, or reducer domain. Each row is one
durable advisory-affects-package pair for a service. The row identity is
`ServiceVulnerabilityEvidenceKey(service_id, identity)` =
`vulnerabilities:<service_id>:<canonical_id>:<package_ecosystem>:<package_name>`.
The canonical advisory id (CVE/GHSA) and the affected package (ecosystem + name)
are durable external identities; observable fields (severity, KEV, EPSS,
source-confidence) are hashed into the payload so a change flips the row to
`updated`. The identity carries no evidence `fact_id` or per-scan generation id,
so an unchanged advisory hashes identically across re-materializations and
classifies as `unchanged` instead of churning every generation.

Production loader WIRED (correlation truth). Unlike the incidents family, the
vulnerabilities loader is repo-scoped, not service-scoped, and is wired in
production. The source is the durable `reducer_supply_chain_impact_finding` fact
(`supply_chain_impact_writer.go`), keyed by `repository_id` — the materialized,
dependency-derived impact a `DomainSupplyChainImpact` run already publishes. A
service is attributed an advisory ONLY when its repository carries a matching
impact finding: there is NO fuzzy advisory-to-service name match. The handler
resolves each correlated service to its repository through
`serviceRepositoryIndex(decisions)` (the same durable join the deployment,
dependencies, and runtime families use), loads all repositories' findings in ONE
bounded `ServiceVulnerabilityAdvisoryLoader.GetSupplyChainAdvisoriesForRepos`
call, and partitions the result per service by repository id. The Postgres
implementation (`storage/postgres/service_vulnerability_advisory_loader.go`)
reads active, non-tombstone findings filtered by `payload->>'repository_id'`,
paged by the `fact_id` keyset, and maps each finding to a
`ServiceVulnerabilityRecord` (canonical id = `cve_id` then `advisory_id`,
ecosystem, package name, primary advisory id, severity from
`provenance.selected_severity_label`, KEV from `known_exploited`, EPSS from
`epss_probability`, source confidence from `confidence`). A service whose
repository has no finding simply carries no vulnerabilities rows; a nil loader
leaves the generation without vulnerabilities rows, so the family stays additive.

No-Regression Evidence: `go test ./internal/reducer -run
'Vulnerab|BuildServiceVulnerability|ServiceMaterialization|ServiceCatalogHandler'
-count=1` and `go test ./internal/storage/postgres -run
'SupplyChainAdvisoriesForRepos' -count=1` prove: a service is attributed an
advisory only when its repository has a matching finding (positive,
`TestServiceCatalogHandlerCommitsVulnerabilitiesFamilyWhenWired` asserts the load
is bounded to exactly `[repo-checkout]`), a finding on a different repository is
never attributed (negative,
`TestServiceCatalogHandlerAttributesVulnerabilityOnlyViaRepositoryFinding`), the
repo-scoped load runs once and is bounded, the family is additive (a nil loader
leaves the generation without vulnerabilities rows and prior families' tests stay
green), an identical re-materialization is a no-op while a changed payload hash
flips the generation, retired rows are tombstoned, and records without a complete
durable identity are dropped. The finding read reuses the active-generation join
and the new partial index `fact_records_supply_chain_impact_repository_lookup_idx`
(`payload->>'repository_id'`, `fact_id ASC`, `generation_id`) under the
`reducer_supply_chain_impact_finding` + `is_tombstone = FALSE` predicate; the
vulnerabilities snapshot rows reuse the existing
`service_evidence_snapshots_diff_idx`. Baseline before: the vulnerabilities loader
was nil in `cmd/reducer`, so the family never populated in production. After: one
bounded per-intent repository-scoped Postgres read with no graph read or write.
Live-Postgres SQL is proven by the same fake `Queryer`/`Rows` harness Stage-1
uses; the live SQL gate is the Postgres integration suite in CI.

Observability Evidence: this slice adds no new metric instrument, label, queue
domain, lease, or runtime knob. The vulnerabilities family lands inside the
existing `service_catalog_correlation` reducer execution span/counter and the same
service materialization commit path Stage-1 already instruments; operators
diagnose it through reducer run spans, execution counters, `fact_work_items`
status/failure fields, the durable `service_materialization_generations`/
`service_evidence_snapshots` rows, and the `get_service_changed_since`
API/MCP/CLI read surface that now reports the vulnerabilities family alongside the
prior families. The new Postgres read is visible through the existing
`InstrumentedDB` query spans/duration metrics for the reducer store.
