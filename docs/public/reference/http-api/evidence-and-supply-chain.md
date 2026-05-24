# HTTP Evidence And Supply-Chain Routes

Use these routes when a client needs evidence packets, documentation truth,
package identity, CI/CD correlation, SBOM attachment state, or vulnerability
impact.

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
Exact owned lockfile dependency rows can prove the observed package version.
Npm lockfile-backed findings may include `dependency_path`,
`dependency_depth`, and `direct_dependency` so callers can explain direct versus
transitive package impact without re-walking the lockfile.
Manifest ranges remain partial package evidence until a lockfile, SBOM/image,
or another owned exact-version source narrows the version. Product-only CPE
facts and package-registry facts without owned repository, image,
package-manifest, lockfile, or SBOM evidence remain source intelligence and do
not appear as impact findings.

## SBOM And Attestation Attachments

`GET /api/v0/supply-chain/sbom-attestations/attachments`

Lists reducer-owned SBOM and attestation attachment facts. The caller must
provide `limit` and at least one bounded anchor: `subject_digest`,
`document_id`, or `document_digest`.

Rows expose `attachment_status`, `parse_status`, and `verification_status`
separately. Component evidence is returned as document evidence only; this
route does not emit vulnerability priority or affected-by findings.
