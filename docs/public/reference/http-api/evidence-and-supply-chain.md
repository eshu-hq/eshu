# HTTP Evidence And Supply-Chain Routes

Use these routes when a client needs evidence packets, documentation truth,
package identity, CI/CD correlation, provider security alert reconciliation, or
SBOM attachment state. Use
[Security Intelligence](security-intelligence.md) for source advisory evidence,
vulnerability impact findings, impact explain, and the standalone scanner read
contract.

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
- `environment`

When `provider_run_id` or `run_id` is the only anchor, callers must also
provide `provider` because provider-native run IDs are not globally unique.
CI success, environment observations, and shell-only deployment hints do not
become deployment truth by themselves.
`repository_id` accepts a canonical source repository id or the same human
repository selectors used by repository context routes. Eshu resolves selectors
before reading reducer CI/CD correlation facts for the list, count, and
inventory routes; unknown or ambiguous selectors return a selector error.

## Vulnerability And Security Intelligence

Use [Security Intelligence](security-intelligence.md) for source advisory
evidence, the standalone vulnerability scanner contract, reducer-owned impact
findings, and impact explanations. Provider alert reconciliation remains below
because it keeps provider-reported alert state separate from Eshu-owned impact
truth.

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

Rows keep `provider_alert` and `eshu_impact` as separate objects. Provider
alert fields preserve reported alert ID/number, state, dependency ecosystem and
name, manifest path, dependency scope, relationship, GHSA/CVE IDs, vulnerable
range, patched version, severity, CVSS, EPSS, CWE, timestamps, and sanitized
source URL. Eshu impact fields come only from Eshu-owned dependency and impact
evidence. `eshu_impact.observed_version` is the exact package version Eshu
observed when available; `eshu_impact.match_reason` and
`eshu_impact.missing_evidence[]` explain range-only, malformed-version,
provider-only, stale, or other missing-evidence states without copying provider
alert data into Eshu truth. Valid reconciliation statuses are `matched`,
`unmatched`, `stale`, `dismissed`, `fixed`, and `provider_only`.

This route does not turn provider alert state into vulnerability impact truth.
Use `/api/v0/supply-chain/impact/findings` for reducer-owned impact findings.
Provider `severity` is returned inside `provider_alert`; it is not the same as
the impact `severity` scanner filter.

No-Regression Evidence: `go test ./internal/query ./internal/mcp -run 'Test(VulnerabilityScannerReadContract|SupplyChainImpactFindingsAcceptsScannerContractFilters|SupplyChainImpactFindingsRejectsUnsupportedScannerFiltersBeforeStore|SupplyChainImpactAggregatesAcceptScannerContractFilters|SupplyChainImpactInventoryCanGroupByEcosystem|ResolveRouteMapsVulnerabilityScannerContract|SupplyChainImpactMCPRouteForwardsScannerContractFilters|SupplyChainImpactAggregateMCPRoutesForwardScannerContractFilters|SupplyChainListSecurityAlertReconciliationsSeparatesProviderAndEshuState|SupplyChainListSecurityAlertReconciliationsSurfacesMissingObservedVersionEvidence|DecodeSecurityAlertReconciliationRowPreservesMalformedVersionEvidence|OpenAPISpecIncludesSecurityAlertReconciliations)' -count=1` covers the scanner read contract, bounded unsupported-filter failures, shared API/MCP filter forwarding, provider-only separation, security-alert observed-version and missing-version readback, malformed-version fail-closed evidence, and deterministic aggregate/list read semantics without graph traversal.

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
