# HTTP Security Intelligence Routes

Use these routes when a client needs source-only vulnerability advisory
evidence, the standalone vulnerability scanner read contract, reducer-owned
impact findings, or one-finding impact explanations.

## Advisory Evidence

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

## Vulnerability Impact Findings

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
default no-op value and does not count as a bounded anchor by itself. `sort`
accepts `finding_id`, `priority`, `priority_score_desc`, or
`priority_score_asc`; the priority sorts page by `(priority_score, finding_id)`
so cursor paging does not drop lower-priority rows.

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
  precise: range-only manifest ranges, CPE/SBOM-derived image paths without an
  exact version, malformed advisory ranges, and missing observed versions.
  Unsupported non-OS package ecosystems do not appear as finding rows; the
  readiness envelope reports them through `unsupported_targets[]` with stable
  reason codes. Comprehensive rows keep their `impact_status`, `confidence`,
  `match_reason`, and `missing_evidence` so callers see exactly why the
  precise bar was not met.

Each row carries `detection_profile` (`precise` or `comprehensive`) so a UI or
MCP client can compose the two profiles without re-running the query. The
response body echoes the requested profile in the top-level `detection_profile`
field. Provider-only security alerts without owned package or SBOM evidence
remain in `/api/v0/supply-chain/security-alerts/reconciliations`; they are not
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
`include_suppressed=true` to be returned; `expired`, `provider_dismissed`, and
`scope_mismatch` stay visible by default because they preserve audit signal.
Every finding row carries a `suppression` block with the source, justification,
author, timestamps, reason, evidence reference, and any VEX
document/statement IDs so callers can explain why a finding is hidden or why a
related suppression did not apply. Provider dismissals are evidence: the reducer
surfaces them as `provider_dismissed` without removing the finding from the
default view.

Version fields intentionally do not collapse into one string:

- `observed_version`: exact installed version from lockfile, manifest, SBOM, or
  image evidence when known.
- `requested_range`: original manifest/requested dependency value, including
  range-only values such as npm caret ranges.
- `fixed_version`: source-selected fixed version when advisory evidence
  reports one.
- `vulnerable_range`: source-reported affected range expression copied from the
  advisory the reducer's provenance selector picked. Older rows written before
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
  `input`, `value`, and `contribution`.

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
reachability evidence is explicit and never becomes a clean result. For JVM
Maven and Gradle findings, `jvm_package_api_reachable` means resolver-proven
package API prefixes matched Java/Kotlin/Scala parser or SCIP usage in the same
repository. It prioritizes the finding as reachable but does not change
`impact_status` or prove not-called safety.

Each row also carries a `provenance` block so callers can see which advisory
source supplied the selected severity, fixed version, and vulnerable range,
plus alternate severities reported by other sources for the same advisory.
Selection uses documented per-ecosystem priority: vendor advisories (Red Hat,
Debian, Ubuntu, Alpine, SUSE, Wolfi, Chainguard, Amazon Linux, Oracle Linux)
outrank generic GLAD/GHSA/OSV/NVD records for the matching OS package class;
GHSA outranks GLAD, OSV, and NVD for normalized language advisory metadata when
a finding can otherwise be admitted. Impact-supported language version matchers
are npm, Go module, PyPI, Composer, RubyGems, NuGet, Cargo, Pub, Swift, Hex,
and Maven today; vendor-backed OS package facts use the OS package priority
above. Matcher-unimplemented ecosystems remain source-only, missing, or
unsupported evidence rather than finding rows.

Exact owned lockfile dependency rows can prove the observed package version.
Npm, Composer, RubyGems, Cargo, Go module, PyPI, Hex, Pub, Swift, and NuGet
lockfile-backed findings may include `dependency_path`, `dependency_depth`, and
`direct_dependency` so callers can explain direct versus transitive package
impact without re-walking the lockfile. Manifest ranges remain partial package
evidence until a lockfile, SBOM/image, or another owned exact-version source
narrows the version. Product-only CPE facts and package-registry facts without
owned repository, image, package-manifest, lockfile, or SBOM evidence remain
source intelligence and do not appear as impact findings.

Runtime context is evidence-only. Findings may include `repository_id`,
`subject_digest`, `image_ref`, `workload_ids[]`, `service_ids[]`, and
`environments[]` only when reducer-owned package/SBOM/image evidence joins to
explicit deployment or service-catalog facts. Ambiguous images, stale deployment
evidence, missing workload links, or missing service/environment links stay in
`missing_evidence[]` instead of being inferred from repository, tag, workload,
or service names.

### Remediation

Each finding row and the explain payload also carry a `remediation` block that
explains the advisory-only safe-upgrade path Eshu can compute for that finding.
The reducer computes remediation only for ecosystems whose version ordering and
manifest-range semantics are represented in reducer matchers: npm, Go modules,
PyPI, Maven/Gradle, NuGet, Cargo, Composer, RubyGems, and vendor-gated RPM,
Debian/dpkg, and Alpine/APK OS packages. Debian and Alpine recommendations
require vendor advisory provenance, a parseable installed OS package version,
and one source-attributed fixed branch. OS package managers or fixed-version
branches without enough provenance remain explicit unsupported or
missing-evidence outcomes rather than guessed upgrade paths. Eshu never
auto-opens pull requests from this block.

Remediation exposes the ecosystem, current version, vulnerable range,
fixed-version source, first patched version, patched-version branches, manifest
range, whether the manifest allows the fix, direct/transitive status,
transitive parent package when known, confidence, a stable reason enum, and
`missing_evidence[]`. Older finding facts written before remediation
computation landed expose a missing `remediation` block; callers must treat
that as "no remediation computed yet," not "no fix available."

No-Regression Evidence: `go test ./internal/reducer ./internal/query ./internal/mcp ./internal/storage/postgres -run 'TestSupplyChainImpactPriority|TestSupplyChainListImpactFindingsFiltersAndSortsByPriority|TestSupplyChainListImpactFindingsRejectsInvalidPriorityFilters|TestDecodeSupplyChainImpactFindingRowPreservesPriority|TestSupplyChainImpactFindingQuerySupportsPriorityFiltersAndSort|TestResolveRouteMapsSupplyChainImpactPriorityFilters|TestSupplyChainImpactToolSchemaAdvertisesPriorityFilters|TestBootstrapDefinitionsIncludeSupplyChainImpactFactIndexes' -count=1` covers score contribution explainability, truth-preserving missing-evidence behavior, API/MCP priority filters and sorts, query shape, and the priority lookup index. The sorted read remains bounded by active reducer impact facts and pages by `(priority_score, finding_id)`.

No-Observability-Change: priority scoring reuses the existing
`SupplyChainImpactFindings` reducer counter,
`reducer_supply_chain_impact_finding` fact kind, impact evidence fields,
readiness envelope, and `query.supply_chain_impact_findings` request span. No
new graph write, queue, worker, metric instrument, or runtime deployment knob is
introduced.

No-Regression Evidence: `go test ./internal/query -run 'Test(SupplyChain.*RepositorySelector|SupplyChainAggregateRoutesRejectInvalidRepositorySelector|SupplyChainListImpactFindingsUsesBoundedStore|SupplyChainListSecurityAlertReconciliationsSeparatesProviderAndEshuState|SecurityAlertReconciliationAggregateSourceFreshnessUsesCurrentFactAlias|PackageRegistryCorrelationsResolveRepositorySelectors|CICDRunCorrelationsResolveRepositorySelectors|CICDRunCorrelationAggregatesResolveRepositorySelectors|ServiceCatalogCorrelationsResolveRepositorySelectors|SupplyChainImpactExplainResolvesRepositorySelectors)' -count=1` covers internal id fast paths, repository name/slug/path resolution, invalid selector errors, provider-only alert scope preservation for repository reads, and bounded API reads for impact findings, impact aggregates, provider security-alert reconciliations, reconciliation aggregates, package registry correlations, CI/CD correlations, service-catalog correlations, and impact explain.

No-Observability-Change: selector resolution runs before the existing Postgres
read-model calls and reuses `query.supply_chain_impact_findings`,
`query.supply_chain_impact_aggregate`, `query.supply_chain_security_alerts`,
`query.security_alert_reconciliation_aggregate`,
`query.package_registry_correlations`, `query.ci_cd_run_correlations`,
`query.ci_cd_run_correlation_aggregate`, `query.service_catalog_correlations`,
Postgres query instrumentation, and the readiness envelope where applicable. No
graph write, queue, worker, metric instrument, or runtime deployment knob is
introduced.

The response includes a `readiness` envelope so clients can tell `nothing
matched` from `Eshu did not have the evidence to match yet`. Readiness is
computed from existing source and reducer facts only. The endpoint never
invents findings; it surfaces counts and freshness so a zero or partial answer
can be interpreted correctly.

## Impact Explain

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
no_finding` with readiness and missing-evidence reasons instead of implying the
target is safe.

`repository_id` accepts a canonical source repository id or a human repository
selector and resolves it before reading reducer impact facts. Container-image
routes keep their own `repository_id` field as an OCI/image repository identity;
they do not accept source repository selectors.

The explanation payload separates advisory metadata, package/component version
truth, dependency-chain evidence, repository/SBOM/image/workload/service
anchors, ordered present and missing impact-path hops, freshness, and
`missing_evidence`. Workload, image, service, environment, and provider-alert
anchors remain evidence only; the route does not infer reachability or
deployment truth from names, tags, or provider alerts.

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
