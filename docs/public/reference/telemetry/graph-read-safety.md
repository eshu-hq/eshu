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

`POST /api/v0/code/visualize` was a known gap, tracked separately: it followed
the contract at runtime but had no OpenAPI path entry at all — a gap that
predated this contract, so it could not advertise `503`/`504` until that entry
existed. #5762 gave it a complete `openapi/paths/code/graph.go` entry (request
schema, response schema, and the `503`/`504` bounded-read responses) and
removed it from `.github/openapi-known-drift.txt`; it now maps like every
other guarded route and is proven by
`TestOpenAPIDocumentsBoundedGraphReadFailuresOnEveryGuardedRoute`. The gap was
not a scanner blind spot in `scripts/verify-openapi.sh` — the route's
`mux.HandleFunc("POST /api/v0/code/visualize", ...)` registration (`code.go`)
is a direct string literal, the same shape the verifier resolves for every
other route — it was a stale, TODO-tagged entry in the known-drift suppression
list that the verifier subtracts before comparing, present since #3781.

That suppression list, `.github/openapi-known-drift.txt`, used to accept two
different claims on equal footing: "this route is intentionally not part of
the OpenAPI surface" (permanent) and "this route's fragment predates the
verifier" (a deferred TODO wearing a permanent-exclusion entry). #5762 closed
the marker-based shape of the second category and added a best-effort check
against a fixed list of prose deferral phrases: `scripts/verify-openapi.sh`
now fails before it evaluates any route drift if a known-drift comment line
contains a TODO/TO-DO/FIXME/XXX/HACK/TBD/WIP-style deferral marker (including
plural forms), one of a fixed set of prose deferral phrases such as
"not written", "written yet", "pending", "predates", or "later", if a route
entry has no preceding, substantive justification comment of its own, or if a
justification repeats the immediately preceding justification (whitespace and
a leading `#` normalized away first) — see the "Known-drift exclusions"
section of
[3738-openapi-discipline](https://github.com/eshu-hq/eshu/blob/main/docs/internal/design/3738-openapi-discipline.md)
for the exact rules. The marker check is a closed, conventional token set and
is exhaustive for those tokens. The prose check is not exhaustive: it is a
best-effort convenience kept against a fixed phrase list, never a guarantee
that every English deferral is caught — successive reviews of this file kept
finding more surviving prose shapes ("Deferred:", "Revisit once…", "Backlog
item…", and others), and that pattern is expected to continue for any phrase
not already on the list. Neither check, nor the justification-comment
requirement, can catch a plausible-sounding but false justification someone
writes instead.

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
infrastructure-typed entities, `repositoryInfrastructureEntityLimit` --
Kubernetes, Terraform, Terragrunt, ArgoCD, Helm, Kustomize, Crossplane, and
CloudFormation entity types; both the content-path and graph-path reads
filter to that type set before applying the bound, so the count is never the
repository's total entity count of any type): a HEALTHY read that lands
past the bound is disclosed with the distinct `infrastructure_truncated`
reason (never both reasons for the same read — a failed read has no rows to
bound, and a bounded read did not fail), and the stage log carries an
unconditional `truncated` boolean attribute alongside the conditional
`failure_class` attribute. On the story route, `infrastructure_truncated`
also sets the response's top-level `truncated` field (and therefore
`answer_metadata.truncated`), OR'd together with the row-narrative bound
above (P3 review follow-up to #5764): either bound landing past its limit is
disclosed the same way. Since #6810, `entry_points`, `languages` (context
only; the story's languages are part of the propagating narrative rows above),
`relationships`/`dependencies`, `relationship_overview`,
`source_tool_breakdown`, `consumers`, `api_surface` (`queryRepoAPISurface`) and
the deployable-unit relationship supplement
(`queryRepoDeployableUnitRelationshipOverview`) disclose a failed read the same
way: the response carries `<read>_read_degraded` in `partial_reasons` (story:
`limitations`) and the stage log carries `failure_class=<that reason>`. None
of them adds a `_truncated` reason to `partial_reasons`; only `api_surface` is
bounded, and it discloses its bound as `detail_truncated` on its own panel. The
deployment/infrastructure overview builder
(`artifacts.LoadDeploymentArtifactOverview`, whose error both routes discard)
is neither bounded nor disclosed, and neither route's full call graph
has an exhaustive audit yet: a graph-read failure there still folds into the
"no rows" path with no `failure_class` signal. See
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
backend transaction deadline expired while the caller was still live. This
includes a *shared* per-label-loop budget (see "Shared bounded-read loops"
below), which IS the graph-read policy's own deadline even though the code
that created it lives in a handler, not `Neo4jReader` itself.
`caller_deadline` means an enclosing deadline that is NOT the graph-read
policy's own budget expired first (e.g. an MCP dispatch timeout, or any other
caller-imposed `context.WithTimeout`). This includes a caller deadline set
*outside* a shared `querycontract.WithBoundedGraphReadDeadline(For)` budget
that happens to be shorter than it and so fires first -- only a deadline the
policy itself created counts as `deadline`; a shorter enclosing deadline is
not counted as a graph-policy deadline even though it expires the same
wrapped context.

The `neo4j.query` span records the same outcome plus
`eshu.graph_read.attempts`, `eshu.graph_read.configured_deadline_ms`,
`eshu.graph_read.query_name`, and
`eshu.graph_read.statement_fingerprint` -- the first 12 hex characters of the
sha256 of the statement's redacted shape (see below), computed on every bounded
read regardless of outcome (issue #7035). Statements that differ only in
literal values share one fingerprint. Bound parameters never enter the
fingerprint or any other graph-read signal, and inline literals are redacted
before they are hashed or logged. Slow, deadline, and unavailable reads also
emit `query.graph_read.warning` with `pipeline_phase="query"`, a bounded
`failure_class`, `duration_seconds`, `graph_query_name`, and two fields that
name the exact statement shape: `graph_read.statement_fingerprint` (the same
value as the span attribute) and `graph_read.statement_head` (the same redacted
shape, truncated to 300 characters with an `...[truncated]` marker when it
exceeds that bound).

`eshu.graph_read.query_name` (and the `graph_query_name` log field) is a
bounded, low-cardinality caller-supplied name for the route/handler that issued
the read (e.g. `code_quality.complexity`, `code_quality.refactoring`,
`entity.context`, `platform_impact.deployment_chain`), set via
`querycontract.WithGraphQueryName` and defaulting to `unnamed` when a caller
sets none, so the attribute is never silently absent. It makes a bounded-read
timeout or slow read attributable to a specific route without reading Cypher
text (issue #7006's telemetry gap).

The redacted shape is what makes the head safe for ad-hoc Cypher. Bound
`$parameters` are never part of the statement text, but the read-only Cypher
route (`POST /api/v0/code/cypher` and its `execute_cypher_query` MCP tool, plus
`POST /api/v0/code/visualize`) runs a caller's query with every value written
inline, so the reader redacts the
statement itself before it reaches a hash, span, or log
(`go/internal/query/graph/statement`):

- Replaced with `<REDACTED>`: integers, floats (including exponent, `f`/`d`
  suffix, hex `0x..` and octal `0o..` forms, Neo4j 5 digit separators such as
  `4111_1111`, and a minus sign that touches the digits), single-quoted
  strings, and double-quoted strings, including the bounds of a variable-length
  range such as `*1..5`. Every character that trails a number's digits belongs
  to the number, as in Neo4j's lexer. Backslash escapes and doubled quotes stay
  inside their string.
- Kept verbatim: keywords, `true`, `false` and `null`, identifiers (digits
  inside an identifier such as `n1` are not literals), labels, relationship
  types, property keys, `$parameters`, and backtick-quoted identifiers.
- Dropped: `//` and `/* */` comments, because free text in a comment can carry
  the same values a literal would. Whitespace runs collapse to one space, where
  whitespace is ASCII space, tab, newline and the other ASCII separators plus
  every Unicode space character (no-break space, ideographic space, and the
  rest of Neo4j 5's lexer whitespace set), so a pasted `= 1234` cannot turn its
  digits into a kept identifier.
- Fail closed: an unterminated string, block comment, or backtick identifier
  redacts to the end of the statement.

The slow threshold defaults to 1 second (it was 2 seconds before #7035, so
expect more `slow` warnings after upgrading) and is configurable through `ESHU_GRAPH_READ_SLOW_THRESHOLD` (a Go duration
string, for example `750ms`). An unset value keeps the default; an invalid or
non-positive value logs `query.graph_read.invalid_slow_threshold` once at
startup and falls back to the default rather than silently disabling the
warning.

### Matching a fingerprint to NornicDB

`graph_read.statement_fingerprint` identifies the Eshu-side statement shape; it
has no equivalent on the NornicDB side, so matching relies on the statement
text instead. The fingerprint hashes Eshu's redacted text, which NornicDB never
sees, so hashing NornicDB's `query` field will not reproduce it.

- **NornicDB slow-query log** -- NornicDB's own `event="slow_query"` log
  (threshold `NORNICDB_SLOW_QUERY_THRESHOLD`, default 5s) carries `plan_hash`
  and a literal-redacted `query` field truncated to 500 characters. NornicDB's
  `RedactLiterals` (`pkg/cypher/redaction.go`) replaces exactly three lexer
  token classes with `<REDACTED>`: integer, float, and double-quoted
  `STRING_LITERAL` tokens. It does **not** replace single-quoted strings (a
  separate `CHAR_LITERAL` token, `pkg/cypher/antlr/CypherLexer.g4`), hex or
  octal numbers, or comments, and it copies whitespace through byte-for-byte,
  so the `query` field keeps the statement's original newlines and indentation
  (checked by running `RedactLiterals` at NornicDB `d97f02c1`).
  Eshu's `graph_read.statement_head` replaces every numeric and string literal class, drops
  comments, and collapses whitespace, so a raw comparison fails for any
  statement that spans more than one line or carries a single-quoted string.
  Normalize NornicDB's `query` field to Eshu's shape before comparing: collapse
  each run of whitespace to one space and trim, then replace each single-quoted
  string with `<REDACTED>` (and any hex/octal number or comment, which are
  uncommon in Eshu's own queries). Then check whether Eshu's head is a prefix
  of the first 300 characters of that normalized text. Both sides fold a minus
  sign that touches a number into the literal, and both redact each bound of a
  `*1..5` range, so those need no adjustment. For example, a NornicDB `query`
  field reading `"MATCH (n:Repo)\n  WHERE n.name = 'x' RETURN n"` normalizes to
  `"MATCH (n:Repo) WHERE n.name = <REDACTED> RETURN n"`, which compares directly
  against an Eshu `graph_read.statement_head` of the same text. Because Eshu's
  default threshold (1s) is well below NornicDB's default (5s), a read that is
  `slow` in Eshu but absent from the NornicDB log is expected -- lower
  `NORNICDB_SLOW_QUERY_THRESHOLD` to correlate reads in the 1-5s range.

  Known limitation on current NornicDB builds (checked on upstream commits
  `6ac958a9`, `f2163176` and `3691d795`; `f2163176` is the commit behind the
  `fix-6915-f2163176` image): the per-database executors that serve Bolt and HTTP
  `/db/<name>/tx/commit` queries are built without the slow-query logger or
  threshold (`pkg/bolt/server.go` `newDatabaseScopedCypherExecutor`,
  `cmd/nornicdb/main.go` `ConfigureDatabaseExecutor`), so Eshu's reads
  produce no `slow_query` record at any threshold. Even where a record is
  emitted, `plan_hash` is always `0000000000000000`, and some `CALL { ... UNION
  ... }` and variable-length statements are logged as the literal
  `<REDACTED>`. Until NornicDB fixes this, use the pprof capture below with
  `graph_read.statement_head` as the correlation path.
- **NornicDB pprof capture** -- when a statement shape recurs as `slow` or
  `deadline`, enable `NORNICDB_PPROF_ENABLED` (bind address
  `NORNICDB_PPROF_LISTEN`, default `127.0.0.1:9091`) and capture a profile with
  `GET /debug/pprof/profile?seconds=N` for `N` at least as long as the
  observed `duration_seconds`. There is no fingerprint inside the pprof
  profile; use the capture's wall-clock window plus the statement head/shape
  already identified from the Eshu warning and the NornicDB slow-query log to
  find the matching stack.

### Shared bounded-read loops

A route that resolves an id through several sequential label-anchored reads --
`GET /api/v0/entities/{entity_id}/context` and
`POST /api/v0/infra/relationships`, both trying one label per candidate in a
loop until a match or exhaustion, then one final unlabeled read so an id on
any other label still resolves as it did before the loop existed -- derives ONE shared deadline once, before
the loop, via `querycontract.WithBoundedGraphReadDeadline`, and reuses it for
every candidate read. This bounds the whole loop by the same 10-second budget
a single statement gets, instead of paying that budget once per candidate
label. When the shared budget itself expires mid-loop, `Neo4jReader`
classifies the outcome as `deadline` (not `caller_deadline`) and emits the
same `query.graph_read.warning` with `graph_query_name` any other deadline
does. `querycontract.WithBoundedGraphReadDeadline(For)` creates that deadline
with `context.WithTimeoutCause`, and `graphReadResult` checks
`context.Cause(parentCtx)` for that specific cause -- not merely whether the
context descends from a `WithBoundedGraphReadDeadline(For)` call -- so a
caller deadline set outside the bounded ctx that is shorter than the shared
budget and fires first still classifies as `caller_deadline`, never
`deadline`.

`GET /api/v0/entities/{entity_id}/context` runs one more graph read after the
loop, the repo-identity hydration that only fires for a Workload or
WorkloadInstance row with no repo identity. That read is not on the loop's
shared deadline: a loop that spent nearly all of its budget on misses would
otherwise starve it. It gets its own bounded read and the same `entity.context`
`graph_query_name`, so its slow-read and deadline telemetry is attributed to the
route. The route's worst case is the loop budget plus one bounded read.

`POST /api/v0/infra/relationships`'s request span additionally records
`eshu.entity_anchor_labels_tried`, an integer count of how many anchor reads
the loop issued before matching or exhausting the set -- the 1-based index of
the label that matched, `len(impactRelationshipAnchorLabels)+1` when only the
unlabeled fallback matched, and the same value on a full miss. `GET /api/v0/entities/{entity_id}/context` logs the
same count as `labels_tried`/`labels_total` structured fields (plus a
`failure_class` of `deadline` or `graph_read_error`) on its own separate
handler-level warning when the loop ends in an error before resolving --
distinct from `Neo4jReader`'s `query.graph_read.warning`, since a handler-level
anchor-loop warning has no single Cypher statement to attribute to the reader's
own per-read span.

Session-close failures emit `query.graph_read.session_close_failed` with
`pipeline_phase="query"` and `failure_class="session_close_error"`. Because
cleanup has its own one-second bound, total request wall time may extend beyond
the graph-execution budget by up to that cleanup allowance.

Treat `slow` as completed work that remained inside the budget. Treat
`deadline` as exhausted graph-read work and investigate the query plan. Treat
`unavailable` as a health or connectivity event and inspect graph backend
health before retrying.

## Infra resource aggregate read model

`GET /api/v0/infra/resources/count` and `/inventory` (and their MCP tools)
read content-derived infrastructure nodes from the Postgres
`infra_resource_entities` table once its backfill has completed. CloudResource,
TerraformStateResource, and the Terraform state projector's TerraformModule
and TerraformOutput nodes are still read from the graph, in one pass.
`eshu_dp_infra_inventory_reads_total{route,source}` counts every read by
route (`count`, `inventory`) and serving store (`read_model`, `graph`).

- After a deploy, `source="read_model"` should become the unscoped share of
  traffic within one backfill (`category=cloud` reads stay `graph`, because
  that category has only graph-only labels). If `source="graph"` stays high
  for other unscoped traffic, either the backfill has not recorded its
  marker (`eshu_dp_infra_inventory_backfill_runs_total{outcome="failed"}`
  rises and the `infra_inventory.backfill.failed` log says why; failed
  attempts retry with backoff, 30s doubling to 10m, in the same process) or
  fence marks are not draining (`eshu_dp_infra_inventory_dirty_repos` above
  zero; see below). The `/admin/status` field `infra_inventory` names which.
- `eshu_dp_infra_inventory_derives_total{outcome}` counts the content
  writer's derives. `skipped_not_installed` means a writer ran before
  migration 109; the content write succeeded and the backfill covers the
  repository later. `ok_unfenced_session` means the derive succeeded on a
  connection without the derive-aware writer setting, so the fence marked
  its content writes; the `infra_inventory.derive.unfenced_session` log and,
  at startup, `postgres.session_unfenced` name the cause (usually a
  connection pooler that does not forward `eshu.infra_inventory_writer`).
  `error` fails the content write, which then retries.
- Runbook: `eshu_dp_infra_inventory_dirty_repos > 0` after every pod runs the
  new release means an unfenced session is still writing `content_entities`:
  a pooler stripping the writer setting, an operator `psql` session, or an
  unknown binary. Unscoped count and inventory routes serve from the graph
  until the reducer drains the marks; `/admin/status` field
  `infra_inventory` shows `state=fenced`, the count, and the oldest mark's
  age. If `dirty_oldest_age_seconds` keeps growing, the reconcile loop is not
  running (`ESHU_INFRA_INVENTORY_RECONCILE_ENABLED=false`, or the reducer is
  still an older release).
- The reducer's reconcile loop compares each repository's table rows with its
  content rows (row count plus a per-row hash).
  `eshu_dp_infra_inventory_reconcile_total{outcome}` counts repositories
  checked: `match`, `suspect`, `repaired`, `fenced`, or `error`. A single mismatch is
  only `suspect`: a content Write commits its content rows before its derive
  runs, so a check between the two sees the table behind. Suspects are
  re-checked on the next cycle and repaired only if they still differ, so
  `repaired` means a repository differed on two checks at least one
  `ESHU_INFRA_INVENTORY_RECONCILE_INTERVAL` apart. That is a content writer
  that is not deriving (for example an older ingester or projector still
  running), a Write whose derive failed and has not retried yet, or, rarely,
  two checks that both landed inside Writes of a repository being re-indexed
  continuously. Each repair logs `infra_inventory.reconcile.drift` with
  `repo_id` and the row counts; the same `repo_id` repeating across walks is
  the drift signal to chase. A steady `suspect` count during heavy ingestion
  is normal. `error` also counts a whole cycle that failed (for example the
  repository listing), not only single repositories.
  `eshu_dp_infra_inventory_reconcile_duration_seconds` (failed cycles
  included) and the span `reducer.infra_inventory_reconcile` time each cycle.
  The walk position is persisted, so a restarted reducer resumes it. The loop
  does nothing until the backfill marker exists.
- `fenced` counts repositories an older binary (or manual SQL) wrote without
  deriving, typically during a rolling upgrade. Migration 109's triggers mark
  such a repository in the same statement; unscoped reads stay on the graph
  (`eshu_dp_infra_inventory_reads_total{source="graph"}`) while any repository
  is marked, and the next reconcile cycle re-derives it and clears the mark.
  Each one logs `infra_inventory.reconcile.fenced` with `repo_id`, and the
  cycle span carries `eshu.infra_inventory.repos_fenced`. `fenced` that keeps
  appearing after a rollout finished means something outside the Eshu
  runtimes is still writing `content_entities`.
- Scoped-token reads always count as `source="graph"`.
- The content writer logs the per-Write derive as stage
  `derive_infra_inventory` (`path_count`, `rows_deleted`, `rows_inserted`,
  `duration_seconds`). Its statements are timed by
  `eshu_dp_postgres_query_duration_seconds`.
