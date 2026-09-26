# Service-Scope Changed-Since

`GET /api/v0/freshness/services/changed-since` answers "what changed for this
service since a prior service materialization generation?" A service is not an
ingestion scope, so this surface diffs a per-service generation lineage
(`service_materialization_generations`, one active generation per ingestion
scope and `service_id`)
over generation-stable evidence snapshots (`service_evidence_snapshots`) keyed by
a generation-independent `service_evidence_key` (for example
`ownership:<service_id>:<owner_ref>`, `deployment:<service_id>:<identity>`
(where the deployment identity is a digest of the resolved deployment
relationship's generation-independent natural key),
`runtime:<service_id>:<platform_kind>:<environment>:<workload_ref>` (where
`workload_ref` is the durable `WorkloadInstance` id, which carries no resolution
or materialization generation id), or `dependencies:<service_id>:<identity>`
(where the dependency identity is a digest of the resolved dependency
relationship's generation-independent natural key — `DEPENDS_ON` / `USES_MODULE`
/ `READS_CONFIG_FROM` — and, like deployment, its `resolved_id` embeds the
resolution generation and is therefore not a stable diff key), or
`incidents:<service_id>:<provider>:<provider_incident_id>:<slot>:<evidence_kind>:<evidence_id>`
(one durable routing identity per PagerDuty incident-routing slot, where
`evidence_id` is the source fact's generation-independent `StableFactKey` or
durable content-entity id, never the generation-bearing envelope `FactID`).

Required parameters: `service_id` (exact) and `since_generation_id` (a prior
service generation id). Optional `scope_id` selects one ingestion scope's
lineage (see below), and optional `sample_limit` (default 25, max 200) caps the
per-classification sample handles. The response carries the resolved
`service_id`, the `scope_id` of the lineage it read, `unattributed`,
`since_generation_id`, `current_active_generation_id`, and a `categories`
array. The surface reports the `ownership` (#1943), `deployment`
(#1985), `runtime` (#1986), `dependencies` (#1987), `docs` (#1988),
`incidents` (#1989), and `vulnerabilities` (#1990) families. Each category carries
exact `counts` for `added`, `updated`, `unchanged`, `retired`, and `superseded`,
plus bounded `samples` (`stable_fact_key` carrying the `service_evidence_key`,
`fact_kind` carrying the evidence family) per classification and a
per-classification `truncated` flag. Service payloads use stored Go MD5
fingerprints; repository facts use SHA-256 over persisted JSONB text. Neither
surface collapses retired or superseded into `unchanged`.

An unknown `service_id` returns `service_not_found`; an unresolved
`since_generation_id` returns `not_found`; a service with no current active
generation returns `unavailable=true` (and a `building`/`unavailable` freshness
state) rather than zero deltas. The capability key is
`freshness.service_changed_since`. The MCP equivalent is
`get_service_changed_since` (argument `scope_id`) and the CLI helper is `eshu
freshness service-changed-since` (flag `--scope-id`).

A catalog service id is relative to the catalog that declared it, so two
tenants may both declare `component:default/api`. Since #6475 every lineage row
records the ingestion scope that wrote it, and each scope keeps its own
lineage for the id. The route reads exactly one:

- With `scope_id`, it reads that scope's lineage. A `scope_id` outside the
  caller's grant returns `service_not_found`, the same answer as a scope that
  holds nothing.
- Without `scope_id`, the route serves the one admitted lineage with an active
  generation. A lineage with no active generation has nothing to diff against,
  so it never makes an active one ambiguous. More than one admitted lineage
  with an active generation (or, when none is active, more than one attributed
  lineage) returns `409 Conflict` with error code `ambiguous`; `error.details`
  carries `status` (`ambiguous`), `service_id`, `scope_ids` (only the scope ids
  the caller may read, sorted, at most 20) and `truncated`. The route never
  picks one silently. Re-ask with one of the listed scope ids.
- A lineage written before #6475 whose writing scope could not be recovered is
  unattributed. Only an unscoped caller can read it, and only when the id has
  no attributed lineage with an active generation; the response then sets
  `unattributed=true` and an empty `scope_id`. Once the service has been
  re-materialized under a scope, the attributed lineage is served instead, so
  a pre-upgrade baseline generation id from an unattributed row no longer
  resolves and returns `not_found`; take a new baseline from the attributed
  lineage.

The prior generation is looked up inside the lineage the route resolved, so a
`since_generation_id` from another lineage (including another tenant's)
returns the same `not_found` as an id that does not exist.

Scoped tokens and restricted browser sessions receive only lineages of granted
scopes and repositories: the grant binds in SQL on the lineage row's
`scope_id`, directly or through the repository-kind ingestion scope's
`source_key`. An ungranted `service_id` returns `service_not_found`, byte for
byte the answer for an unknown id. An all-scope bearer token or console
session carries no grant for that filter to bind, so it is refused with a
`403` under `hosted_multi_tenant` and under any unrecognized governance mode.
`local_no_policy`, `hosted_single_tenant`, and an unset mode (which defaults to
`local_no_policy`) admit it when it is bound to one tenant and workspace, and it
then reads every lineage, as an admin credential does on every other route
there.

The incidents family's production loader is held behind a durable
PagerDuty-provider-to-Eshu-catalog service-id join that is a tracked #1989
follow-up, and the vulnerabilities family's loader is held behind a durable
service-to-repository-to-package-to-advisory join that is a tracked #1990
follow-up, so their rows materialize once those joins exist. All six service
evidence families now ship the emitter, category, delta surface, and a
nil-tolerant loader seam.

Performance Evidence: a request runs one resolve statement over the requested
service id's lineage rows, reached through
`service_materialization_generations_observed_idx` (`service_id` leading), with
the grant joined through `ingestion_scopes_pkey`. It then runs one prior-generation
primary-key probe, one counts statement over the two generations'
`service_evidence_snapshots`, and one samples statement per non-empty
classification bucket, each capped at `sample_limit + 1` keys. A 409 ambiguity
answer runs only the resolve statement; a scoped not-found adds one `EXISTS`
probe on `service_id`. Laptop-local `EXPLAIN (ANALYZE, BUFFERS)` on an
80,492-row lineage table put the scope-aware resolve at about 0.04 ms slower
than the single-pick resolve it replaced (median 0.047 ms before, 0.072 to
0.084 ms after, under 0.3 ms in every sample); see
`docs/internal/evidence/6475-service-lineage-readers.md`. No whole-graph or
cross-service scan is performed.

Observability Evidence: each request emits one
`query.freshness_service_changed_since` span carrying the service id, the since
and current generation ids, the changed count, `unavailable`, and
`eshu.service_changed_since.unattributed` (the diff read an unattributed legacy
lineage). A `409` answer records `eshu.service_changed_since.ambiguous_scope_count`,
a count and never the scope ids. A handler-level refusal records
`eshu.service_changed_since.grant_refused=true` with
`eshu.service_changed_since.grant_refused_reason` set to `empty_grant` or
`not_granted` (the id holds lineage rows, none in a granted scope); the response
body for either stays the ordinary `service_not_found`. The route adds no
worker, queue, graph query, or metric label.
