# HTTP Evidence And Supply-Chain Routes

Use these routes when a client needs evidence packets, documentation truth,
package identity, CI/CD correlation, provider security alert reconciliation,
SBOM attachment state, or vulnerability impact.

## Deployment Evidence Pointers

Repository, workload, service, and deployment-trace responses may include
`deployment_evidence`. The object is compact by design: it returns grouped
pointers instead of embedding every Postgres evidence row.

- `artifacts[]` carries one inspectable deployment, CI, IaC, or config signal.
- `artifacts[].resolved_id` is the durable lookup key for the
  `resolved_relationships` row in Postgres.
- `artifacts[].generation_id` identifies the relationship generation that
  produced the row.
- `artifacts[].source_location` records `repo_id`, `repo_name`, `path`, and
  line range when the extractor emitted line data.
- `evidence_index.lookup_basis` is `resolved_id`.

## Relationship Evidence

`GET /api/v0/evidence/relationships/{resolved_id}`

Dereferences one deployment evidence pointer into the durable relationship
evidence row. The response includes lookup basis, source and target repository
metadata, relationship type, confidence, evidence count, evidence kinds,
rationale, generation metadata, `evidence_preview`, and decoded details.

Use this route when a client needs to explain why an edge exists without
embedding full evidence payloads in every graph response.

## Citation Packets

`POST /api/v0/evidence/citations`

Hydrates bounded file and entity handles into a reusable citation packet. Send
handles from story, investigation, search, or drill-down responses with
`repo_id + relative_path` for files or `entity_id` for entities.

The route accepts at most 500 input handles, hydrates at most 50 citations per
packet, preserves distinct line ranges and reasons for the same file, and
returns `coverage.truncated` when the caller should request another packet.
It reads the Postgres content store and does not traverse the graph.

## Documentation Truth

Documentation updater services should use these routes instead of reading graph
internals directly.

- `GET /api/v0/documentation/findings`
- `GET /api/v0/documentation/findings/{finding_id}/evidence-packet`
- `GET /api/v0/documentation/evidence-packets/{packet_id}/freshness`

`eshu docs verify` emits the same `documentation_finding` and
`documentation_evidence_packet` fact shapes that these routes expose after the
facts are persisted by a caller or data-plane runtime. Unsupported claim
families stay visible as `unsupported_claim_type`.

`GET /api/v0/documentation/findings` accepts filters for finding type, source,
document, status, truth level, freshness state, scope, generation, repository,
updated time, limit, and cursor.

`GET /api/v0/documentation/findings/{finding_id}/evidence-packet` returns the
bounded packet an external updater can snapshot before it plans a diff. Eshu
does not draft text or write documentation through this route.

`GET /api/v0/documentation/evidence-packets/{packet_id}/freshness` lets an
updater check whether a saved packet is stale before publishing a diff.

## Package Registry

Package registry routes expose identity materialized from package registry
facts. They do not claim repository ownership, publication ownership, or
runtime consumption truth unless reducer correlation admits that relationship.
Package and version responses include the normalized package identity plus
source-explanation fields: `purl`, `bom_ref`, `package_manager`,
`source_path`, and `source_specific_id` when the collector source supplies
them. Dependency responses expose the same identity shape for dependency
targets as `dependency_purl`, `dependency_bom_ref`, and `dependency_manager`.
Package list responses always include `identity_issues[]` for malformed graph
rows that cannot be returned as valid package identities. A blank package id is
classified with `reason=package_id_missing` and
`missing_evidence=["package_id"]`; valid scoped or unscoped npm identities with
`version_count=0` remain normal package rows. HTTP and MCP package-list reads
return the same response shape.

- `GET /api/v0/package-registry/packages`
- `GET /api/v0/package-registry/versions`
- `GET /api/v0/package-registry/dependencies`
- `GET /api/v0/package-registry/correlations`

`/packages` requires `limit` and either `package_id` or `ecosystem`. `name` may
narrow an ecosystem-scoped lookup.

`/versions` requires `package_id` and `limit`.

`/dependencies` requires `limit` and either `package_id` or `version_id`. When
both are provided, the version must belong to that package.

`/correlations` requires `limit` and either `package_id` or `repository_id`.
`repository_id` accepts a canonical source repository id or the same human
selectors repository context routes accept: repository name, repo slug, indexed
path, local path, or remote URL. Eshu resolves selectors before reading
reducer package-correlation facts; unknown or ambiguous selectors return a
selector error instead of an empty page.
`relationship_kind` can request ownership candidates, publication evidence, or
manifest-backed consumption correlations. Provenance-only rows remain marked
with `provenance_only=true`.

## CI/CD Run Correlation

`GET /api/v0/ci-cd/run-correlations`

Lists reducer-owned CI/CD run, artifact, and environment correlations. The
caller must provide `limit` and at least one bounded anchor:

- `scope_id`
- `repository_id`
- `commit_sha`
- `provider_run_id`
- `run_id`
- `artifact_digest`
- `image_ref`
- `environment`

When `provider_run_id` or `run_id` is the only anchor, callers must also
provide `provider` because provider-native run IDs are not globally unique.
CI success, environment observations, and shell-only deployment hints do not
become deployment truth by themselves.
`repository_id` accepts a canonical source repository id or the same human
repository selectors used by repository context routes. Eshu resolves selectors
before reading reducer CI/CD correlation facts for the list, count, and
inventory routes; unknown or ambiguous selectors return a selector error.
`image_ref` is also accepted by the list, count, and inventory routes so
target-story and MCP callers can prove tag-or-reference CI/CD evidence without
fetching a repository-wide page and filtering client-side.
Repository-scoped list responses also return `evidence_summary`:
`static_workflow_artifacts` reports indexed GitHub Actions workflow files from
the content read model, while `live_run_correlations` reports only
reducer-owned run correlation rows. When static workflow files are present but
the reducer has no live run rows, the response keeps `correlations=[]` and marks
`live_run_correlations.reason=static_workflow_only_live_run_correlation_missing`
instead of implying the repository has no CI/CD evidence.

No-Regression Evidence: `go test ./internal/query -run 'TestCICDListRunCorrelationsExplains(StaticWorkflowOnlyEvidence|LiveRunEvidence|NoEvidence)|TestOpenAPISpecIncludesCICDRunCorrelations' -count=1` fails if the CI/CD list response stops distinguishing static workflow artifacts from live reducer run rows.

No-Observability-Change: `evidence_summary` reuses the existing bounded CI/CD query span (`query.ci_cd_run_correlations`), repository-scoped content-store file lookup, Postgres/content query instrumentation, truth envelope, and HTTP status/error bodies. It adds no graph write, reducer work, queue, worker, metric instrument, metric label, or runtime knob.

No-Regression Evidence: `go test ./internal/query -run 'TestCICD(ListRunCorrelationsUsesImageRefAnchor|RunCorrelationQueryFiltersImageRef|RunCorrelationAggregate(Count|Inventory)PassesImageRefFilter)' -count=1` and `go test ./internal/mcp -run 'TestResolveRouteMapsCICDRunCorrelation' -count=1` failed before CI/CD list/count/inventory routes and MCP dispatch accepted `image_ref`, then passed after the bounded query predicates and tool schemas included it.

No-Observability-Change: `image_ref` reuses the existing CI/CD query handler spans (`query.ci_cd_run_correlations`, `query.ci_cd_run_correlation_aggregate`) and Postgres fact-read instrumentation; the change adds no worker, queue, graph write, metric instrument, or metric label.

## Vulnerability Impact

Vulnerability impact, scanner contract, findings, counts, inventory, explain,
scanner report, workload, and remediation plan routes live in
[Vulnerability Impact](vulnerability-impact.md). They remain source-evidence and
reducer-truth reads; provider alert reconciliation stays below because it
explains provider-only and stale rows separately from admitted impact truth.

## Provider Security Alert Reconciliation

`GET /api/v0/supply-chain/security-alerts/reconciliations`

Lists reducer-owned reconciliation rows for provider security alerts. The
caller must provide `limit` and at least one bounded anchor:

- `repository_id`
- `provider`
- `package_id`
- `cve_id`
- `ghsa_id`

`repository_id` accepts the canonical internal repository id plus the same human
selectors repository context routes accept: repository name, repo slug, indexed
path, local path, or remote URL. Unknown or ambiguous selectors return a
selector error before the reconciliation read model runs.
For security-alert reads, the resolved repository scope also includes the
provider repository identity when Eshu has it from the repository catalog. If
the catalog only has the repository name, Eshu can look up an exact provider
security-alert repository scope by that name; multiple provider scopes are
reported as an ambiguity instead of guessed. That keeps `provider_only` rows
visible for the selected repository while preserving their missing-evidence
status.
The count and inventory aggregate routes use the same repository selector
resolution before reading reducer-owned aggregate facts.

`provider_state` and `reconciliation_status` may narrow an anchored request,
but they are filters only and are rejected when sent without one of the anchors
above.

Rows keep `provider_alert`, `eshu_package`, and `eshu_impact` as separate
objects. Provider alert fields preserve reported alert ID/number, state,
dependency ecosystem and name, manifest path, dependency scope, relationship,
GHSA/CVE IDs, vulnerable range, patched version, severity, CVSS, EPSS, CWE,
timestamps, and sanitized source URL. `eshu_package.observed_version` is
populated only from Eshu-owned dependency evidence when exact installed-version
evidence exists; range-only or malformed version evidence remains explicit in
`eshu_package.missing_evidence`. Eshu impact fields only appear when the
reducer matched owned impact evidence. Valid reconciliation statuses are
`matched`, `unmatched`, `stale`, `dismissed`, `fixed`, `provider_only`,
`unsupported`, and `ambiguous`.

Each row also carries `reason_code` and may carry structured
`missing_evidence[]` objects. These details explain why provider-only, stale,
unsupported, or ambiguous rows did not become matched impact rows. Missing
evidence entries name a bounded `kind`, stable `reason`, and optional
`evidence_id`; they do not embed raw provider payloads, private repository
names, account names, hosts, or environment names.

A representative proof run can render the row-level reconciliation table
without exposing private source details:

| Package | Provider alert | Status | Eshu impact | Actionable reason |
| --- | --- | --- | --- | --- |
| `npm://registry.npmjs.org/left-pad` | `github_dependabot#1` | `matched` | `impact-matched` | Exact owned dependency and reducer impact evidence agree. |
| `npm://registry.npmjs.org/no-owned-evidence` | `github_dependabot#2` | `provider_only` | none | No owned dependency evidence is available for the provider alert. |
| `npm://registry.npmjs.org/left-pad` | `github_dependabot#3` | `stale` | none | Current manifest evidence no longer matches the provider alert path. |
| `pkg:unsupported/example` | `github_dependabot#4` | `unsupported` | none | The provider ecosystem is not supported by the current impact matcher. |
| `npm://registry.npmjs.org/ambiguous-name` | `github_dependabot#5` | `ambiguous` | none | Multiple owned dependency rows could match, so Eshu refused to guess. |

This route does not turn provider alert state into vulnerability impact truth.
Use `/api/v0/supply-chain/impact/findings` for reducer-owned impact findings.
Provider `severity` is returned inside `provider_alert`; it is not the same as
the impact `severity` scanner filter.

No-Regression Evidence: `go test ./internal/reducer ./internal/query ./internal/mcp -run 'TestBuildSecurityAlertReconciliationsExplains(TriageOutcomes|AmbiguousOwnedEvidence)|Test(DecodeSecurityAlertReconciliationRowPreservesTriageDetails|SupplyChainListSecurityAlertReconciliationsSurfacesTriageDetails|OpenAPISpecIncludesSecurityAlertReconciliations)|TestSecurityAlertReconciliationToolAdvertisesTriageFields' -count=1` failed before reconciliation rows carried structured triage details, then passed after provider-only, stale, unsupported, ambiguous, and matched rows exposed stable reason codes and missing-evidence details across reducer, HTTP, OpenAPI, and MCP.

No-Observability-Change: row-level triage reuses the existing reducer
execution telemetry, persisted reconciliation facts, `query.supply_chain_security_alerts`
span, and Postgres query timing. It adds no route, queue, worker, graph write,
metric instrument, metric label, or runtime knob.

No-Regression Evidence: `go test ./internal/query ./internal/mcp -run 'Test(VulnerabilityScannerReadContract|SupplyChainImpactFindingsAcceptsScannerContractFilters|SupplyChainImpactFindingsRejectsUnsupportedScannerFiltersBeforeStore|SupplyChainImpactAggregatesAcceptScannerContractFilters|SupplyChainImpactInventoryCanGroupByEcosystem|ResolveRouteMapsVulnerabilityScannerContract|SupplyChainImpactMCPRouteForwardsScannerContractFilters|SupplyChainImpactAggregateMCPRoutesForwardScannerContractFilters)' -count=1` covers the scanner read contract, bounded unsupported-filter failures, shared API/MCP filter forwarding, provider-only separation, and deterministic aggregate/list read semantics without graph traversal.

No-Observability-Change: the scanner contract route is static metadata and the
new filters reuse the existing HTTP/MCP truth envelope, readiness envelope,
limit/truncated fields, and bounded Postgres read-model errors; no reducer,
collector, worker, queue, graph write, metric, span, or log contract changes.

## SBOM And Attestation Attachments

`GET /api/v0/supply-chain/sbom-attestations/attachments`

Lists reducer-owned SBOM and attestation attachment facts. The caller must
provide `limit` and at least one bounded anchor: `subject_digest`,
`document_id`, or `document_digest`.

Rows expose `attachment_status`, `parse_status`, and `verification_status`
separately. Component evidence is returned as document evidence only; this
route does not emit vulnerability priority or affected-by findings.

Rows also expose `attachment_scope` and `missing_evidence` so callers can tell
image-attached evidence from parse-only corpus evidence. `image_subject` means
Eshu saw an OCI referrer tying the SBOM or attestation document to the subject
digest. `parse_only_unanchored` and `subject_only_unanchored` rows remain
visible for diagnostics, but they are not image impact evidence until an OCI
referrer proves the document is attached to the subject image.

No-Regression Evidence: `go test ./internal/reducer -run
'Test(BuildSBOMAttestationAttachmentDecisionsClassifiesSubjectsAndTrust|ScannerWorkerGeneratedSBOMFactsAdmittedByReducerAttachment|PostgresSBOMAttestationAttachmentWriterPersistsAllStatuses)'
-count=1` and `go test ./internal/query -run
'Test(SupplyChainListSBOMAttestationAttachmentsUsesBoundedStore|OpenAPISpecIncludesSBOMAttestationAttachments)'
-count=1` failed before SBOM attachment decisions and readbacks carried
attachment scope and missing anchor evidence, then passed after the reducer fact
payload, API row, OpenAPI fragment, and MCP tool description exposed that
truth.

No-Observability-Change: this changes SBOM attachment classification and
readback fields only. It adds no worker, queue, graph write, query, metric
instrument, span, metric label, runtime flag, or broad read path. Operators
continue to diagnose the path through the existing reducer attachment counter by
status, `query.sbom_attestation_attachments` handler span, Postgres fact-read
instrumentation, `canonical_writes`, `attachment_scope`, and
`missing_evidence`.
