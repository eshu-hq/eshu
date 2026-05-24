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

## Vulnerability Impact

`GET /api/v0/supply-chain/impact/findings`

Lists reducer-owned vulnerability impact findings. The caller must provide
`limit` and at least one bounded anchor:

- `cve_id`
- `package_id`
- `repository_id`
- `subject_digest`
- `impact_status`

Valid impact statuses are `affected_exact`, `affected_derived`,
`possibly_affected`, `not_affected_known_fixed`, and `unknown_impact`.
Rows keep CVSS, EPSS, KEV, fixed-version state, runtime reachability,
repository/image evidence, and missing evidence separate.

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
GHSA outranks GLAD, OSV, and NVD for language ecosystems (npm, PyPI, Go,
Maven, etc.). If the selected source did not publish a severity, the reducer
falls back to the next-best source instead of emitting a zero severity.
Exact owned lockfile dependency rows can prove the observed package version.
Npm lockfile-backed findings may include `dependency_path`,
`dependency_depth`, and `direct_dependency` so callers can explain direct versus
transitive package impact without re-walking the lockfile.
Manifest ranges remain partial package evidence until a lockfile, SBOM/image,
or another owned exact-version source narrows the version. Product-only CPE
facts and package-registry facts without owned repository, image,
package-manifest, lockfile, or SBOM evidence remain source intelligence and do
not appear as impact findings.

The response also includes a `readiness` envelope so a UI, MCP client, or
operator can tell `nothing matched` from `Eshu did not have the evidence to
match yet`:

- `readiness_state` is one of `not_configured`, `target_incomplete`,
  `evidence_incomplete`, `ready_zero_findings`, `ready_with_findings`, or
  `readiness_unavailable`. The last state is returned when the readiness
  lookup itself fails; the findings page is preserved but coverage cannot
  be classified.
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
  never returns raw advisory payloads.
- `source_states[]` reports durable vulnerability source target state:
  `last_attempt_at`, `last_success_at`, `next_retry_at`, `last_error_class`,
  collection window, `freshness_state`, `terminal_status`, `result_count`, and
  `warning_count`. Freshness states distinguish `not_configured`, `pending`,
  `fresh`, `stale`, `rate_limited`, `failed`, and `partial`; terminal status
  distinguishes pending, succeeded, partial, retryable failure, and terminal
  failure without raw advisory bodies or source URLs.
- `missing_evidence[]` names the absent required join families, such as
  `advisory_sources`, `owned_packages`, `sbom_or_image_evidence`,
  `target_collection_incomplete`, or `readiness_unavailable`. Reasons stay
  deduplicated, sorted, and free of package names or advisory bodies; the
  list is empty on `ready_*` states so callers cannot see contradictory
  "ready" + "missing" signals.
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
  digests, workload ids, provider-alert handles, and evidence fact ids when
  present. Workload, image, and provider-alert anchors remain evidence only;
  the route does not infer reachability or deployment truth from names, tags,
  or provider alerts.
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
- `provider_state`
- `reconciliation_status`

Rows keep `provider_alert` and `eshu_impact` as separate objects. Provider
alert fields preserve reported alert ID/number, state, dependency ecosystem and
name, manifest path, dependency scope, relationship, GHSA/CVE IDs, vulnerable
range, patched version, severity, CVSS, EPSS, CWE, timestamps, and sanitized
source URL. Eshu impact fields only appear when the reducer matched owned
impact evidence. Valid reconciliation statuses are `matched`, `unmatched`,
`stale`, `dismissed`, `fixed`, and `provider_only`.

This route does not turn provider alert state into vulnerability impact truth.
Use `/api/v0/supply-chain/impact/findings` for reducer-owned impact findings.

## SBOM And Attestation Attachments

`GET /api/v0/supply-chain/sbom-attestations/attachments`

Lists reducer-owned SBOM and attestation attachment facts. The caller must
provide `limit` and at least one bounded anchor: `subject_digest`,
`document_id`, or `document_digest`.

Rows expose `attachment_status`, `parse_status`, and `verification_status`
separately. Component evidence is returned as document evidence only; this
route does not emit vulnerability priority or affected-by findings.
