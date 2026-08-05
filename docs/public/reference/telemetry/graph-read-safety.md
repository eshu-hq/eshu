# Graph-read safety

Eshu applies one 10-second budget to each logical NornicDB or Neo4j read made
through `Neo4jReader`. An earlier caller deadline wins. The reader passes the
remaining budget to the backend transaction, so collection and backend work
share the same clock. A typed retryable connectivity failure may open one fresh
session, but both attempts remain inside the original budget.

After execution, Eshu closes the driver session with a separate one-second
cleanup context. This lets the driver return connections to its pool even when
the read budget expired. A cleanup failure emits a sanitized
`query.graph_read.session_close_failed` warning; it does not expose driver
text, query text, or backend addresses.

The policy also covers graph reads performed during API and MCP startup,
including the cloud-resource owner-ledger backfill. Each backfill page uses the
same bounded reader rather than a raw driver session.

Route budgets still bound non-graph work. For example, the read-only Cypher
route has a 30-second outer budget, while its graph execution is limited by the
tighter 10-second reader budget. The reader deadline is a safety net; it does
not replace query-shape fixes.

## Caller responses

Every graph-backed HTTP route maps these sentinels onto the same stable status
and error envelope, so a bounded-availability failure is never reported as a
generic HTTP 500 — or, for a route whose graph read happens during repository
selector resolution, a misleading HTTP 400. This spans the read-only Cypher API
and its `execute_cypher_query` MCP tool, repository inventory/context/story, the
code search, relationships, call-graph, dead-code, flow, and quality routes,
entity and service resolution, the impact and change-surface family,
infrastructure and image reads, package-registry and service-catalog
correlations, the supply-chain evidence and security-alert-reconciliation
routes, the secrets-IAM grant-posture summary, and the service-story seam.
Their MCP tools therefore surface `backend_timeout` / `backend_unavailable`
rather than a generic transport failure:

| Condition | HTTP status | Error code | Message |
| --- | --- | --- | --- |
| Reader budget expired | `504` | `backend_timeout` | `graph query exceeded its deadline` |
| Graph unavailable | `503` | `backend_unavailable` | `graph temporarily unavailable; retry after graph health is restored` |

Responses do not expose Bolt addresses, Cypher text, or raw driver errors.

One known gap, tracked separately:

- `POST /api/v0/code/visualize` does follow the contract at runtime, but has no
  OpenAPI path entry at all — a gap that predates this contract — so it cannot
  advertise `503`/`504` until that entry exists.

`POST /api/v0/code/language-query` was the other gap. It now maps like every
other guarded route, reporting `symbol_graph.language_entities` — a
route-level capability minted for this route (#5761), not a reused id. The
route's own MCP tool, `execute_language_query`, is already bound to five
`symbol_graph.*` facets (`decorators`, `argument_names`, `class_methods`,
`imports`, `inheritance`) in `specs/capability-matrix.v1.yaml`, but each of
those names one specific semantic facet, not "look up entities of kind K in
language L" — what this route actually does across its graph-backed,
graph-first-content, and content-only entity-type families. Reusing
`code_search.symbol_lookup` (a different route's capability, owned by
`POST /api/v0/code/symbols/search`) was considered and rejected: sharing one id
across two unrelated routes with different failure semantics would make an
operator's capability-keyed triage ambiguous about which route actually
failed.

Repository context and story map these sentinels for their base repository
lookup (`repositoryBaseCypher`, a `RunSingle`) and, as of #5764, for their
primary scalar/narrative auxiliary reads too: the summary counts
(`file_count`/`workload_count`/`platform_count`/`dependency_count`), the
story's workload/platform/language narrative rows, and the deployment
evidence pointers. Before #5764 those auxiliary reads silently folded a
bounded graph-read error into the same "no rows" fallback path as a genuine
empty result, so a deadlined or unavailable graph answered a fabricated zero
count or empty narrative instead of `503`/`504` — indistinguishable from a
real answer to the caller.

The story's workload/platform/language narrative rows are also bounded (500
rows, `repositoryStoryStringRowLimit`) so a single narrow read cannot run
unbounded. A healthy read that lands past that bound is capped in Go rather
than fabricating an exact-looking count: `workload_count` and `platform_count`
are `len()` of these bounded lists, not separate `count()` queries, so a
capped list silently presented as exact would be the same fabricated-value
defect this issue exists to remove. The cap is disclosed with the
`story_rows_truncated` reason in `limitations` /
`answer_metadata.partial_reasons`, and `answer_metadata.truncated` reports
`true` for that response.

Not every auxiliary read on these two routes maps the sentinels, however:
`infrastructure` is a genuine auxiliary panel layered on top of those headline
facts. A graph-read failure there degrades to an empty result with the
response still answering `200` rather than failing the whole context/story
read; the degradation surfaces in `partial_reasons` (context) or `limitations`
/ `answer_metadata.partial_reasons` (story) plus a `failure_class` on the
`repository_query.stage_completed` / `service_query.stage_completed` log for
the failing stage. That same infrastructure read is bounded too (5000
entities, `repositoryInfrastructureEntityLimit`): a HEALTHY read that lands
past the bound is disclosed with the distinct `infrastructure_truncated`
reason (never both reasons for the same read — a failed read has no rows to
bound, and a bounded read did not fail), and the stage log carries an
unconditional `truncated` boolean attribute alongside the conditional
`failure_class` attribute. `entry_points`, `languages` (context only; the
story's languages are part of the propagating narrative rows above),
`relationships`/`dependencies`, `relationship_overview`,
`source_tool_breakdown`, `consumers`, `api_surface` (`queryRepoAPISurface`),
the deployable-unit relationship supplement
(`queryRepoDeployableUnitRelationshipOverview`), and the deployment/
infrastructure overview builder (`loadDeploymentArtifactOverview`, whose error
both routes discard) remain a known, unwidened gap — including but not
limited to this list, since neither route's full call graph has an
exhaustive audit yet: a graph-read failure on any of those still silently
folds into the same "no rows" (or discarded-error) path as a genuine empty
result, with no `failure_class` signal. See
`go/internal/query/AGENTS-evidence-history-3.md` (part 3, linked from
`go/internal/query/AGENTS.md`) for the full per-site propagate/degrade
rationale and the reasoning behind the narrower scope, and
`go/internal/query/AGENTS-evidence-history-2.md` (part 2) for the P1 review
follow-up that bounded and disclosed the story's workload/platform/language
row reads.

Every route in `boundedGraphReadRoutes` follows the table above and advertises
both statuses in the OpenAPI spec. That list is the enforced set — read it as
"these routes are proven to map", not as a proof that no other graph-backed
route exists.

`TestOpenAPIDocumentsBoundedGraphReadFailuresOnEveryGuardedRoute` keeps the spec
from drifting away from that set, but note what it can and cannot do: it asserts
that every route **on its list** documents `503` and `504`. It cannot detect a
graph-backed route that was never added to the list, nor a handler whose
guard covers only some of its branches. Adding a graph-backed route therefore
means adding it to `boundedGraphReadRoutes` as well — derive that list from the
call graph rather than by inspection, since a guard often sits in a helper
several frames below the registered handler.

Routes that reach Postgres or the content store rather than the graph are
unaffected, unless their failure carries a graph-read sentinel, as with the
graph-first fallback paths above — a route whose content-only entity-type
families never touch the graph keeps those branches' existing status, but a
branch that reaches the graph first and falls back to content on a bounded
graph-read sentinel still reports `503`/`504` for that sentinel.

## Operator signals

Use `eshu_dp_neo4j_query_duration_seconds{operation="read"}` with its closed
`outcome` label: `success`, `slow`, `recovered`, `deadline`, `unavailable`,
`caller_deadline`, `canceled`, or `error`. `deadline` means the graph policy or
backend transaction deadline expired while the caller was still live.
`caller_deadline` means the enclosing request deadline expired first; it is not
counted as a graph-policy deadline.

The `neo4j.query` span records the same outcome plus
`eshu.graph_read.attempts` and
`eshu.graph_read.configured_deadline_ms`. Slow, deadline, and unavailable
reads also emit `query.graph_read.warning` with `pipeline_phase="query"`, a
bounded `failure_class`, and `duration_seconds`.

Session-close failures emit `query.graph_read.session_close_failed` with
`pipeline_phase="query"` and `failure_class="session_close_error"`. Because
cleanup has its own one-second bound, total request wall time may extend beyond
the graph-execution budget by up to that cleanup allowance.

Treat `slow` as completed work that remained inside the budget. Treat
`deadline` as exhausted graph-read work and investigate the query plan. Treat
`unavailable` as a health or connectivity event and inspect graph backend
health before retrying.
