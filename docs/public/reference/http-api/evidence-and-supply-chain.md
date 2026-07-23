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
- `artifacts[].source_repo_canonical_id` and
  `artifacts[].target_repo_canonical_id` carry a privacy-safe canonical
  repository identity when the source remote URL can be normalized.
- `artifacts[].source_repo_scope_key` and `artifacts[].target_repo_scope_key`
  carry a stable `scope:s_...` discriminator when an ingestion scope is known.
  The response never exposes the raw scope identifier through these fields.
- `artifacts[].source_location` records `repo_id`, `repo_name`, `path`, and
  line range when the extractor emitted line data.
- `artifacts[].ref_value` and `artifacts[].ref_pinned` (issue #5372, GitHub
  Actions artifacts only): the raw `@ref` an action or reusable workflow
  step pins to, and whether that ref is a full-length commit SHA (the only
  ref shape that is immutable). Both fields are omitted together when the
  workflow declares no ref at all (a local `./` reusable workflow, a Docker
  action, or no `@` segment) -- never defaulted. See
  [Relationship Mapping Evidence](../relationship-mapping-evidence.md) for
  the full contract, including why a branch and a tag are not distinguished
  beyond "not pinned."
- `evidence_index.lookup_basis` is `resolved_id`.

`deployment_evidence` (and therefore `ref_value`/`ref_pinned`) is surfaced by
the repository, service, and workload context/story MCP tools and their HTTP
equivalents: `get_repo_context`, `get_repo_story`, `get_service_context`,
`get_service_story`, `get_workload_context`, `get_workload_story`,
`trace_deployment_chain`, `investigate_deployment_config`, and
`investigate_service`. The repository workflow-artifact rollup's
`unpinned_action_refs` signal (the raw `owner/repo@ref` string for each action
pinned to something other than a full commit SHA, excluding `actions/checkout`,
which is modeled through its own checkout-repository signal, so the list is not
an exhaustive audit of every unpinned `uses:`) is surfaced through the same
repository-context/story surfaces via the workflow-artifact loader
(`repository_workflow_artifacts_loader.go`, reached through
`repository_deployment_artifacts_loader.go`).

## Relationship Evidence

`GET /api/v0/evidence/relationships/{resolved_id}`

Dereferences one deployment evidence pointer into the durable relationship
evidence row. The response includes lookup basis, source and target repository
metadata, relationship type, confidence, evidence count, evidence kinds,
rationale, generation metadata, `evidence_preview`, and decoded details.
Correlation rows also include `confidence_basis`: `evidence_constant` for a
single extractor confidence weight, `evidence_aggregate` for resolver
corroboration across multiple facts, or `assertion_override` for an explicit
control-plane assertion.

Use this route when a client needs to explain why an edge exists without
embedding full evidence payloads in every graph response.

## Admission Decisions

`GET /api/v0/evidence/admission-decisions`

Lists reducer-owned admission decisions for correlation domains. The required
`domain`, `scope_id`, and `generation_id` filters keep every read bounded to one
source generation. Optional `state`, `anchor_kind`, and `anchor_id` filters
narrow the page to decisions for a service, repository, workload, cloud
resource, package, incident, or another domain-owned anchor.

Admission decisions are not canonical graph edges. They explain why a candidate
was admitted, rejected, ambiguous, stale, missing evidence, permission hidden,
unsupported, or unsafe. A candidate becomes graph truth only when the reducer's
canonical-write block says it was eligible and written. Non-admitted rows stay
visible with redaction-safe source handles and recommended next calls so
operators can inspect missing or conflicting evidence without fabricating
progress.

Scoped tokens may read only decisions attributable to granted scopes or
repository anchors. Empty or out-of-grant scoped requests return a bounded empty
page without reading the store, so callers do not learn whether another tenant's
decision exists.

## Citation Packets

`POST /api/v0/evidence/citations`

Hydrates bounded file and entity handles into a reusable citation packet. Send
handles from story, investigation, search, or drill-down responses with
`repo_id + relative_path` for files or `entity_id` for entities.

The route accepts at most 500 input handles, hydrates at most 50 citations per
packet, preserves distinct line ranges and reasons for the same file, and
returns `coverage.truncated` when the caller should request another packet.
It reads the Postgres content store and does not traverse the graph.

Each hydrated citation carries the unified evidence contract (issue #3489): a
`confidence` score (carried through from the handle's optional `confidence`), a
byte-level citation (`start_line`/`end_line` plus `byte_offset`/`byte_length`
into the source content, with `content_hash` and `commit_sha` pinning the cited
bytes), and a typed `provenance` object (`basis`, `rationale`, `source`). This
is the same `confidence + byte-level citation + provenance` shape defined by the
canonical `truth.Evidence` record, so relationship evidence, citation packets,
and documentation evidence all describe proof the same way.

## Documentation Truth

Documentation updater services should use these routes instead of reading graph
internals directly.

- `GET /api/v0/documentation/findings`
- `GET /api/v0/documentation/facts`
- `GET /api/v0/documentation/findings/{finding_id}/evidence-packet`
- `GET /api/v0/documentation/evidence-packets/{packet_id}/freshness`

`eshu docs verify` emits the same `documentation_finding` and
`documentation_evidence_packet` fact shapes that these routes expose after the
facts are persisted by a caller or data-plane runtime. Unsupported claim
families stay visible as `unsupported_claim_type`.

`GET /api/v0/documentation/findings` accepts filters for finding type, source,
document, status, truth level, freshness state, scope, generation, repository,
updated time, limit, and cursor.

`GET /api/v0/documentation/facts` accepts source, document, section, repository,
target, service, scope, generation, search, updated time, limit, and cursor
filters. Responses return `facts`, page `count`, normalized `limit`,
`truncated`, `missing_evidence`, `states`, and `next_cursor` only when the
bounded page has more rows.
The optional `fact_kind` filter accepts the canonical
`semantic.documentation_observation` kind plus `semantic_observation` and
`documentation_observation` aliases. These rows remain provenance-only semantic
evidence and do not become documentation findings without reducer-owned
admission.

`GET /api/v0/documentation/findings/{finding_id}/evidence-packet` returns the
bounded packet an external updater can snapshot before it plans a diff. Eshu
does not draft text or write documentation through this route.

`GET /api/v0/documentation/evidence-packets/{packet_id}/freshness` lets an
updater check whether a saved packet is stale before publishing a diff.

Documentation findings and evidence packets carry an optional `source_acl_state`
(`allowed`, `denied`, `partial`, `missing`, or `stale`) when the collector
observed a bounded source-ACL posture. It is a distinct access-posture axis kept
separate from the binary `permissions` decision and from `freshness_state`:
it can represent `partial` or `stale` ACL that the binary permission flag
cannot, and a finding can be fresh yet denied. The field is omitted when the
source asserted no bounded ACL claim (absence means "no ACL claim").

Each finding and packet also carries a bounded `access_disposition` enforced
from `source_acl_state` and the per-caller read decision (#2164):

| Disposition | Behavior |
| --- | --- |
| `visible` | Content intact. |
| `access_denied` | Caller authenticated-but-not-authorized. The row is disclosed (not silently dropped) with `permission_denied: true` and `content_withheld: true`, and its protected content is stripped — only bounded identity/state fields remain. The single evidence-packet read returns `403 permission_denied` with no body. |
| `partial` | Returned with a partial marker and `content_withheld: true`; protected content boundaries are respected. |
| `stale` | Surfaced as stale on the ACL axis; the source is permitted-but-stale so content stays intact. |
| `missing` | Source not found/deleted at the origin; disclosed as missing with no content. |

A reader can therefore distinguish "no evidence" from "evidence exists but is
denied, partial, or stale." Withholding is fail-closed: a denied or partial
source never has its content or excerpt returned. The disclosure is a distinct
axis from the freshness/truth taxonomy (#2138): `freshness_state`, `truth_level`,
`missing_evidence`, `unsupported_reason`, and the other truth labels are
preserved on a withheld row and never collapsed into the permission error.

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
- `GET /api/v0/package-registry/dependency-chains`

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

`/dependency-chains` requires `limit` and `repository_id` (same selector rules as
`/correlations`). It resolves package-evidenced consumer-repo -> package ->
publisher-repo chains for the repository entirely on the read side: it joins the
repository's canonical manifest-backed consumption correlations with the
provenance-only publication/ownership correlations for each consumed package, in
two bounded reads (one consumption read, one batched publisher read keyed by the
consumed package set). Each chain carries the canonical consumption leg
(`consumption_provenance_only=false`) and zero or more publisher legs. Publisher
legs are inferred provenance-only links (`provenance_only=true`) and are never
asserted as canonical `(:Repository)-[:DEPENDS_ON]->(:Repository)` graph edges,
because the reducer holds package publication/ownership correlations
provenance-only until corroborating build/release/CI evidence exists. A package
with no publisher correlation terminates at the package; a package with more than
one candidate publisher is marked `ambiguous=true` and never collapsed to a
single asserted publisher; a publisher repository that equals the consumer
repository is dropped as a self-reference.

### Scoped-token access (#5167 W5b)

`/packages`, `/versions`, `/dependencies`, `/packages/count`, and
`/packages/inventory` support scoped bearer tokens. Package/version/dependency
graph nodes carry no repository or tenant property, so these routes gate on
package `visibility` instead of a repository grant:

- A `visibility: public` package's identity, versions, and dependencies are
  world-readable to any scoped token (the same class of global data the
  advisory-evidence route already exposes); `source_path` is redacted on these
  rows because a public registry row can still name an unrelated repository's
  manifest path.
- A `visibility: private` or `visibility: unknown` package is visible only when
  a reducer-owned package correlation (ownership, publication, or consumption)
  proves the caller's granted repositories own, consume, or publish it — the
  same bounded probe `/correlations` already exposes. `source_path` is
  returned unredacted on a granted row.
- A private/unknown package outside the caller's grant returns the exact same
  empty page as a nonexistent `package_id` — there is no way to distinguish
  "exists but denied" from "does not exist" through this surface.
- `ecosystem`-only browsing on `/packages` (no `package_id` or `name`) returns
  only `visibility: public` rows for a scoped caller; correlation-augmented
  inclusion of a caller's own private packages in an ecosystem browse is not
  yet implemented (tracked as a follow-up).
- `/packages/count` and `/packages/inventory` force `visibility=public` onto a
  scoped caller's aggregate regardless of the `visibility` query parameter,
  UNLESS the caller explicitly requests `visibility=private` or
  `visibility=unknown`, in which case the response is an empty envelope (the
  response `scope` always reflects the value actually applied, not the
  request). `group_by=visibility` therefore degenerates to a single `public`
  bucket for scoped callers.

**Operator hygiene:** a package-registry collector target's declared
`visibility` (`public`/`private`) determines whether scoped tokens can ever see
that target's packages via this path. A target with no declared `visibility`
defaults to `unknown` and is treated as not-provably-public, so its packages
are invisible to scoped callers unless a correlation grant proves ownership.
Operators who intend a source to be publicly browsable by scoped tokens MUST
set `visibility: public` on that collector target.

Performance Evidence: proved against the pinned PR261/compose-default
NornicDB image (Bolt + HTTP tx/commit) and a live Postgres instance with the
real `eshu-bootstrap-data-plane` schema, per CLAUDE.md's Prove-The-Theory-First
gate, before landing the row-filtering code.

- Ecosystem-browse visibility filter (`packageRegistryPackagesScopedEcosystemCypher`):
  120k-node single-ecosystem partitions, two shapes -- (a) 90% public / 10%
  private, (b) pathological 100% `unknown` (predicate matches nothing).
  Combined-`WHERE` form (`MATCH (p:Package) WHERE p.ecosystem = $ecosystem AND
  p.visibility = 'public'`): shape (a) warm ~10ms/201-row page; shape (b) warm
  ~10-40ms/0-row page. Equivalence: NEW result set (full scan, no LIMIT) vs.
  OLD result set client-filtered to `visibility=='public'`, symmetric
  set-difference on `package_id` = 0/0 (108000/108000 match on shape (a); 0/0
  on shape (b)). The literal decision-doc shape (inline `MATCH` property +
  trailing `WHERE`) was DISPROVEN -- see
  docs/public/reference/nornicdb-pitfalls.md ("Inline MATCH Property Pattern
  Silently Dropped By A Trailing WHERE"); it silently drops the `$ecosystem`
  anchor and both a correctness leak and a full-scan regression.
- Correlation-grant probe (`ListPackageRegistryCorrelations`, `Limit: 1`):
  worst case is a hyper-consumed package with ~15,000 correlation rows (5,000
  granted-repository-shaped scopes x 3 fact kinds) inside a 1M-row
  `fact_records` table, probed with a 100-id granted-repository list that
  matches nothing (forces the full per-package index range scan via
  `fact_records_package_correlations_v2_lookup_idx`). `EXPLAIN (ANALYZE,
  BUFFERS)` warm execution time: ~5.4-7.1ms (bound: <10ms). Delta check: the
  `LIMIT 1` probe returns 0 rows iff an unbounded (`LIMIT 20000`) same-predicate
  query also returns 0 rows for the no-match grant, and returns >=1 row iff the
  unbounded query also returns >=1 (1 vs. 3 rows for a matching grant).
- Forced-visibility aggregates (`countPackageRegistryPackages`,
  `packageRegistryPackageInventory`): query text is unchanged (already
  parameterized `$visibility`); measured on a 240k-node corpus (the two
  ecosystem-browse shapes combined), unfiltered vs. `visibility='public'`
  counts both warm ~10-70ms, no measurable regression.

No-Observability-Change: the handlers keep the existing
`startQueryHandlerSpan`/`query.package_registry_*` spans and per-route
`readiness`/truth envelopes; the only addition is two span attributes on the
existing request span, `pkgreg.scoped_visibility_forced` (bool) and
`pkgreg.correlation_grant` (`hit`/`miss`/`unavailable`), set only on the
scoped-caller gate path. No new metric instrument, metric label, worker,
queue, or runtime deployment knob is introduced.

## Gated List Collector Readiness

Seven gated supply-chain list routes are fed by opt-in collectors that stay off
in a default git-only deploy: `/package-registry/packages`,
`/package-registry/versions`, `/package-registry/dependencies`,
`/package-registry/correlations`, `/supply-chain/sbom-attestations/attachments`,
`/supply-chain/container-images/identities`, and `/ci-cd/run-correlations`. For
these routes an empty page is otherwise ambiguous: a caller cannot tell "no data
matched" from "the feeding collector is not enabled." Each response therefore
carries a `collector_readiness` envelope (the per-collector mirror of the
vulnerability impact-findings `readiness` envelope) so the empty page is never
ambiguous:

- `readiness_state`:
  - `not_configured`: no enabled, non-deactivated instance of the feeding
    collector is registered, so the empty page reflects a disabled collection
    lane rather than absent data.
  - `ready_zero_results`: the feeding collector is enabled but the bounded query
    returned no rows, so the empty page is a genuine zero result for the scope.
  - `ready_with_results`: the page returned at least one row. Returned rows are
    themselves proof the collector ran, so a non-empty page never consults the
    configured probe.
  - `readiness_unavailable`: the configured probe itself failed. The page is
    still returned, but its emptiness cannot be classified, so callers must not
    read zero rows as configured-but-empty in this state.
- `collector_kind`: the feeding collector family (`sbom_attestation`,
  `package_registry`, `oci_registry`, or `ci_cd_run`).
- `counts`: `results_returned` and `results_truncated` for the returned page.

The configured probe is a single bounded existence check over the
`collector_instances` registry and is omitted entirely when no readiness store
is wired, so handlers built without the probe keep their existing response
shape.

No-Regression Evidence: `go test ./internal/query -run
'Test(BuildCollectorListReadiness|GatedListTools(ReportNotConfiguredWhenCollectorDisabled|ReportReadyZeroResultsWhenCollectorConfigured|ReportReadinessUnavailableOnProbeError|OmitReadinessWhenStoreUnset)|PostgresCollectorListReadiness)'
-count=1` fails if any of the 7 gated list routes stops distinguishing an
unconfigured feeding collector from a configured-but-empty page, or if the
Postgres configured probe drifts from the `collector_instances` existence check.

No-Observability-Change: the readiness envelope reuses the existing per-route
request span, truth envelope, and Postgres/graph query instrumentation each
gated list route already emits. The configured probe is one bounded indexed
existence read over `collector_instances` keyed by `(collector_kind, enabled)`;
it adds no graph read, graph write, reducer work, queue, worker, metric
instrument, metric label, or runtime knob.

## Dependency Inventory

`GET /api/v0/dependencies` is a bounded, name-anchored browse over the
package-native dependency graph. It answers two questions directly, without
requiring the caller to first resolve an internal package id:

- forward (`direction=forward`, the default): what does a package depend on.
- reverse (`direction=reverse`): which packages depend on the anchor package.

This complements `GET /api/v0/package-registry/dependencies`, which is anchored
by exact `package_id`/`version_id`. The dependency inventory anchors on the
indexed `Package.normalized_name` and walks
`Package -[:HAS_VERSION]-> PackageVersion -[:DECLARES_DEPENDENCY]->
PackageDependency -[:DEPENDS_ON_PACKAGE]-> Package`. Repository and service
ownership are not asserted here; they remain reducer correlation concerns.

Parameters:

- `limit` bounds the page (1..200, default 50). The read requests `limit+1`
  internally to set `truncated` and a keyset `next_cursor`.
- `direction` is `forward` or `reverse` (default `forward`).
- `package` is the normalized package name to anchor on. It is required for
  `direction=reverse` and optional for `direction=forward` (omit to browse all
  forward edges).
- `ecosystem` restricts to one ecosystem such as `npm` or `maven`.
- `after_name` and `after_edge` page a prior `next_cursor`; they must be sent
  together.

Each row carries the active `direction`, the anchor identity
(`anchor_package`, `anchor_package_id`, `anchor_ecosystem`), the declaring
`declaring_version`, the related package at the other end of the edge
(`related_package`, `related_package_id`, `related_ecosystem`), and the declared
edge facts (`dependency_range`, `dependency_type`, `optional`, `edge_id`). For
reverse rows the declaring package is reported from its `PackageVersion`, because
declaring packages are not always materialized as `Package` nodes. Responses are
`exact` truth from the authoritative graph with deterministic ordering and
keyset paging.

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
Rows for ambiguous, unresolved, stale, or rejected catalog declarations can also
include `required_anchor_keys`, a bounded list of accepted proof anchors that
would let the reducer promote the declaration without relying on catalog names
alone.

No-Regression Evidence: `go test ./internal/query -run 'TestServiceCatalogListCorrelationsExplains(LocalOnlyDescriptorEvidence|ExternalCatalogMatch|AmbiguousLocalDescriptor|NoEvidence)|TestServiceCatalogLocalDescriptorEvidenceQueryUsesActiveRepositoryScope|TestOpenAPISpecIncludesServiceCatalogCorrelations' -count=1` covers local-only descriptors, external catalog matches, ambiguous repo-local descriptor correlations, no-evidence responses, the active repository-scope source-fact query shape, and the OpenAPI response schema.

No-Observability-Change: the descriptor summary is a bounded Postgres read over
active `service_catalog.*` facts for one repository scope and reuses the
existing `query.service_catalog_correlations` request span, truth envelope,
Postgres query instrumentation, and HTTP/MCP envelope paths. It adds no graph
write, queue, worker, metric instrument, metric label, or runtime deployment
knob.

## Codeowners Ownership

`GET /api/v0/codeowners/ownership` (issue #5419 Phase 4) is a bounded,
repository-anchored read over the Phase 3 `DECLARES_CODEOWNER` graph edges
(`Repository -[:DECLARES_CODEOWNER]-> CodeownerTeam`, one edge per CODEOWNERS
rule pattern and owner pair), plus an `effective_owner` resolved by
manifest-vs-codeowners precedence.

Parameters:

- `repository_id` is required.
- `limit` bounds the page (1..200, default 50). The read requests `limit+1`
  internally to set `truncated` and a keyset `next_cursor`.
- `after_order_index`, `after_pattern`, and `after_ref` page a prior
  `next_cursor`; they must be sent together.

Each ownership row carries `pattern`, `source_path`, `order_index`, and
`owner_ref`, ordered deterministically on `(order_index, pattern, owner_ref)`
since one repository can declare the same pattern with more than one owner
token.

`effective_owner` resolves precedence in this order:

1. A service-catalog manifest declaration wins when
   `list_service_catalog_correlations`'s reducer store returns a row for the
   repository with a non-empty `owner_ref` and an `exact` or `derived`
   outcome. `effective_owner.source` is `service_catalog`.
2. Otherwise, the repository's CODEOWNERS rules resolve last-match-wins: the
   `DECLARES_CODEOWNER` edge with the highest `order_index` is the last
   pattern in the file that would match, so its owner is the fallback.
   `effective_owner.source` is `codeowners`.
3. Otherwise `effective_owner` is empty (neither `owner_ref` nor `source` is
   present) -- this is a normal, non-error outcome for a repository with no
   resolvable owner.

Scoped tokens may read ownership only for a granted `repository_id`. An
out-of-grant `repository_id` returns a bounded empty page (`ownership: []`,
`effective_owner: {}`) without reading the `DECLARES_CODEOWNER` graph or the
service-catalog correlation store, so a caller cannot use either read path to
probe another tenant's CODEOWNERS rules or manifest owner.

Responses are `exact` truth from the authoritative graph with deterministic
ordering and keyset paging. The equivalent MCP tool is
`list_codeowners_ownership`, which re-dispatches into this same route rather
than running its own Cypher.

No-Regression Evidence: this is a new read surface, not a rewrite of an
existing hot-path query, so there is no prior-shape baseline to diff against.
Both the paginated list and the effective-owner last-match lookup anchor on
uniquely constrained properties (`Repository.id`, `CodeownerTeam.ref`), so
each traversal resolves through an index rather than a label scan; no local
NornicDB/Neo4j checkout was available to capture a live `PROFILE` (stated per
cypher-query-rigor). `go test ./internal/query -run
'TestCodeownersOwnership|TestResolveEffectiveRepositoryOwner' -count=1` and
`go test ./internal/mcp -run 'TestCodeownersOwnershipToolIsRegistered|TestResolveRouteMapsCodeownersOwnership'
-count=1` cover the bounded list (defaults, invalid limit, truncation,
keyset cursor threading, backend-unavailable) and all three effective-owner
precedence branches plus both dependency error paths.
`TestCodeownersOwnershipScopedCallerCannotReadUngrantedRepository` (issue
#5419 Phase 4b) is the scoped-grant cross-tenant leak proof: a caller granted
only `repo-a` requesting `repository_id=repo-b` gets an empty page even
though the graph and correlation store both hold real `repo-b` data, while a
caller granted `repo-b` (or unscoped) sees it.

Observability Evidence: the handler reuses the existing query-handler
envelope (`WriteSuccess` + `BuildTruthEnvelope` with
`TruthBasisAuthoritativeGraph`) and the shared `GraphQuery.Run`/`RunSingle`
adapters, and registers one new span name, `query.codeowners_ownership`
(`telemetry.SpanQueryCodeownersOwnership`), alongside every other
query-handler span. It adds no graph write, queue, worker, or runtime knob.

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
the content read model, including public-safe counts for explicit workflow
image refs, unresolved templated image refs, and ambiguous multi-image
commands. It never returns raw shell commands. `live_run_correlations` reports
only reducer-owned run correlation rows. `run_artifact_evidence` is a third,
separate bridge block derived only from returned live rows that carry an
admitted artifact digest or image reference. Ambiguous artifact matches stay
`state=ambiguous` and do not become exact build/deploy truth. When static
workflow image refs are present but the reducer has no live run rows, the
response keeps `correlations=[]` and marks
`run_artifact_evidence.reason=workflow_image_ref_static_only`; otherwise static
workflow-only repositories mark
`live_run_correlations.reason=static_workflow_only_live_run_correlation_missing`
instead of implying the repository has no CI/CD evidence.
`evidence_summary.missing_evidence[]` gives stable public-safe missing-hop
classes for the source-to-image bridge without exposing provider URLs, image
refs, repository names, or shell commands. Current classes include
`source_to_ci_run_evidence_missing`, `ci_run_to_image_artifact_evidence_missing`,
`live_ci_provider_evidence_unavailable`, `ci_cd_evidence_missing`,
`ci_cd_run_correlation_missing`, `static_workflow_evidence_unavailable`,
`workflow_image_ref_unresolved`, and `workflow_image_ref_ambiguous`. The hosted
GitHub Actions collector runtime can now poll bounded live run, job, and
artifact metadata through claim-driven workflow targets. Representative remote
target-story proof still must show either exact live artifact bridge evidence or
these named API/MCP missing-hop classes for the selected target.

Container image identity count readbacks also expose `source_bridge` when
`source_repository_id` is present. A scoped zero count now carries the same
public-safe bridge classes as the list route, including
`deployment_image_reference_missing`, `image_registry_observation_missing`, and
`source_to_image_correlation_missing`, so target-story and MCP callers can
distinguish an honest missing bridge from an unclassified aggregate zero.

No-Regression Evidence: `go test ./internal/workflowimage ./internal/facts ./internal/collector ./internal/reducer ./internal/storage/postgres ./internal/query -run 'TestExtract|TestCICDRunFactKindsAndSchemaVersions|TestBuildStreamingGenerationEmitsWorkflowImageEvidence|TestBuildContainerImageIdentityDecisionsUsesWorkflowImageEvidenceAsSourceAnchor|TestBuildCICDRunCorrelationDecisionsUsesWorkflowImageEvidence|TestListActiveCICDRunCorrelationFactsQueryIsArtifactBoundedAndPaged|TestCICDListRunCorrelationsExplains(StaticWorkflowImageEvidence|UnresolvedStaticWorkflowImageEvidence)' -count=1` fails if workflow image extraction, durable fact registration, collector emission, image-identity source anchoring, CI/CD correlation, active image-identity lookup, or repository-scoped CI/CD summary classes drift.

No-Observability-Change: workflow image evidence reuses the existing Git collector fact stream, container-image identity reducer counters, CI/CD run-correlation reducer counters, bounded Postgres fact reads, query spans, truth envelope, and HTTP status/error bodies. The change adds no new runtime service, queue domain, worker, graph query, graph write shape, metric instrument, metric label, or runtime knob; operators still diagnose collection through fact counts and collector spans, reducer admission through existing outcome counters, and readback through `query.ci_cd_run_correlations`.

No-Regression Evidence: `go test ./internal/query -run 'TestCICDListRunCorrelationsExplains(StaticWorkflowOnlyEvidence|LiveRunEvidence|WorkflowArtifactDigestEvidence|WorkflowImageRefEvidence|AmbiguousArtifactEvidence|NoEvidence|StaticWorkflowImageEvidence)|TestBuildCICDEvidenceSummaryNamesUnavailableLiveProviderEvidence|TestOpenAPISpecIncludesCICDRunCorrelations' -count=1` and `go test ./internal/mcp -run 'TestDispatchToolCICDRunCorrelationsPreserves(ArtifactEvidenceSummary|MissingEvidenceSummary)|TestResolveRouteMapsCICDRunCorrelationsToBoundedQuery' -count=1` fail if the CI/CD list response or MCP transport stops distinguishing static workflow artifacts, provider run rows, digest/image bridges, ambiguous artifacts, explicit missing-hop classes, and no-evidence pages.

No-Observability-Change: `evidence_summary` reuses the existing bounded CI/CD query span (`query.ci_cd_run_correlations`), repository-scoped content-store file lookup, Postgres/content query instrumentation, truth envelope, and HTTP status/error bodies. The bridge is computed from the already-returned reducer fact page after repository selector resolution, deterministic ordering, `limit+1` truncation probing, and exact payload predicates; it does not add a graph read, broad graph scan, graph write, reducer work, queue, worker, metric instrument, metric label, or runtime knob.

No-Regression Evidence: `go test ./internal/query -run 'TestCICD(ListRunCorrelationsUsesImageRefAnchor|RunCorrelationQueryFiltersImageRef|RunCorrelationAggregate(Count|Inventory)PassesImageRefFilter)' -count=1` and `go test ./internal/mcp -run 'TestResolveRouteMapsCICDRunCorrelation' -count=1` failed before CI/CD list/count/inventory routes and MCP dispatch accepted `image_ref`, then passed after the bounded query predicates and tool schemas included it.

No-Observability-Change: `image_ref` reuses the existing CI/CD query handler spans (`query.ci_cd_run_correlations`, `query.ci_cd_run_correlation_aggregate`) and Postgres fact-read instrumentation; the change adds no worker, queue, graph write, metric instrument, or metric label.

## Vulnerability Impact

Vulnerability impact, scanner contract, findings, counts, inventory, explain,
scanner report, workload, and remediation plan routes live in
[Vulnerability Impact](vulnerability-impact.md). They remain source-evidence and
reducer-truth reads; provider alert reconciliation stays below because it
explains provider-only and stale rows separately from admitted impact truth.
`GET /api/v0/supply-chain/advisories`

Browses the vulnerability-intelligence catalog. Unlike the evidence and detail
routes below, it needs no advisory, package, repository, service, or workload
anchor: it lists canonical advisories so the console can browse known CVE
intelligence (for example `CVE-2021-44228` Log4Shell) that is not yet reachable
in any indexed service.

The caller must provide `limit` (1–200, default 50 in callers). Optional
filters:

- `severity` — canonical severity label (case-insensitive), e.g. `CRITICAL`
- `ecosystem` — affected-package ecosystem (case-insensitive), e.g. `npm`
- `kev` — `true` to limit the page to CISA KEV advisories
- `q` — prefix match against canonical advisory id, CVE id, GHSA id, affected
  package id, or PURL

Rows carry summary columns only: `advisory_key`, `canonical_id`, `cve_id`,
`ghsa_id`, `severity_label`, `cvss_score`, `kev`, `ecosystems[]`,
`package_ids[]`, `published_at`, and `sources[]`. Results are ordered by
descending `cvss_score` then ascending `advisory_key`, with keyset pagination
through `next_cursor.after_cvss` and `next_cursor.after_advisory_key` (both must
be sent together on the next page).

These rows are known source intelligence and do **not** imply repository, image,
workload, or deployment impact. Service reachability remains the separate impact
findings routes. Use
`GET /api/v0/supply-chain/vulnerabilities/{advisory_id}` for full source
evidence on one advisory.

This route reads active `vulnerability.cve`, `vulnerability.affected_package`,
and `vulnerability.known_exploited` source facts only.

No-Regression Evidence: `go test ./internal/query -run 'AdvisoryCatalog' -count=1` covers required/bounded limit, severity/ecosystem/KEV/`q` filter pass-through, keyset cursor parsing, truncation/`next_cursor`, backend-unavailable handling, and the active source-fact read-model query shape (`TestAdvisoryCatalogQueryUsesActiveSourceFactReadModel`).

No-Observability-Change: the catalog read reuses the per-route `query.advisory_catalog` handler span (route + capability attributes) and the existing `eshu_http_request_duration_seconds` histogram, matching the established advisory evidence read pattern. It adds no graph write, worker, queue, new metric instrument, metric label, or runtime deployment knob.

`GET /api/v0/supply-chain/advisories/evidence`

Lists source-only advisory evidence. The caller must provide `limit` and at
least one bounded anchor:

- `cve_id`
- `advisory_id`
- `package_id`
- `repository_id`
- `service_id`
- `workload_id`

The optional `source` filter narrows an already anchored request. Rows group
GHSA, CVE/NVD, OSV, GitLab Advisory Database, FIRST EPSS, CISA KEV, CWE,
reference, affected-package, affected-product/CPE, withdrawn, affected-range,
and fixed-version evidence under one canonical advisory identity. They also
surface `source_disagreements[]` for severity, withdrawn status, affected
ranges, and fixed versions without selecting a winner.

`repository_id` accepts a canonical source repository id or the same human
repository selectors used by repository context routes. Repository, service,
and workload scopes first select active reducer-owned impact findings, then use
only those finding CVE/advisory/package anchors to read advisory source facts.
Provider-alert-only rows are not used to seed advisory evidence.

This route reads active `vulnerability.*` source facts only. It does not emit
or imply reducer-owned package, repository, image, workload, deployment, or
reachability impact. Use the impact routes below when you need admitted impact
truth.

No-Regression Evidence: `go test ./internal/query -run 'TestSupplyChainListAdvisoryEvidence(ResolvesRepositoryScopedFindings|RejectsUnknownRepositorySelectorBeforeRead)|TestAdvisoryEvidenceQueryDerivesRepositoryScopeFromImpactFindings|TestAdvisoryEvidenceQueryUsesActiveSourceFactReadModel' -count=1` and `go test ./internal/mcp -run 'TestResolveRouteMapsAdvisoryEvidenceToBoundedQuery|TestAdvisoryEvidenceToolSchemaAdvertisesRepositoryScope' -count=1` cover repository selector resolution, reducer-impact-only advisory anchoring, active source-fact reads, and MCP schema/dispatch parity.

No-Observability-Change: repository/service/workload advisory evidence scopes reuse the existing `query.advisory_evidence` span, Postgres query instrumentation, truth envelope, and MCP dispatch logging. The change adds no graph write, worker, queue, metric instrument, metric label, or runtime deployment knob.

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
| `subject_digest`, `digest`, `image_ref` | exact, missing-evidence driven | Impact, SBOM, and container-image read models. |
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
- `image_ref`
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
`environment`, `severity`, `profile`, `priority_bucket`, `image_ref`,
`min_priority_score`, `suppression_state`, and `include_suppressed` filters and
default to the same low-noise precise, unsuppressed semantics. `sort`,
`after_finding_id`, and list pagination remain list-only controls.

`image_ref` is an exact reducer-owned image reference filter on impact finding
payloads. It does not infer tags from repository names or container image
registries; findings appear for an image reference only after reducer evidence
has joined explicit OCI image identity, SBOM attachment/component, package, and
advisory facts.

No-Regression Evidence: `go test ./internal/projector -run 'TestBuildProjectionQueues(ContainerImageIdentityForOCIReferrer|SupplyChainImpactForSBOMComponentEvidence|SupplyChainImpactForOCIReferrerEvidence|SBOMAttestationAttachmentForOCIReferrerEvidence)' -count=1`, `go test ./internal/reducer -run 'Test(SupplyChainImpactHandlerExpandsActiveEvidenceFromOCIReferrer|SBOMAttestationAttachmentHandlerLoadsActiveDocumentEvidenceForReferrer)' -count=1`, `go test ./internal/query -run 'TestSupplyChainListImpactFindingsUsesImageRefScope|TestSupplyChainImpactFindingQueryUsesActiveFactReadModel|TestPostgresSupplyChainImpactReadinessScansForImageRefScope' -count=1`, `go test ./internal/mcp -run TestResolveRouteMapsSupplyChainImpactFindingsToBoundedQuery -count=1`, and `go test ./internal/storage/postgres -run 'TestListActive(SupplyChainImpactFactsQueryIsPackageBoundedAndPaged|SBOMAttestationAttachmentFactsQueryIsDigestBoundedAndPaged)' -count=1` failed before reducer/source-fact routing, active evidence reads, API/MCP `image_ref` forwarding, and image-ref readiness anchoring were wired, then passed.

No-Observability-Change: the change reuses existing projector/reducer work items, reducer execution spans and counters, Postgres query timing, `query.supply_chain_impact_findings`, `query.supply_chain_impact_aggregate`, MCP dispatch logging, readiness envelopes, and durable `reducer_supply_chain_impact_finding` / `reducer_sbom_attestation_attachment` payloads. It adds no worker, queue domain, graph write, metric instrument, metric label, or runtime deployment knob.

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
`subject_digest`, `image_ref`, `workload_ids[]`, `service_ids[]`,
`environments[]`, `catalog_entity_refs[]`, and `catalog_owner_refs[]` only when
reducer-owned package/SBOM/image evidence joins to explicit deployment or
service-catalog facts. Ambiguous images, stale deployment evidence, missing
workload links, or missing service/environment links stay in
`missing_evidence[]` instead of being inferred from repository, tag, workload,
or service names. Exact repository-scoped service-catalog correlation evidence
is still attached to the finding path and can preserve catalog entity and owner
anchors. When that correlation lacks explicit `service_id` or `workload_id`
anchors, the row reports `service/workload catalog anchor missing` instead of
saying service-catalog correlation evidence is absent.

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
  `ambiguous_scope`, `readiness_unavailable`, or `unsupported`.
  `ambiguous_scope` means a single explain scope matched multiple reducer-owned
  findings and the caller must narrow the request. `readiness_unavailable`
  preserves the findings page when the coverage lookup fails. `unsupported`
  means Eshu observed real target evidence the matcher cannot resolve; callers
  MUST NOT interpret it as clean or affected.
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
  `target_collection_incomplete`, `ambiguous_scope`,
  `readiness_unavailable`, or `unsupported_targets`. Reasons stay
  deduplicated, sorted, and free of package names or advisory bodies; the list
  is empty on `ready_*` states so callers cannot see contradictory "ready" +
  "missing" signals.
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
  `truncated` to know if more pages exist. Explain responses with
  `outcome: ambiguous_scope` withhold individual findings but still report the
  matched reducer finding count observed by the ambiguity probe, so callers do
  not misread the refusal as zero findings.

Readiness is computed from existing source and reducer facts only. The
endpoint never invents findings; it surfaces counts and freshness so a zero
or partial answer can be interpreted correctly.

`GET /api/v0/supply-chain/impact/explain`

Explains one reducer-owned vulnerability impact finding or one bounded
advisory/package/repository/image/workload/service path. The caller must
provide either:

- `finding_id`; or
- `advisory_id` or `cve_id` plus at least one of `package_id`,
  `repository_id`, `subject_digest`, `image_ref`, `workload_id`, or
  `service_id`.

The route never performs a whole-graph explain. It reads one active
`reducer_supply_chain_impact_finding` fact and hydrates only the
`evidence_fact_ids` referenced by that finding. If a composite scope matches
more than one finding, the route returns `outcome: ambiguous_scope` with the
same readiness and missing-evidence envelope and asks for `finding_id` or a
narrower advisory/package/repository/image/workload/service anchor. If the
scope is bounded but no finding exists, it returns `outcome: no_finding` with
readiness and missing-evidence reasons instead of implying the target is safe.
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
refusal envelope, and no-evidence readiness response.

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
`document_id`, `document_digest`, `repository_id`, `workload_id`, or
`service_id`. `repository_id` accepts a canonical source repository id or the
same human repository selectors used by repository context routes: repository
name, repo slug, indexed path, local path, or remote URL. Unknown or ambiguous
repository selectors fail before the attachment read instead of returning a
misleading empty page. Repository, workload, and service anchors are source
scopes over reducer-owned attachment facts; callers still need
`attachment_scope`, `canonical_writes`, and `missing_evidence` before treating
a row as attached image evidence.

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
Repository, workload, or service scoped SBOM attachment count readbacks expose
`missing_evidence[]` as well. When the count is zero because the selected source
has no admissible image identity, callers see
`repository_to_image_evidence_missing`, `workload_to_image_evidence_missing`, or
`service_to_image_evidence_missing`; when an image exists without an attachment,
callers see `image_to_sbom_evidence_missing`.

Each row carries up to 100 deduplicated `component_evidence` rows per document.
The reducer sorts the complete component tuple lexicographically with `fact_id`
as the final tiebreaker before applying that write-time cap, so replay order
cannot change the persisted preview. `component_count` remains the full distinct
tuple count before the cap and `component_evidence_truncated` is `true` when the
persisted row count is lower. Each row also carries `dependency_relationships` (declared `sbom.dependency_relationship`
edges between components: `from_component_id`, `to_component_id`,
`relationship_type`, `relationship_origin`, `fact_id`) and `external_references`
(declared `sbom.external_reference` rows: `component_id`, `reference_type`,
`reference_url`, `reference_locator`, `fact_id`). Both are declared-evidence
rows, not resolved graph truth: a `from`/`to`/`component_id` value is not
validated against the document's indexed components, so it may be dangling.
The reducer bounds each document to 100 dependency-relationship rows and 50
external-reference rows (deduplicated on the full field tuple, sorted
lexicographically with `fact_id` as the final tiebreaker before the cap so the
kept rows are deterministic across replays); `dependency_relationship_count`
and `external_reference_count` report the full distinct-tuple count computed
before that cap, and `dependency_relationships_truncated` /
`external_references_truncated` are `true` when the count exceeds the number
of rows returned.

No-Regression Evidence: for the `sbom.dependency_relationship` /
`sbom.external_reference` wiring (issue #5370), `go test ./internal/reducer -run
'Test(BuildSBOMAttestationAttachmentDecisionsSurfacesDependencyAndExternalReferenceEvidence|BuildSBOMAttestationAttachmentDecisionsAllowsDanglingComponentIDs|DependencyRelationshipEvidenceRowsDedupesCapsAndCountsBeforeCap|ExternalReferenceEvidenceRowsDedupesCapsAndCountsBeforeCap|BuildSBOMAttestationAttachmentDecisionsQuarantinesDependencyMissingDocumentID|SBOMAttestationAttachmentHandlerLoadsActiveDependencyAndExternalReferenceEvidence)'
-count=1` and `go test ./internal/query -run
'Test(DecodeSBOMAttestationAttachmentRowSurfacesDependencyAndExternalReferenceEvidence|SupplyChainListSBOMAttestationAttachmentsSurfacesDependencyAndExternalReferenceWire)'
-count=1` and `go test ./internal/storage/postgres -run
'TestListActiveSBOMAttestationAttachmentFactsQueryIsDigestBoundedAndPaged'
-count=1` failed before those two kinds had a decode case in
`buildSBOMAttachmentIndex` (they were queue-routed but silently dropped), then
passed after the reducer index, decision payload, Postgres active-evidence
loader allowlist, and HTTP/MCP read model all carried the bounded evidence
through.

For the `sbom.component` write-time bounding this change (#5412) adds,
`sbom.component` already had a decode case — the pre-#5412 defect was that
`ComponentEvidence` was unbounded end to end (`ComponentCount ==
len(components)`, no cap, no dedupe, no deterministic sort) and
`ComponentEvidenceTruncated` did not exist on the Row/Result structs.
`go test ./internal/reducer -run
'Test(ComponentEvidenceRowsDedupesCapsAndCountsBeforeCap|ComponentEvidenceRowsIsOrderInvariantAcrossShuffledDuplicates)'
-count=1` and `go test ./internal/query -run
'Test(DecodeSBOMAttestationAttachmentRowSurfacesComponentEvidenceTruncation|SupplyChainListSBOMAttestationAttachmentsUsesBoundedStore)'
-count=1` fail against the pre-#5412 shape (no cap/dedupe/sort, no truncation
flag) and pass against this change: the reducer test also proves the
`fact_id` sort tiebreak is genuinely order-invariant (byte-identical output
across 40 independently shuffled input orderings of a duplicate-carrying
component set), not merely correct for one fixed input ordering.

No-Observability-Change: the bounded component preview and existing declared-evidence
fields reuse the existing reducer execution spans and counters, Postgres query
timing, `query.sbom_attestation_attachments`, MCP dispatch logging, and durable
`reducer_sbom_attestation_attachment` payloads. This wires already-typed, already-queue-routed
fact kinds into the existing SBOM attachment decode/write/read path. It adds
no new reducer domain, worker, queue, graph write, metric instrument, span, or
runtime flag. Operators continue to diagnose the path through the existing
`sbom_attestation_attachment` reducer counter by status,
`query.sbom_attestation_attachments` span, and Postgres fact-read
instrumentation; the new evidence is bounded (100/50 rows per document) at
reducer write time, so it does not change the fact payload's operating size
class.

No-Regression Evidence: `go test ./internal/reducer -run
'Test(BuildSBOMAttestationAttachmentDecisionsClassifiesSubjectsAndTrust|ScannerWorkerGeneratedSBOMFactsAdmittedByReducerAttachment|PostgresSBOMAttestationAttachmentWriterPersistsAllStatuses)'
-count=1` and `go test ./internal/query -run
'Test(SupplyChainListSBOMAttestationAttachmentsUsesBoundedStore|OpenAPISpecIncludesSBOMAttestationAttachments)'
-count=1` failed before SBOM attachment decisions and readbacks carried
attachment scope and missing anchor evidence, then passed after the reducer fact
payload, API row, OpenAPI fragment, and MCP tool description exposed that
truth.

Each attestation row also carries `slsa_provenance_predicate_type` and
`slsa_provenance_builder_id`, decoded from a joined `attestation.slsa_provenance`
fact keyed by the statement's `statement_id`. Both are empty when no SLSA
provenance fact joined the statement: an attestation whose predicate type is
outside the closed SLSA provenance set, or whose predicate could not be
decoded (see the SBOM runtime collector's `malformed_slsa_predicate`
warning), never fabricates these fields. A well-formed predicate with no
reported `builder.id` still surfaces `slsa_provenance_predicate_type` with an
empty `slsa_provenance_builder_id`.

No-Regression Evidence: `go test ./internal/collector/sbomruntime -run
'TestClaimedSource.*SLSA'
-count=1` and `go test ./internal/reducer -run
'TestBuildSBOMAttestationAttachmentDecisions.*SLSAProvenance'
-count=1` and `go test ./internal/query -run
'Test(DecodeSBOMAttestationAttachmentRowSurfacesSLSAProvenance|SupplyChainListSBOMAttestationAttachmentsSurfacesSLSAProvenanceWire)'
-count=1` and `go test ./internal/mcp -run
'TestDispatchToolSBOMAttestationAttachmentsSurfacesSLSAProvenance'
-count=1` failed before the SBOM runtime collector emitted
`attestation.slsa_provenance` and the reducer decoded and joined it by
`statement_id` (issue #5371: the fact kind, typed struct, and schema already
existed but no collector produced the fact), then passed after the collector
emitter, reducer index/decision/writer, and HTTP/MCP read model all carried
the SLSA provenance evidence through.

No-Observability-Change: this wires an already-typed, already-queue-routed
fact kind into the existing SBOM attachment decode/write/read path, following
the same shape as issue #5370. It adds no new reducer domain, worker, queue,
graph write, metric instrument, span, or runtime flag. Operators continue to
diagnose the path through the existing `sbom_attestation_attachment` reducer
counter by status, `query.sbom_attestation_attachments` span, and the
`eshu_dp_reducer_input_invalid_facts_total` quarantine counter for a
malformed `attestation.slsa_provenance` fact.

No-Regression Evidence: `go test ./internal/query ./internal/mcp -run
'Test(SupplyChainListSBOMAttestationAttachments(AcceptsRepositoryScope|ResolvesRepositorySelectors|RejectsInvalidRepositorySelectors)|SBOMAttestationAttachmentAggregateRoutes(ForwardSourceScopes|ResolveRepositorySelectors|RejectInvalidRepositorySelectors|DoNotDropServiceScope)|SBOMAttestationAttachmentAggregateQueriesFilterSourceScopes|ResolveRouteForwardsSBOMRepositoryScopeToHTTPContract|DispatchSBOMAggregateRepositoryScopeReturnsScopedCount)'
-count=1` proves repository-scoped SBOM attachment list, count, inventory, and
MCP aggregate calls keep the source scope, resolve human repository selectors
before the bounded read, and reject unknown or ambiguous selectors instead of
dropping the scope or reading unscoped global attachment totals.
`scripts/test-verify-remote-e2e-target-story.sh` proves release target-story
validation starts SBOM proof from `target_repository_id` and only adds
`subject_digest` when the manifest supplies a digest anchor.

No-Observability-Change: this changes SBOM attachment readback routing only.
It adds no new SBOM read model, worker, queue, graph write, metric instrument,
span, metric label, runtime flag, or broad read path. Human repository
selectors use the existing content-catalog lookup before the bounded SBOM
attachment read; canonical repository ids skip that lookup. Operators continue
to diagnose the path through the existing reducer attachment counter by status,
`query.sbom_attestation_attachments`,
`query.sbom_attestation_attachment_aggregate`, repository selector errors,
Postgres fact-read instrumentation, `canonical_writes`, `attachment_scope`, and
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

## Performance Evidence

- No-Regression Evidence: #3489 introduces the canonical `truth.Evidence` type
  and non-destructive `Canonical()` bridges; each former evidence model is
  retained and additionally projected as `unified_evidence`. Baseline: existing
  relationship, correlation, and documentation evidence emission. After:
  identical emission plus the additive unified projection — no reducer domain,
  Cypher, graph write, worker, lease, batch size, or concurrency knob changed.
  Backend/version: NornicDB and Neo4j compatibility paths unchanged. Input
  shape: existing evidence facts and documentation packets. Terminal queue and
  row counts: unaffected. Verified by the full suite (6149 tests across 540
  packages) staying green. Safe because it is a type-unification with bridges,
  not a behavior change.
- No-Observability-Change: no new or changed spans, metrics, metric labels,
  logs, or status fields on any runtime path; the existing
  `query.sbom_attestation_attachments` span and truth-envelope fields are
  unchanged.
