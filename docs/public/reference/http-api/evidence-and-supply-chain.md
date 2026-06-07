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

## Service Catalog Correlation

`GET /api/v0/service-catalog/correlations`

Lists reducer-owned service-catalog ownership and drift correlations. The caller
must provide `limit` and at least one bounded anchor:

- `scope_id`
- `entity_ref`
- `repository_id`
- `service_id`
- `workload_id`
- `owner_ref`

`repository_id` accepts a canonical source repository id or the same human
repository selectors used by repository context routes. Repository-scoped
responses also include `evidence_summary.local_descriptors`, which reports
active repo-local Backstage, OpsLevel, Cortex, or warning facts from the
`git-repository-scope:<repository_id>` generation. These local descriptor facts
prove that the repository contains catalog evidence, but they do not by
themselves become external catalog confirmation. The separate
`evidence_summary.external_catalog_confirmation` block reports whether the
returned reducer correlation page contains non-provenance exact or derived
catalog confirmation; otherwise its `reason` explains local-only, ambiguous,
missing, not-checked, or unavailable evidence.

No-Regression Evidence: `go test ./internal/query -run 'TestServiceCatalogListCorrelationsExplains(LocalOnlyDescriptorEvidence|ExternalCatalogMatch|AmbiguousLocalDescriptor|NoEvidence)|TestServiceCatalogLocalDescriptorEvidenceQueryUsesActiveRepositoryScope|TestOpenAPISpecIncludesServiceCatalogCorrelations' -count=1` covers local-only descriptors, external catalog matches, ambiguous repo-local descriptor correlations, no-evidence responses, the active repository-scope source-fact query shape, and the OpenAPI response schema.

No-Observability-Change: the descriptor summary is a bounded Postgres read over
active `service_catalog.*` facts for one repository scope and reuses the
existing `query.service_catalog_correlations` request span, truth envelope,
Postgres query instrumentation, and HTTP/MCP envelope paths. It adds no graph
write, queue, worker, metric instrument, metric label, or runtime deployment
knob.

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
`GET /api/v0/supply-chain/advisories/evidence`

Lists source-only advisory evidence. The caller must provide `limit` and at
least one bounded anchor:

- `cve_id`
- `advisory_id`
- `package_id`

The optional `source` filter narrows an already anchored request. Rows group
GHSA, CVE/NVD, OSV, GitLab Advisory Database, FIRST EPSS, CISA KEV, CWE,
reference, affected-package, affected-product/CPE, withdrawn, affected-range,
and fixed-version evidence under one canonical advisory identity. They also
surface `source_disagreements[]` for severity, withdrawn status, affected
ranges, and fixed versions without selecting a winner.

This route reads active `vulnerability.*` source facts only. It does not emit
or imply reducer-owned package, repository, image, workload, deployment, or
reachability impact. Use the impact routes below when you need admitted impact
truth.

`GET /api/v0/supply-chain/vulnerabilities/{advisory_id}`

Path-param convenience over the same advisory evidence read model. It returns a
single canonical advisory matched by canonical id, GHSA id, or CVE id (a value
beginning with `CVE-` is anchored as a CVE), with the same source-specific CVSS,
EPSS, KEV, CWE, range, fixed-version, reference, and affected-package evidence as
the list route. An unknown identifier returns a `404` envelope. Like the list
route, this is source evidence only and does not imply repository, service,
image, or workload impact; the impact findings routes below carry admitted
impact truth, including the affected services and repositories the console links
to.

## Standalone Vulnerability Scanner Read Contract

`GET /api/v0/supply-chain/vulnerability-scanner/contract`

Returns the machine-readable scanner contract used by HTTP and MCP clients. It
names each scanner-facing filter, the routes that support it, the support class
(`exact`, `derived`, `provider-only`, `unsupported`, or `missing-evidence
driven`), and the backing index or precomputed read model. `?route=` narrows the
response to one route contract and rejects unknown route names before any read
model query runs.

| Filter | Support | Backing |
|---|---|---|
| `repository_id` | derived, exact, provider-only | Repository selector resolution plus reducer/provider Postgres read models. |
| `package_id` | exact | Active reducer impact facts and provider reconciliation facts. |
| `cve_id`, `advisory_id`, `ghsa_id`, `osv_id` | exact, provider-only | Payload advisory identifiers on reducer and provider reconciliation facts. |
| `subject_digest`, `digest` | exact, missing-evidence driven | Impact, SBOM, and container-image read models. |
| `workload_id`, `service_id`, `environment` | derived, missing-evidence driven | Reducer impact arrays populated only from admitted runtime/service evidence. |
| `ecosystem` | exact, unsupported | Impact payload ecosystem predicate; unsupported ecosystems stay in readiness gaps. |
| `language` | unsupported | No scanner read model maps source language to vulnerability impact truth. |
| `severity` | derived | CVSS-derived impact severity buckets: `critical`, `high`, `medium`, `low`, `none`. |
| `impact_status`, `reconciliation_status` | exact, provider-only | Reducer impact truth and provider reconciliation truth stay separate. |
| `readiness` | missing-evidence driven | Readiness is reported in envelopes and reports, not accepted as a row filter. |
| `provider_state`, `provider` | provider-only | Provider alert reconciliation facts; these never change Eshu impact truth. |

List, count, inventory, explain, and scanner-report reads share the same truth
envelope and missing-evidence vocabulary. List and inventory routes return
deterministic ordering, `limit`, `truncated`, and cursor or offset metadata.
Count routes do not page, but they use the same scope filters as list reads.
Unsupported scanner filters fail with a bounded `400` response instead of
falling through to broad graph or Postgres reads.

`GET /api/v0/supply-chain/impact/findings`

Lists reducer-owned vulnerability impact findings. The caller must provide
`limit` and at least one bounded anchor:

- `cve_id`
- `advisory_id`, `ghsa_id`, or `osv_id`
- `package_id`
- `repository_id`
- `subject_digest`
- `impact_status`
- `ecosystem`
- `workload_id`
- `service_id`
- `environment`
- `severity`
- `priority_bucket`
- `min_priority_score` greater than `0`

`repository_id` accepts the canonical internal repository id plus the same human
selectors repository context routes accept: repository name, repo slug, indexed
path, local path, or remote URL. Eshu resolves human selectors before reading
reducer impact facts; unknown or ambiguous selectors return a selector error.
The count and inventory aggregate routes use the same repository selector
resolution before reading reducer-owned aggregate facts. Aggregates also accept
the list route's `advisory_id`, `ecosystem`, `workload_id`, `service_id`,
`environment`, `severity`, `profile`, `priority_bucket`,
`min_priority_score`, `suppression_state`, and `include_suppressed` filters and
default to the same low-noise precise, unsuppressed semantics. `sort`,
`after_finding_id`, and list pagination remain list-only controls.

Valid impact statuses are `affected_exact`, `affected_derived`,
`possibly_affected`, `not_affected_known_fixed`, and `unknown_impact`.
Valid severity buckets are `critical`, `high`, `medium`, `low`, and `none`.
Valid priority buckets are `critical`, `high`, `medium`, `low`, and
`informational`. `min_priority_score` accepts `0` through `100`; `0` is the
default no-op value and does not count as a bounded anchor by itself. `sort` accepts
`finding_id`, `priority`, `priority_score_desc`, or `priority_score_asc`; the
priority sorts page by `(priority_score, finding_id)` so cursor paging does not
drop lower-priority rows.

Rows keep CVSS, EPSS, KEV, installed-version state, requested manifest range,
fixed-version state, match reason, runtime reachability, repository/image
evidence, workload/service/environment anchors, priority metadata, and missing
evidence separate. Priority is an explainable triage score only. A `critical`
or `high` priority bucket never turns missing version, package, SBOM, image,
deployment, or workload evidence into `affected_exact` or `affected_derived`
truth.

### Detection Profiles

Callers can ask for two detection profiles via `?profile=`:

- `precise` (default): returns only findings backed by an exact installed
  version anchor (lockfile, manifest with pinned version, or SBOM component
  with an explicit version) resolved by an ecosystem-aware matcher. This is
  Eshu's low-noise default. Supported exact-version matchers include npm,
  PyPI, Cargo, Swift, NuGet, Maven, and vendor-backed RPM-family OS package
  facts; Swift requires an exact remote source-control pin from
  `Package.resolved`.
- `comprehensive`: also returns findings whose evidence stops short of
  precise — range-only manifest ranges, CPE/SBOM-derived image paths
  without an exact version, malformed advisory ranges, and missing observed
  versions. Unsupported non-OS package ecosystems do not appear as finding rows;
  the readiness envelope reports them through `unsupported_targets[]` with
  stable reason codes. Comprehensive rows keep their `impact_status`, `confidence`,
  `match_reason`, and `missing_evidence` so callers see exactly why the precise
  bar was not met.

Each row carries `detection_profile` (`precise` or `comprehensive`) so a
UI or MCP client can compose the two profiles without re-running the query.
The response body echoes the requested profile in the top-level
`detection_profile` field. Provider-only security alerts without owned
package or SBOM evidence remain in
`/api/v0/supply-chain/security-alerts/reconciliations`; they are not
promoted into either profile of the impact findings list. Count and inventory
aggregates apply the same detection profile before counting rows and echo the
requested profile as `detection_profile`; `profile=comprehensive` is required
for aggregate buckets such as `possibly_affected` when those rows only meet the
comprehensive evidence bar.

### Suppression Filtering

`suppression_state` filters by the reducer's VEX/operator-policy decision.
Valid suppression states are `active`, `not_affected`, `accepted_risk`,
`false_positive`, `ignored`, `expired`, `provider_dismissed`, and
`scope_mismatch`. Operator-asserted hidden states (`not_affected`,
`accepted_risk`, `false_positive`, `ignored`) require
`include_suppressed=true` to be returned; `expired`, `provider_dismissed`,
and `scope_mismatch` stay visible by default because they preserve audit
signal. Every finding row carries a `suppression` block (always populated,
`state=active` when nothing matched) with the source (`vex_statement`,
`eshu_policy`, or `provider_dismissal`), justification, author, timestamps,
reason, evidence reference, and any VEX document/statement IDs so callers
can explain why a finding is hidden or why a related suppression did not
apply. Provider dismissals are evidence: the reducer surfaces them as
`provider_dismissed` without removing the finding from the default view.

Version fields intentionally do not collapse into one string:

- `observed_version`: exact installed version from lockfile, manifest, SBOM, or
  image evidence when known.
- `requested_range`: original manifest/requested dependency value, including
  range-only values such as npm caret ranges.
- `fixed_version`: source-selected fixed version when advisory evidence
  reports one.
- `vulnerable_range`: source-reported affected range expression copied from
  the advisory the reducer's provenance selector picked. Persisted on the
  canonical finding payload so the findings list, the explain route, and
  the MCP tools all expose the same expression. Older rows written before
  remediation computation may omit this value.
- `match_reason`: reducer explanation for the version decision, including
  supported npm, Go module, PyPI, Composer, RubyGems, Cargo, Hex, Pub, Swift,
  NuGet, Maven, and implemented OS package matches; range-only manifests; and
  malformed installed versions or advisory ranges. Unsupported ecosystems are
  readiness coverage gaps, not clean results.

Priority fields intentionally describe urgency without changing impact truth:

- `priority_score`: reducer score from 0 through 100.
- `priority_bucket`: `critical`, `high`, `medium`, `low`, or
  `informational`.
- `priority_reason_codes`: stable machine-readable reason codes for the
  signals that contributed to the score.
- `priority_contributions[]`: signed contribution rows with `reason_code`,
  `input`, `value`, and `contribution`. Inputs can include CVSS v3/v4, EPSS,
  CISA KEV, advisory age, dependency scope, direct/transitive relationship,
  exact/range-only/missing version evidence, SBOM/image evidence, deployed
  workload evidence, owned repository evidence, and fixed-version availability.

Reachability fields are enrichment and prioritization metadata. They do not
change `impact_status`, `confidence`, missing-evidence truth, suppression
state, or readiness:

- `runtime_reachability`: legacy compact signal such as `image_sbom`,
  `package_manifest`, `symbol_reachable`, `jvm_package_api_reachable`, or
  `not_called`.
- `reachability.state`: one of `reachable`, `not_called`, `unknown`,
  `unavailable`, or `missing_evidence`.
- `reachability.confidence`: confidence in the reachability signal, separate
  from vulnerability impact confidence.
- `reachability.source`: evidence family such as `govulncheck`,
  `parser_js_ts`, `scip_js_ts`, `jvm_parser_resolver`, `runtime_or_sbom`, or
  `not_available`.
- `reachability.language_maturity`: `implemented`, `partial`, `unavailable`,
  or `unsupported` for the ecosystem's current vulnerability-reachability
  support.
- `reachability.missing_evidence[]`: analyzer or runtime evidence that would
  improve the reachability state.

JavaScript and TypeScript npm rows can use parser or SCIP package API evidence
only when the imported, called, or re-exported package identity exactly matches
the vulnerable package. Similar unscoped/scoped names remain ambiguous, and
missing parser or SCIP rows become `missing_evidence` rather than a clean
result.

`not_called` currently has strong semantics for Go only when it comes from
govulncheck-style evidence. For other ecosystems, missing parser or
reachability evidence is explicit and never becomes a clean result.
For JVM Maven and Gradle findings, `jvm_package_api_reachable` means
resolver-proven package API prefixes matched Java/Kotlin/Scala parser or SCIP
usage in the same repository. It prioritizes the finding as reachable but does
not change `impact_status` or prove not-called safety.

Each row also carries a `provenance` block so callers can see which advisory
source supplied the selected severity, fixed version, and vulnerable range,
plus alternate severities reported by other sources for the same advisory:

- `provenance.selected_severity_source`, `selected_severity_score`,
  `selected_severity_vector`, `selected_severity_label`: the chosen source's
  severity attribution.
- `provenance.selected_fixed_version_source`,
  `provenance.selected_range_source`: which source supplied the selected
  fixed version and the selected vulnerable-range expression.
- `provenance.alternate_severities[]`: severities other sources reported for
  the same advisory that were not selected.
- `provenance.fixed_version_branches[]`: every source-reported fixed-version
  branch with the originating source preserved, including prerelease branches
  one source publishes and another does not.
- `provenance.advisory_sources[]`: every contributing source observation,
  including `source`, `advisory_id`, `source_updated_at`, and `withdrawn_at`
  so callers can explain selection and detect withdrawn records that were
  excluded from selection. Withdrawn observations remain visible.

Selection uses documented per-ecosystem priority: vendor advisories (Red Hat,
Debian, Ubuntu, Alpine, SUSE, Wolfi, Chainguard, Amazon Linux, Oracle Linux)
outrank generic GLAD/GHSA/OSV/NVD records for the matching OS package class;
GHSA outranks GLAD, OSV, and NVD for normalized language advisory metadata
when a finding can otherwise be admitted. Impact-supported language version
matchers are npm, Go module, PyPI, Composer, RubyGems, NuGet, Cargo, Pub,
Swift, Hex, and Maven today; vendor-backed OS package facts use the OS package
priority above. Matcher-unimplemented ecosystems remain source-only, missing,
or unsupported evidence rather than finding rows. If the selected source did
not publish a severity, the reducer falls back to the next-best source instead
of emitting a zero severity.
Exact owned lockfile dependency rows can prove the observed package version.
Npm, Composer, RubyGems, Cargo, Go module, PyPI, Hex, Pub, Swift, and NuGet
lockfile-backed findings may include `dependency_path`,
`dependency_depth`, and `direct_dependency` so callers can explain direct versus
transitive package impact without re-walking the lockfile.
Manifest ranges remain partial package evidence until a lockfile, SBOM/image,
or another owned exact-version source narrows the version. Product-only CPE
facts and package-registry facts without owned repository, image,
package-manifest, lockfile, or SBOM evidence remain source intelligence and do
not appear as impact findings.

Runtime context is evidence-only. Findings may include `repository_id`,
`subject_digest`, `image_ref`, `workload_ids[]`, `service_ids[]`, and
`environments[]` only when reducer-owned package/SBOM/image evidence joins to
explicit deployment or service-catalog facts. Ambiguous images, stale deployment
evidence, missing workload links, or missing service/environment links stay in
`missing_evidence[]` instead of being inferred from repository, tag, workload,
or service names. Exact repository-scoped service-catalog correlation evidence
is still attached to the finding path. When that correlation lacks explicit
`service_id` or `workload_id` anchors, the row reports
`service/workload catalog anchor missing` instead of saying service-catalog
correlation evidence is absent.

### Remediation (Safe Upgrade)

Each finding row and the explain payload also carry a `remediation` block
that explains the advisory-only safe-upgrade path Eshu can compute for that
finding. The reducer computes remediation only for ecosystems whose version
ordering and manifest-range semantics are represented in reducer matchers:
npm, Go modules, PyPI, Maven/Gradle, NuGet, Cargo, Composer, RubyGems, and
vendor-gated RPM, Debian/dpkg, and Alpine/APK OS packages. Debian and Alpine
recommendations require vendor advisory provenance, a parseable installed OS
package version, and one source-attributed fixed branch. OS package managers or
fixed-version branches without enough provenance remain explicit unsupported or
missing-evidence outcomes rather than guessed upgrade paths. Eshu never
auto-opens pull requests from this block.

- `ecosystem`: ecosystem the recommendation was computed for.
- `current_version`: installed version that matched the impact finding.
- `vulnerable_range`: source-reported affected range expression.
- `fixed_version_source`: advisory source that supplied the selected fixed version.
- `match_reason`: reducer version-match reason that explains why the package was affected or known fixed; kept separate from the remediation `reason`.
- `first_patched_version`: lowest source-reported fix Eshu can defend, preferring branches inside the observed major so callers are not pushed into a needless major bump.
- `patched_version_branches[]`: every source-attributed fixed-version branch (version + source) so callers see multi-branch advisories explicitly.
- `manifest_range`: original manifest/requested range preserved from package consumption evidence.
- `manifest_allows_fix`: one of `allowed`, `blocked`, or `unknown`; ecosystem-specific manifest semantics are checked before deciding whether the manifest range admits the first patched version, while transitive findings stay `unknown` because the user does not own the parent package's manifest.
- `direct`: `true` for direct dependencies, `false` for transitive, mirroring the lockfile-derived `direct_dependency` flag on the finding.
- `parent_package`: parent package the caller would need to upgrade for a transitive finding; blank for direct dependencies or chains without an identifiable parent.
- `confidence`: `exact`, `partial`, or `unknown`; exact means every required input was present and unambiguous, partial means the recommendation is actionable but at least one input is ambiguous, and unknown means Eshu cannot recommend a safe upgrade yet.
- `reason`: stable closed enum (`direct_upgrade_allowed`, `direct_range_blocked`, `transitive_parent_upgrade_required`, `already_fixed`, `no_patched_version`, `multiple_patched_branches`, `package_manager_unsupported`, `manifest_range_missing`, `manifest_range_malformed`, `installed_version_missing`, `installed_version_malformed`).
- `missing_evidence[]`: structured reasons the recommendation could not be
  computed exactly so callers can surface remediable gaps. OS package
  remediation can report `advisory_provenance_missing`,
  `fixed_version_branch_ambiguous`, or `version_ordering_unsupported` when
  Debian/APK/RPM provenance is not strong enough.

Older finding facts written before remediation computation landed expose a
missing `remediation` block; callers must treat that as "no remediation
computed yet," not "no fix available."

No-Regression Evidence: `go test ./internal/reducer ./internal/query ./internal/mcp ./internal/storage/postgres -run 'TestSupplyChainImpactPriority|TestSupplyChainListImpactFindingsFiltersAndSortsByPriority|TestSupplyChainListImpactFindingsRejectsInvalidPriorityFilters|TestDecodeSupplyChainImpactFindingRowPreservesPriority|TestSupplyChainImpactFindingQuerySupportsPriorityFiltersAndSort|TestResolveRouteMapsSupplyChainImpactPriorityFilters|TestSupplyChainImpactToolSchemaAdvertisesPriorityFilters|TestBootstrapDefinitionsIncludeSupplyChainImpactFactIndexes' -count=1` covers score contribution explainability, truth-preserving missing-evidence behavior, API/MCP priority filters and sorts, query shape, and the priority lookup index. The sorted read remains bounded by active reducer impact facts and pages by `(priority_score, finding_id)`.

No-Observability-Change: priority scoring reuses the existing `SupplyChainImpactFindings` reducer counter, `reducer_supply_chain_impact_finding` fact kind, impact evidence fields, readiness envelope, and `query.supply_chain_impact_findings` request span. No new graph write, queue, worker, metric instrument, or runtime deployment knob is introduced.

No-Regression Evidence: `go test ./internal/query -run 'Test(SupplyChain.*RepositorySelector|SupplyChainAggregateRoutesRejectInvalidRepositorySelector|SupplyChainListImpactFindingsUsesBoundedStore|SupplyChainListSecurityAlertReconciliationsSeparatesProviderAndEshuState|SecurityAlertReconciliationAggregateSourceFreshnessUsesCurrentFactAlias|PackageRegistryCorrelationsResolveRepositorySelectors|CICDRunCorrelationsResolveRepositorySelectors|CICDRunCorrelationAggregatesResolveRepositorySelectors|ServiceCatalogCorrelationsResolveRepositorySelectors|SupplyChainImpactExplainResolvesRepositorySelectors)' -count=1` covers internal id fast paths, repository name/slug/path resolution, invalid selector errors, provider-only alert scope preservation for repository reads, and bounded API reads for impact findings, impact aggregates, provider security-alert reconciliations, reconciliation aggregates, package registry correlations, CI/CD correlations, service-catalog correlations, and impact explain.

No-Observability-Change: selector resolution runs before the existing Postgres read-model calls and reuses `query.supply_chain_impact_findings`, `query.supply_chain_impact_aggregate`, `query.supply_chain_security_alerts`, `query.security_alert_reconciliation_aggregate`, `query.package_registry_correlations`, `query.ci_cd_run_correlations`, `query.ci_cd_run_correlation_aggregate`, `query.service_catalog_correlations`, Postgres query instrumentation, and the readiness envelope where applicable. No graph write, queue, worker, metric instrument, or runtime deployment knob is introduced.

The response includes a `readiness` envelope so clients can tell `nothing
matched` from `Eshu did not have the evidence to match yet`:

- `readiness_state` is one of `not_configured`, `target_incomplete`,
  `evidence_incomplete`, `ready_zero_findings`, `ready_with_findings`,
  `readiness_unavailable`, or `unsupported`. `readiness_unavailable` preserves
  the findings page when the coverage lookup fails. `unsupported` means Eshu
  observed real target evidence the matcher cannot resolve; callers MUST NOT
  interpret it as clean or affected.
- `target_scope` echoes the bounded anchors the caller used. `impact_status`
  alone is not a fact-anchor: the readiness store skips its Postgres scan
  and returns an empty snapshot for impact_status-only requests, because
  impact_status is a reducer-finding attribute that does not exist on
  source facts. The findings page is still returned.
- `evidence_sources[]` reports per-family source-fact counts and
  `latest_observed_at` for `vulnerability.advisory`,
  `vulnerability.exploitability`, `package.consumption`, `package.registry`,
  `sbom.component`, `sbom.attestation`, and `container_image.identity`. Each
  family carries its own `freshness` of `fresh`, `stale`, or `unknown` relative
  to a fourteen-day window. Families with zero in-scope facts are omitted so
  the payload reflects only evidence Eshu actually has for the caller.
  `package.registry` is only counted when the request anchors on a specific
  `package_id`; repository-only requests cannot satisfy `owned_packages`
  through global registry metadata.
- `source_snapshots[]` reports vulnerability source cache metadata:
  `source`, optional `ecosystem`, `cache_artifact_version`, `snapshot_digest`,
  `last_updated_at`, `freshness`, `complete`, and bounded warning fields. It
  is filtered to source scopes and ecosystems derived from the requested target
  before the envelope computes aggregate freshness, and it never returns raw
  advisory payloads.
- `source_states[]` reports durable vulnerability source target state:
  `last_attempt_at`, `last_success_at`, `next_retry_at`, `last_error_class`,
  collection window, `freshness_state`, `terminal_status`, `result_count`, and
  `warning_count`. Freshness states distinguish `not_configured`, `pending`,
  `fresh`, `stale`, `rate_limited`, `failed`, and `partial`; terminal status
  distinguishes pending, succeeded, partial, retryable failure, and terminal
  failure without raw advisory bodies or source URLs.
- `missing_evidence[]` names the absent required join families, such as
  `advisory_sources`, `owned_packages`, `sbom_or_image_evidence`,
  `target_collection_incomplete`, `readiness_unavailable`, or
  `unsupported_targets`. Reasons stay deduplicated, sorted, and free of
  package names or advisory bodies; the list is empty on `ready_*` states
  so callers cannot see contradictory "ready" + "missing" signals.
- `unsupported_targets[]` lists observed coverage-gap evidence Eshu cannot
  match. Each entry carries `target_kind` (`ecosystem`,
  `package_manager_file`, `sbom_target`, `package_registry_metadata`, or
  `image_target`), a stable `reason` code, a bounded `count`, and optional
  `ecosystem`, `lockfile_flavor`, or `feature_token` fields describing why the
  target is unsupported. `package_registry_metadata` with
  `reason=metadata_too_large` means a package-registry source document exceeded
  the configured byte cap and was recorded as coverage-gap evidence instead of
  being retried. The list is surfaced additively alongside `ready_*` states
  when only some observed targets are unsupported, and as the dominant signal
  when `readiness_state=unsupported`.
- `incomplete_reasons[]` lists collector-emitted reasons explaining why
  source collection is still in flight; only populated when
  `readiness_state` is `target_incomplete`.
- `freshness` aggregates per-family freshness and source target state into one
  label. Values are `fresh`, `stale`, `unknown`, `pending`, `rate_limited`,
  `failed`, or `partial`.
- `counts` reports `findings_returned`, `findings_truncated`,
  `findings_by_status`, and `evidence_facts_total`. `findings_returned` and
  `findings_by_status` describe the returned page only; combine with
  `truncated` to know if more pages exist.

Readiness is computed from existing source and reducer facts only. The
endpoint never invents findings; it surfaces counts and freshness so a zero
or partial answer can be interpreted correctly.

`GET /api/v0/supply-chain/impact/explain`

Explains one reducer-owned vulnerability impact finding or one bounded
advisory/package/repository path. The caller must provide either:

- `finding_id`; or
- `advisory_id` or `cve_id` plus at least one of `package_id`,
  `repository_id`, or `subject_digest`.

The route never performs a whole-graph explain. It reads one active
`reducer_supply_chain_impact_finding` fact and hydrates only the
`evidence_fact_ids` referenced by that finding. If a composite scope matches
more than one finding, the route returns `409` and asks for a narrower anchor.
If the scope is bounded but no finding exists, it returns `outcome:
no_finding` with readiness and missing-evidence reasons instead of implying
the target is safe.
`repository_id` accepts a canonical source repository id or a human repository
selector and resolves it before reading reducer impact facts. Container-image
routes keep their own `repository_id` field as an OCI/image repository identity;
they do not accept source repository selectors.

The explanation payload separates:

- `advisory`: CVE/advisory identifiers, source rows, references, selected
  severity/fixed-version/range source, and source-reported vulnerable range
  when the referenced evidence facts contain it.
- `component` and `version`: package/component identity, PURL, manifest range,
  observed version, vulnerable range, fixed version, and whether version
  evidence is `exact`, `range_only`, or `missing`.
- `dependency_chain`: direct/transitive path, depth, and direct-dependency
  status when manifest, lockfile, or SBOM evidence provides it.
- `anchors`: repository, manifest/lockfile paths, SBOM document ids, image
  digests, image refs, workload ids, service ids, environments,
  provider-alert handles, and evidence fact ids when present. Workload, image,
  service, environment, and provider-alert anchors remain evidence only; the
  route does not infer reachability or deployment truth from names, tags, or
  provider alerts.
- `impact_path`: ordered present and missing hops from advisory/package
  evidence through repository dependency, SBOM/image digest, deployment,
  workload, service, and environment evidence. Missing hops include the
  reducer-owned reason and remain visible to API and MCP callers.
- `freshness`: latest referenced evidence observation and evidence fact count.
- `missing_evidence`: reducer/readiness reasons plus explanation-level gaps
  such as missing observed version, vulnerable range, fixed version, or
  dependency chain.

No-Regression Evidence: `go test ./internal/query -run
'TestSupplyChainExplain|TestBuildSupplyChainImpactExplanation' -count=1`
proves bounded input rejection, exact finding explanation, range-only version
evidence, provider-only alert handling, SBOM/image anchors, ambiguous-scope
rejection, and no-evidence readiness response.

Observability Evidence: the explain route adds the
`query.supply_chain_impact_explanation` request span and reuses the existing
Postgres query instrumentation and readiness envelope. It adds no graph query,
queue, reducer lane, worker, or metric instrument.

The security-intelligence architecture keeps these findings separate from
source facts and readiness coverage. See
[Security Intelligence](../security-intelligence.md) for the target/capability
model, zero-finding readiness semantics, provider-alert parity gate, and future
local one-shot scanning direction.

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
`document_id`, `document_digest`, `workload_id`, or `service_id`.
`repository_id` is rejected for this route because repository proof must come
from repository-scoped routes such as impact findings, service-catalog
correlations, CI/CD correlations, container image identities, or security-alert
reconciliations.

Rows expose `attachment_status`, `parse_status`, and `verification_status`
separately. Component evidence is returned as document evidence only; this
route does not emit vulnerability priority or affected-by findings.

Rows also expose `attachment_scope` and `missing_evidence` so callers can tell
image-attached evidence from parse-only corpus evidence. `image_subject` means
Eshu saw an OCI referrer tying the SBOM or attestation document to the subject
digest. `subject_only_unanchored` rows can still carry reducer-owned repository,
workload, or service image anchors for scoped readback, but `canonical_writes`
remains `0` until OCI referrer evidence proves the document is attached to the
subject image. `parse_only_unanchored` rows remain visible for diagnostics and
must surface the missing evidence that blocks image attachment.

Parser warnings are not hidden. Each row returns `warning_summaries` as a
bounded duplicate-collapsed preview, `warning_summary_count` as the total number
of warning summary entries recorded on the reducer payload, and
`warning_summaries_truncated=true` when duplicate or overflow entries were
omitted from the preview. HTTP and MCP readbacks use the same response shape.

No-Regression Evidence: `go test ./internal/reducer -run
'Test(BuildSBOMAttestationAttachmentDecisionsClassifiesSubjectsAndTrust|ScannerWorkerGeneratedSBOMFactsAdmittedByReducerAttachment|PostgresSBOMAttestationAttachmentWriterPersistsAllStatuses)'
-count=1` and `go test ./internal/query -run
'Test(SupplyChainListSBOMAttestationAttachmentsUsesBoundedStore|OpenAPISpecIncludesSBOMAttestationAttachments)'
-count=1` failed before SBOM attachment decisions and readbacks carried
attachment scope and missing anchor evidence, then passed after the reducer fact
payload, API row, OpenAPI fragment, and MCP tool description exposed that
truth.

No-Regression Evidence: `go test ./internal/query ./internal/mcp -run
'Test(SupplyChainListSBOMAttestationAttachmentsRejectsRepositoryScope|SBOMAttestationAttachmentAggregateRoutesRejectRepositoryScope|ResolveRouteForwardsSBOMRepositoryScopeToHTTPContract|DispatchSBOMAggregateRepositoryScopeReturnsHTTPContractError)'
-count=1` proves repository-scoped SBOM attachment list, count, inventory, and
MCP aggregate calls fail before any read-model call while MCP still forwards
the unsupported `repository_id` argument to the HTTP contract guard.

No-Observability-Change: this changes SBOM attachment classification and
readback fields only. It adds no worker, queue, graph write, query, metric
instrument, span, metric label, runtime flag, or broad read path. Operators
continue to diagnose the path through the existing reducer attachment counter by
status, `query.sbom_attestation_attachments` handler span, Postgres fact-read
instrumentation, `canonical_writes`, `attachment_scope`, and
`missing_evidence`.

No-Regression Evidence: `go test ./internal/query -run
'Test(SupplyChainListSBOMAttestationAttachments(BoundsWarningSummaryPreview|UsesBoundedStore)|OpenAPISpecIncludesSBOMAttestationAttachments)'
-count=1` and `go test ./internal/mcp -run
'Test(DispatchToolSBOMAttestationAttachmentsReturnsBoundedWarningPreview|ResolveRouteMapsSBOMAttestationAttachmentsToBoundedQuery)'
-count=1` failed before attachment warning summaries were bounded, then passed
after the list response returned a duplicate-collapsed warning preview with
`warning_summary_count` and `warning_summaries_truncated`. The proof uses one
attachment with 256 duplicate warning summaries and shows response size is
bounded by attachment `limit` plus the fixed warning preview, not by the full
warning list.

No-Observability-Change: the warning preview change only reshapes existing
HTTP/MCP list readbacks after the same scoped, limit-required Postgres read.
It adds no new query, graph traversal, reducer domain, worker, queue, metric
instrument, metric label, span, or runtime flag. Operators continue to diagnose
the route through the existing `query.sbom_attestation_attachments` span,
truth envelope, limit/truncated cursor fields, Postgres query instrumentation,
and persisted warning summary entries on the attachment payload.
