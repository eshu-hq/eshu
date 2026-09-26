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
- Without `scope_id`, a single lineage the caller may read is served. More than
  one returns `409 Conflict` with error code `ambiguous`; `error.details`
  carries `status` (`ambiguous`), `service_id`, `scope_ids` (only the scope ids
  the caller may read, sorted, at most 20) and `truncated`. The route never
  picks one silently. Re-ask with one of the listed scope ids.
- A lineage written before #6475 whose writing scope could not be recovered is
  unattributed. Only an unscoped caller can read it, and only when the id has
  no attributed lineage; the response then sets `unattributed=true` and an
  empty `scope_id`.

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

Performance Evidence: the diff is bounded by the requested `sample_limit` and
keyed by `(scope_id, generation_id, stable_fact_key)`. A request evaluates the
classification diff once: one statement materializes the classified keys and
returns each non-empty bucket's exact count with its first `sample_limit+1` keys
by `stable_fact_key` (a lateral join per bucket). It replaces a counts statement
plus one samples statement per non-empty bucket, each of which re-scanned both
generations (1+N diffs); rows are identical, and a live differential proves it.
On a 2.2M-row local fixture the summed `EXPLAIN ANALYZE` time fell from a median
of 65.12 s (8 statements) to 10.88 s (1 statement); see
`docs/internal/evidence/7127-changed-since.md`. Each per-generation scan still
anchors on `fact_records_scope_generation_idx` (`scope_id, generation_id`) with a
hash join on `stable_fact_key`, and equal minimum digests on duplicate-key groups
trigger a sorted multiset comparison. One diff stays O(generation size) and is
tracked in #7127. No whole-graph or cross-scope scan is performed.

No-Observability-Change: the surface adds the bounded
`query.freshness_changed_since` span with low-cardinality scope-id,
since-generation, current-generation, changed-count, and unavailable attributes;
it adds no worker, queue, graph query, or new metric label.
