# Dead Code Reachability Spec

This page defines the contract for Eshu's `code_quality.dead_code` capability.
It is a current runtime contract, not a historical implementation plan.

The short version: "no incoming calls" is not enough to call code dead. A
symbol can be reachable through entrypoints, public API rules, framework
registration, callbacks, generated runtime wiring, reflection, configuration,
or language dispatch. Eshu only claims `exact` dead-code truth when those roots
are modeled for the queried scope.

## Current Status

`code_quality.dead_code` is supported in graph-backed profiles and currently
returns `derived` truth.

The capability is exposed through:

- `POST /api/v0/code/dead-code`
- `POST /api/v0/code/dead-code/investigate`
- `POST /api/v0/code/dead-code/cross-repo`
- MCP tools `find_dead_code`, `investigate_dead_code`, and
  `find_cross_repo_dead_code`
- CLI command `eshu analyze dead-code`

The capability matrix marks `local_authoritative`, `local_full_stack`, and
`production` as supported with `max_truth_level: derived`. `local_lightweight`
is unsupported because dead-code analysis requires the graph plus root metadata.

The implementation scans graph or content-backed entity candidates, removes
symbols with incoming `CALLS`, `IMPORTS`, `REFERENCES`, `INHERITS`, or
`EXECUTES` edges, applies the default root policy, and returns bounded results
with truncation metadata.

`POST /api/v0/code/dead-code/cross-repo` keeps the producer candidate scan
bounded to an explicit `repo_id`, then classifies each active candidate against
active-generation consumer evidence from the materialized reachability read
model. A deterministic consumer row marks the symbol `live_by_consumer`.
Ambiguous ownership, stale generations, missing evidence coverage, and
scoped-token-hidden consumers are returned as `unknown_needs_evidence`; they
are never converted into dead-code truth.

### Consumer coverage

A symbol with no consumer row is only `dead` when no consumer repository the
answer is judged against is a coverage gap. No row means "not called" only for a
repository whose snapshot holds its calls; a repository whose active generation
has no snapshot, a truncated one, or one built under an older verdict schema
epoch contributes no rows, or wrong ones, for symbols it really does call
(#7547). Those three cases are all the check detects; see the limits below.

Each request runs one coverage statement against
`code_reachability_repository_watermarks`. A repository scope with an active
generation is a gap when it has no watermark for that generation, or the
watermark is `truncated`, or its `verdict_schema_epoch` is below the current
`CodeReachabilityVerdictSchemaEpoch` (the writer bumps it when verdict semantics
change, and a snapshot built earlier carries the old ones), **and** that
generation has a `code_calls` or
`inheritance_edges` projection intent (completed or still pending). On a full
generation a refresh intent (a projection intent whose payload `action` is `refresh`)
does not count, because it carries no edge. On a delta generation any such intent counts,
because a delta generation never gets a watermark. A repository with no such
intent has no code edges, cannot be a consumer, and is complete
without a watermark. A repository with intents is never excluded for having zero
roots: its edges can still sit on a chain from a rooted repository to the
producer symbol. The check covers the consumers the request named (a named
repository with no active generation is incomplete), otherwise the caller's
grant, otherwise every repository with an active generation. When
it finds a gap, a candidate with no strong live consumer evidence comes back
`unknown_needs_evidence` with the reason `consumer_coverage_incomplete` instead
of `dead`. Strong consumer evidence still wins: a symbol a covered consumer
calls stays `live_by_consumer`. A store that cannot answer the check yields
`cross_repo_evidence_unavailable`, never `dead`.

The response carries `consumer_coverage`: `complete`, `retryable`, `incomplete`,
`incomplete_repo_ids` (at most 25), `incomplete_truncated` and `coverage_summary`. It is omitted when
the check did not produce an answer: no candidate needed classifying, the
evidence read was unavailable, or the store cannot answer the check.

`incomplete` says why each repository is a gap and whether a snapshot is expected
without action.
Each entry has `repository_id`, `state`, `generation_id`, `retryable`, and a
plain-language `reason` and `next_step` (#7594):

| `state` | Meaning | `retryable` |
| --- | --- | --- |
| `no_snapshot_yet` | The active generation has code edges but no reachability watermark yet. The reducer has not built the snapshot; one is expected. | `true` |
| `older_epoch` | The snapshot was built under an older verdict schema epoch and is expected to refresh. | `true` |
| `truncated` | The snapshot is current but cannot prove a symbol is not called (no roots, or a depth cutoff). | `false` |
| `no_active_scope` | A repository the request named has no active repository scope, so nothing is being built. | `false` |

`reason` and `next_step` are fixed text per `state`, derived from the state
alone, with no extra query. For `truncated` the reason says Eshu cannot tell
which cause applies: the watermark stores only a boolean, so "no entry points
found" and "the walk hit its depth or size limit" look the same. They are left
out for a state this version does not know. The advice for a snapshot that
should clear by itself is a hint, like `retryable`, not a promise.

A request that named its own `consumer_repo_ids` is not told to name them
again: the gaps are the repositories it named. For `truncated` its `next_step`
is `Waiting will not clear this. Check whether this repository's framework
entry points are modeled.`, and `coverage_summary` ends with `Check whether
their framework entry points are modeled.`

`coverage_summary` is one sentence for the whole list, such as `12 repositories
cannot be judged yet: 3 of them should clear on their own, 9 will not. Name the
repositories you care about with consumer_repo_ids.` It counts only the listed
gaps, because the check never counts the repositories it checked. When the list
is cut it says `At least N repositories` and `the list was cut`, with no cap
quoted, because the cut list can hold fewer than the cap. When every listed gap
is an id that is not indexed, it ends with `Check the ids, or index the
repositories.` A complete answer
says `No repository checked has a coverage gap.`

`generation_id` is the repository scope's active generation, the snapshot
expected. It is left out for `no_active_scope`. One watermark that is both
truncated and older-epoch is reported `truncated`. A repository with several
scopes in a gap is reported once: a truncated scope first, so `retryable` never
hints at a refresh that another scope of the same repository would defeat, then the
lowest generation id.

`retryable` is a hint, not a promise. `true` means a snapshot is expected to
appear or refresh without action. It can stay `true` for a long time when the
active generation is a delta generation, or a full generation whose reducer work
did not complete, because the loader schedules only complete runs; such a
repository answers `no_snapshot_yet`, or `older_epoch` if a gated-out run left an
older watermark, until a later full generation replaces it. The classification
(`unknown_needs_evidence`) does not change. The top-level `retryable` is `true`
only when every gap is retryable: each
listed gap is retryable and `incomplete_truncated` is `false`, because a cut list
hides gaps that may not be retryable. It is `false` when `complete` is `true`.
`incomplete_repo_ids` is the same list as `incomplete`, in the same order, kept
for callers that read it before the detail existed. The ids are always returned
sorted. A request that names consumers or a grant returns the lowest-sorting 25. An
unscoped request with no named consumers stops at the first gaps with no ordering,
so with more than 25 gaps which ids come back is an arbitrary subset and
`incomplete_truncated` is `true`. It reports no count
of the repositories it checked, because that count would force a full scan; the
statement stops at the first gaps.

Name `consumer_repo_ids` to narrow a request. An unscoped request with no named
consumers is judged against every repository with an active generation, so one
repository with code edges and no complete snapshot makes every symbol unknown.
`complete` means no gap was found, not that every caller was found. Three
limits are known:

- A consumer whose watermark is not truncated, in a language with no public-API
  root kind, can still hide an exported caller no root reaches.
- A stale or partly drained snapshot with `truncated = false` reads complete until
  the reducer rebuilds it, including one beside a pending `code_calls` or
  `inheritance_edges` intent. The intent only decides whether a missing or
  truncated or older-epoch watermark counts. Building a snapshot from an incomplete run is a
  writer-side matter, outside this check.
- A repository with zero roots is flagged only once the writer stamps its
  watermark `truncated`; before that it reads complete.

The ids are
repositories the request named, the caller's grant, or, for an unscoped caller,
any repository, so a scoped token never learns of a repository outside its grant.
An ungranted consumer with a partial snapshot is not covered by this check,
because the hidden-consumer probe that finds ungranted consumers needs rows to
exist.

## Exactness Rule

Dead-code truth is `exact` only when all of these are true for the queried
scope:

1. The language and framework root model is implemented.
2. The authoritative call and reference graph is present.
3. Generated code and test-code policy is known.
4. Dynamic behavior that can affect reachability is modeled or scoped out.
5. The response can explain the root categories, modeled roots, maturity, and
   exactness blockers that were applied.

If those conditions are not met, Eshu must return `derived`,
`derived_candidate_only`, `non_code_iac_evidence`, `unsupported_language`, or an
explicit unsupported-capability error. It must not silently promote a partial
root model to exact cleanup-safe truth.

## Root Categories

Every dead-code result explains the root categories used by the analyzer. The
current response includes:

| Category | What it protects |
| --- | --- |
| `language_entrypoints` | `main`, `init`, `__main__`, runtime startup hooks, and equivalent language entrypoints. |
| `generated_and_tool_owned` | Generated or tool-owned symbols that should not be judged by ordinary inbound code calls. |
| `library_public_api` | Exported or public package surfaces where external consumers can call the symbol. |
| `cli_command_roots` | Cobra, Click, Typer, package-bin, and equivalent command handlers. |
| `http_and_rpc_roots` | HTTP route handlers, RPC handlers, framework controller actions, and equivalent request handlers. |
| `framework_callback_roots` | Workers, schedulers, lifecycle hooks, interface/trait/protocol callbacks, DI callbacks, tests, and runtime callback registrations. |

The implementation also reports modeled entrypoints, public API roots,
framework roots, semantic roots, and notes that explain the current derived
model.

## Output Contract

Dead-code responses include analysis metadata so humans and agents can see why a
candidate is safe, ambiguous, or suppressed.

Important response fields:

| Field | Meaning |
| --- | --- |
| `truth.level` | Current truth level. Dead-code is `derived` unless a future scope is proven exact. |
| `results[].classification` | One of `unused`, `ambiguous`, `derived_candidate_only`, or `unsupported_language` for returned candidates. |
| `truncated` | True when either displayed results or the candidate scan window was truncated. |
| `candidate_scan_truncated` | True when the shared candidate scan limit was reached before all selected labels were exhausted. |
| `candidate_scan_limit` | Maximum bounded raw rows across all candidate labels selected by the request. |
| `candidate_scan_limit_per_label` | Maximum share one selected candidate label may consume from the shared raw-row limit. |
| `candidate_scan_pages`, `candidate_scan_rows` | Actual bounded pages and rows inspected. |
| `analysis.root_categories_used` | Root categories applied by the analyzer. |
| `analysis.frameworks_recognized` | Framework values observed in result metadata. |
| `analysis.reflection_modeled` | True only when the requested language has modeled reflection reachability evidence. |
| `analysis.reflection_modeled_languages` | Languages with modeled reflection reachability evidence; currently `java`. |
| `analysis.modeled_entrypoints` | Entrypoint root kinds currently modeled. |
| `analysis.modeled_framework_roots` | Framework and callback root kinds currently modeled. |
| `analysis.modeled_public_api` | Public API root kinds currently modeled. |
| `analysis.dead_code_language_maturity` | Per-language maturity from the query package. |
| `analysis.dead_code_language_exactness_blockers` | Named blockers that prevent exact cleanup-safe truth. |
| `analysis.dead_code_observed_exactness_blockers` | Blockers observed on returned candidates. |
| `analysis.frameworks_without_root_model` | Frameworks observed in result metadata for which dead-code has no root model, grouped by language. Empty until a producer emits per-result `framework` for a language with a modeled-frameworks entry. |
| `analysis.tests_excluded` | Whether test-owned code is excluded by default. |
| `analysis.generated_code_excluded` | Whether generated code is excluded by default. |
| `analysis.user_overrides_applied` | Whether request-level exclusions were applied. |
| `analysis.iac_reachability_mode` | Always `not_modeled_by_code_dead_code` for this capability. |

Cross-repo packets return `candidate_buckets.dead`,
`candidate_buckets.live_by_consumer`, `candidate_buckets.unknown`, and
`candidate_buckets.suppressed`. Consumer evidence rows include
`consumer_repo_id`, `consumer_entity_id`, `evidence_family`, `citation`,
`confidence`, `confidence_label`, `resolution_method`, `generation_id`, and
`generation_status` so callers can cite why a symbol was kept live or why a
result needs more evidence.

See [Dead Code Language Maturity](dead-code-language-maturity.md) for the
current language-by-language model.

No-Regression Evidence: issue #2706 / #2731 / #2732 / #2733 focused proof on
2026-06-18, updated for #7547. `go test ./internal/query/codequery/deadcode -run
'TestDeadCodeIncomingEntityIDs(CompleteSnapshotStillReadsLegacyEdgesForUnansweredEntities|TruncatedReachabilitySnapshotFallsBack|PrefersMaterializedReachabilityRows)|TestHandleDeadCodeReturnsDerivedTruthAndAnalysisMetadata|TestBuildDeadCodeAnalysisForLanguageReportsReflectionModeledTruth'
-count=1` proves a complete, non-truncated snapshot still runs the legacy
one-hop read for the entities it did not answer (this superseded the original
skip), truncated snapshots remain conservative, and reflection modeling is only
claimed for Java. `go test ./internal/query -run
'TestContentReaderCodeReachabilityIncomingEntityIDsUsesCrossRepoRows|TestOpenAPIDeadCodeMentionsHaskellRootsAndLanguageFilter'
-count=1` proves cross-repo materialized rows keep library symbols live through
stable entity IDs and the OpenAPI contract still names the Haskell roots and
language filter. `go test
./internal/storage/postgres -run 'TestCodeReachability' -count=1` proves the
watermark stores truncation truth, active-generation lookups still work, and
the entity-scoped reachability index is present for bounded cross-repo reads.
`go test ./internal/reducer -run
'TestCodeReachabilityProjectionRunner|TestBuildCodeReachabilityRows' -count=1`
proves transitive reachability projection and runner replacement behavior still
converge.

No-Observability-Change: the query path reuses existing `postgres.query` spans
and `db.operation=code_reachability_incoming_entity_ids`,
`code_reachability_coverage` (no longer emitted by the dead-code path after
#7547), and `dead_code_incoming_entity_ids` labels plus
the existing dead-code handler span and HTTP route metrics. The reducer path
keeps the existing code reachability completion log and truncation log line (it now carries a `truncation_reason` of `no_roots`, `max_visited`, or `max_depth`; `no_roots` logs at INFO, the other two at WARN); no
metric, worker, queue domain, runtime knob, graph write, or high-cardinality
telemetry label is added.

The `dead_code_incoming_entity_ids` span also carries
`dead_code_incoming.read_mode`: `active_run` when the legacy incoming read was
answered from the repository's active acceptance run because that run provably
holds its complete edge set, and `all_generations` when the read fell back to
every completed edge across retained generations (#7249).

## Default Policy

The default policy is intentionally conservative:

- Tests are excluded by default.
- Generated code is excluded by default.
- Parser-backed `dead_code_root_kinds` metadata suppresses cleanup candidates.
- Content metadata is preferred when available; graph metadata is still used
  when content is not available.
- Direct incoming code or reference edges suppress candidates. A materialized
  reachability snapshot answers for the entities it has rows for; every other
  entity still gets the producer-anchored one-hop incoming read, whether or not
  the snapshot's watermark reads complete, because a watermark does not prove
  its roots were adequate (#7547). The reducer builds a snapshot only for a
  run whose edge set is complete. An active delta generation (changed files
  only) activated after this change is not projected until the next full
  generation, so dead-code reads for its entities fall back to the legacy
  one-hop incoming read. A delta generation projected before this change
  keeps its partial snapshot until the next generation; that only adds
  reachability evidence, and the legacy read still covers the rest.
- SQL trigger routines are protected when reducer materialization creates
  parser-proven trigger-to-function `EXECUTES` edges.
- JavaScript and TypeScript candidates remain conservative because dynamic
  imports, property dispatch, framework loading, and declaration surfaces are
  not fully exact.
- HCL, Terraform, Terragrunt, Dockerfiles, Helm, Kustomize, Kubernetes, ArgoCD,
  and other infrastructure artifacts are not code dead-code candidates.

IaC cleanup is a separate workflow. Use `find_dead_iac` and the IaC
reachability docs instead of inferring infrastructure deadness from missing code
call edges.

## User Overrides

Repositories can declare additional dead-code roots or exclusions in
`.eshu.yaml`:

```yaml
dead_code:
  roots: []
  exclude_paths: []
  include_generated: false
```

Request-level decorator exclusions also set `analysis.user_overrides_applied`.

## Investigation Coverage

For a repository-scoped request, the `coverage` block of
`POST /api/v0/code/dead-code/investigate` and `investigate_dead_code` reports
content-index coverage from the content files only: `content_coverage_available`,
`file_count`, `languages`, `content_last_indexed_at`, and `freshness_state`.
`content_last_indexed_at` is the newest `indexed_at` of the repository's indexed
files; it no longer folds in the newest entity timestamp, so it can be older
than before when entities were indexed after the last file, and a repository
with entities but no indexed files reports `freshness_state` as `not_reported`.
The block no longer
carries `entity_count` (#7525): the count required a scan of every content
entity of the repository on each call and the investigation does not use it.
`entity_count` remains on `GET /api/v0/repositories/{repo_id}/stats` and
`/coverage`. A whole-index request reports no repository coverage.

## Response Bounds

The three MCP tools default `limit` to `25`, not `100`. A `limit` of `100`
produced replies over the MCP response budget on most measured repositories.
In one measured argument set at `limit` 20, `candidate_buckets` was 79.9% of
the `investigate_dead_code` reply and 89.8% of the `find_cross_repo_dead_code`
reply. `analyze_code_relationships` with `query_type` `dead_code` reaches the
same payload and shares the same `25` default. The MCP default is independent
of the HTTP defaults: an HTTP caller has no byte budget, so
`POST /api/v0/code/dead-code` and its siblings keep their own default of `100`
(maximum `500`). Raise `limit` on the MCP tools only when the reply still fits
the budget, and page `investigate_dead_code` with `offset` and `next_offset`.

The `suppressed` bucket, the modeled roots the default policy excluded, is
bounded by the smaller of `limit` and `50` on both the investigation and the
cross-repo routes. `suppressed_limit` reports that bound, and
`suppressed_truncated` is `true` when rows were dropped, so a short bucket is
never mistaken for the whole set.

The `suppressed` bucket is a bounded sample, not a census, and it has no
`offset`. To see more of it, narrow the request with `language` or `repo_id`.
The top-level `truncated` flag covers the active rows only; check
`suppressed_truncated` for the suppressed bucket.

The cross-repo route returns repository-boundary evidence once. A candidate with
no entity-level consumer evidence falls back to the repository's incoming
relationships, which are the same for every such candidate; they used to be
copied into each row's `consumer_evidence` (about 12 KB per row on the measured
ops-qa repository, enough to push a default-args reply over the MCP budget,
#7129). The route now puts them in `data.boundary_consumer_evidence`, with
`data.boundary_consumer_evidence_count`, after the request's `consumer_repo_ids`
selector and the caller's grant. Both keys are always present. The list is
empty, and the count 0, when no row used the fallback: every candidate had its
own entity evidence, the repository has no boundary relationships, or the caller
may see none of them. Under `evidence_detail` `full` it is not capped, so a
repository with many incoming relationships returns every one of them once
(`handles` caps it, below). A row that used the
fallback keeps `consumer_evidence: []` and carries
`consumer_evidence_source: "repository_boundary"`; every other row carries
`consumer_evidence_source: "entity"` and its own evidence. Each row also reports
`consumer_evidence_count`, the length of its own `consumer_evidence` (0 for a
fallback row). Classification, `needs_evidence_reasons` and
`hidden_consumer_evidence_count` are unchanged: they read the evidence, not the
row field. A client that read a fallback row's `consumer_evidence` for its
citations should read `boundary_consumer_evidence` instead. Entity-level
evidence stays on its row.

Per-entity evidence is bounded by `evidence_detail`, `full` or `handles`
(#7129). One SQL page can hold up to 1,000 evidence items across the candidate
rows, about 616 bytes each, so a page of 25 rows with 40 items each is roughly
1.2 MB of `consumer_evidence` alone. The MCP tool `find_cross_repo_dead_code`
defaults to `handles`, including when `consumer_repo_ids` is named; an explicit
`evidence_detail` wins. The HTTP route defaults to `full`, which keeps the shape
described above, and rejects any other value with HTTP 400.

Under `handles` each row's `consumer_evidence` is at most 5 group objects
`{consumer_repo_id, relationship_type, evidence_family, confidence_label,
item_count}`, one per distinct (consumer repository, relationship type,
evidence family), with `confidence_label` taken from the group's strongest item.
Groups are ordered by highest confidence, then `item_count`, then the three key
fields ascending, so the group that decided a `live_by_consumer` row is first
and is never the one cut. `consumer_evidence_count` stays the number of items
the row held, `consumer_evidence_group_count` is the number of groups before the
cap, and `consumer_evidence_handles_truncated` is `true` only on a row whose
groups were cut. A sentinel item such as `consumer_evidence_truncated` (empty
`consumer_repo_id`) groups like any other and may fall under the cap; its reason
stays in the row's `needs_evidence_reasons`. The hoisted
`boundary_consumer_evidence` becomes the same five-key objects, one per item
(`item_count` 1), strongest first, capped at 25;
`boundary_consumer_evidence_count` stays the total and
`boundary_consumer_evidence_truncated` is `true` only when the list was cut.

Shaping runs after classification, so buckets, `needs_evidence_reasons`,
`hidden_consumer_evidence_count`, `bucket_counts` and `analysis` are identical in
both modes. `data.evidence_detail` is always set. When handles reduced anything,
`data.evidence_detail_drilldown.full_rows` says how to get the rows back (repeat
with `evidence_detail` `full`, narrowing with `consumer_repo_ids` and `limit`)
and `truth.omissions` lists `candidate_buckets.consumer_evidence` (the total
items across rows) and `boundary_consumer_evidence` (the boundary total), each
with `detail: "handles"`. Item detail (citation, consumer entity id, generation)
is only in `full`. `handles` bounds the evidence to at most 17.5% of the 262,144-byte
MCP response budget; it does not reduce the row base. Each docstring is clipped to
512 bytes but echoed about six times per row, so on a repository with long
docstrings the reply can still be delivered as the full resource only (no
`structuredContent`) or exceed the budget, until that echo is deduplicated.

The cross-repo route has no `offset`; it is limit-only. To see more of a large
producer repository, narrow it with a `language` or `consumer_repo_ids`
selector instead of paging.

### Test-only consumers

A `live_by_consumer` row on the cross-repo route carries `test_only_consumers:
true` when it has consumers and every consumer's root entity is in a test file,
by the single test-path rule dead-code uses on every route
(`codemodel.DeadCodeIsTestFile`). The key is present only when true; it is
never `false`. It adds one fact and changes nothing else: a test that calls a
function is a real dependency, so the row stays `live_by_consumer`, its
`confidence_label`, evidence and the bucket counts are the same. What it tells
an admin is that removing the symbol also means removing or rewriting its tests.

The flag is absent on `dead` and `unknown_needs_evidence` rows, when any
consumer root is not a test file, when a root has no `content_entities` row,
when a consumer is hidden from the caller, when a consumer repository's
reachability snapshot is incomplete (`consumer_coverage.complete` is `false`),
when the request named `consumer_repo_ids`, and on a row that used the
repository-boundary fallback. A hidden consumer, an incomplete snapshot and a
named selector each mean the answer may not see every consumer, and "only tests
call this" needs all of them where liveness needs one. The boundary fallback is
absent because it has no entity-level consumer to inspect.

Language scope. A consumer snapshot is rooted at the entities whose parser
metadata carries `dead_code_root_kinds` (`listCodeReachabilityRootsSQL`), and
the cross-repo evidence read (`CrossRepoDeadCodeConsumerEvidence`) filters on
neither the root kind nor the root's file. Test methods carry a root kind only
for C# (`csharp.test_method`), Java (`java.junit_test_method`), Kotlin
(`kotlin.junit_test_method`), Scala (`scala.junit_test_method`,
`scala.scalatest_suite_class`), Rust (`rust.test_function`) and Swift
(`swift.xctest_method`, `swift.swift_testing_method`). For those languages a
test that calls a producer symbol yields a consumer row rooted at the test
method, so the symbol is `live_by_consumer` and, when every consumer root is a
test file, carries `test_only_consumers: true`. Go, Python, JavaScript and
TypeScript have no test root kind: a test that calls a symbol is not a root, so
it appears as no consumer at all, the symbol reads `dead` (or unknown when
consumer coverage is incomplete) and the flag never fires from that test. In
those languages the flag can appear only when some other root, such as a script
entry point, sits in a test path.

Cost: one batched primary-key read of `content_entities` per request (at most
1,000 consumer root ids, no `source_cache`), skipped when no candidate has a
consumer root, a named consumer selector or an incomplete consumer snapshot, and
27 bytes per flagged row on the wire. The same-repository
routes (`/api/v0/code/dead-code`, `/investigation`) do not carry the flag: a
candidate with a strong caller never becomes a result row there, so there is no
row to put it on.

## Fixture Contract

Dead-code exactness is language scoped. Parser fixtures prove syntax
extraction; dead-code fixtures prove cleanup safety. A language cannot claim
exact results until fixtures cover unused symbols, direct reachability edges,
entrypoints, public API surfaces, framework/callback roots, semantic dispatch,
test and generated-code exclusions, and at least one ambiguous dynamic case.

The fixture inventory lives at `tests/fixtures/deadcode/README.md`.

## Proof Gates

Promoting any language or scope above `derived` requires:

1. Parser or SCIP evidence for definitions, calls, references, imports, and root
   metadata.
2. Query tests for unused, reachable, excluded, and ambiguous results.
3. API, MCP, and CLI output for truth labels, classifications, maturity, and
   exactness blockers.
4. Backend conformance for NornicDB and Neo4j query shapes.
5. Performance evidence for bounded candidate scans on representative input.

Until those gates pass, Eshu can help find cleanup candidates, but humans and
agents must treat the result as derived evidence, not an authoritative delete
list.
