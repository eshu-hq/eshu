# Query contracts

## Purpose

`querycontract` holds the stable types and small helpers that query families
need without depending on the root `query` package.

## Ownership boundary

This package owns query profiles, truth and error envelopes, freshness causes,
HTTP response helpers, the shared capability registry, the graph/content read
ports, and the scoped-token repository-access authorization seam
(`RepositoryAccessFilter` and the SHAPE-A inline-map grant primitives in
`repository_authz.go` / `infra_scope_grant.go`). It does not own routes,
handler orchestration, whole graph queries, or Postgres implementations. Those
remain in the root query package or a family package.

It also owns several handler seams promoted from root so a handler-family
subpackage can call the same logic without an import cycle (#6060):

| Promoted seam | File here | What root keeps |
| --- | --- | --- |
| `#6408` projection-placeholder scrubber | moved to `entity/` (#6597) | `codequery` and `selector` call `entity.ClearResolvedEntityRepoProjectionPlaceholders` directly |
| Visualization packet, builder and merge | moved to `visualization/` (#6597) | callers name `visualization.VisualizationPacket`, `visualization.NewVisualizationBuilder` and friends directly |
| Entity-name search and resolution | moved to `entity/` (#6597) | callers name `entity.EntityNameSearch`, `entity.ResolveExactGraphEntityCandidates` and friends directly; root's #6060 aliases were deleted with the move |
| Content-index readiness | `content_index_readiness.go` | exported error alias, function forwarder |
| Evidence-citation packet read models (#6642) | moved to `evidence/` (#6597) | callers name `evidence.EvidenceCitationHandle` and friends directly; root's unexported #6060 aliases were deleted with the move |
| Language alias table and coverage maps | moved to `taxonomy/` (#6597) | callers name `taxonomy.CanonicalLanguage` and friends; `query/language` keeps unexported forwarders |
| `ContentStore` read models (#6060) | `documentation_read_models.go`, `repository_read_models.go`, `repository_summary_read_models.go` | 20 unexported type aliases in root, plus four exported ones |
| `AnswerMetadata` attach helper and the answer packet | moved to `answer/` (#6597) | callers name `answer.AttachAnswerMetadata`, `answer.NewAnswerPacket` and friends directly; root keeps the exported aliases in `answer_metadata_alias.go` and `answer_packet.go` that packages outside `go/internal/query` name; `AssertAnswerMetadata` pin in `impact/` test |
| Edge-materialization coverage | `edge_materialization_coverage.go` | `impact/` callers and the root coverage test reference directly |
| Evidence-boundary disclosures | moved to `evidence/` (#6597) | root's `evidence_boundaries.go` shim was deleted; callers name `evidence.AttachEvidenceBoundaries` directly |
| Hostname environment inference | `hostname_environment.go` | root service-evidence callers reference directly |
| Infra-label helpers | `infra_labels.go` | root infra-aggregate callers reference directly |
| Dead-code contract types | moved to `code/` (#6597) | callers name `code.DeadCodeCandidateLabels`, `code.DeadCodeIncomingEdge`, `code.CrossRepoDeadCodeConsumerReads` and friends directly; nothing in this package referenced them, so no forwarder stayed behind |
| K8s SELECTS matcher | moved to `kubernetes/` (#6597) | callers name `kubernetes.SelectMatch` and friends directly; the #6060 root wrappers were deleted with the move |
| Story-collection helpers | `story_collection_helpers.go` | root and `impact/` callers reference directly |
| Story-row helpers | `story_row_helpers.go` | root and `impact/` callers reference directly, including `CapMapRows` |
| Scoped workload grant decision | `workload_grant.go` | `entity` and `deployment` callers reference directly |
| Permission-denied envelope and gate | `permission_denied.go` | function forwarders `writePermissionDeniedEnvelope`/`requirePermissionFeature` |
| Unauthorized (401) response, OAuth-challenge types, correlation ID | `unauthorized.go` | exported type alias (`OAuthChallengePolicy`) and function forwarders (`unauthorizedResponse`, `requestWithOAuthChallenge`, `documentationCorrelationID`); `oauthWWWAuthenticateChallengeForRequest` keeps no root forwarder because its only caller moved with it |

Root's compatibility shape is not uniform, and the difference matters when
adding to this list. A sentinel error compared with `errors.Is` has to be the
same value re-exported, never a re-declared one, or the comparison silently
goes false and a caller takes its fallback path with nothing failing. A type
alias carries the type but not access to unexported fields, so a root caller
that reached into builder internals needs an accessor rather than an alias.

Two related symbols deliberately did not move. `hydrateResolvedEntityRepoIdentity`
stays in root because it carries a complete `MATCH`/`RETURN` statement, which
`AGENTS.md` keeps out of this leaf; only the pure scrubber it calls moved.
`supportedLanguages`, the accepted-language set, now lives in the language
leaf's `language/registry.go`, which is the file to edit when adding a
language.

The authorization seam emits Cypher *fragments* -- `WHERE` predicate text a
caller splices into its own query -- and that is the one carve-out to the
no-Cypher rule, recorded the same way in `AGENTS.md`. The bounds and the
predicate that enforces them stay together on purpose: hand a caller the grant
bounds without the predicate and it can forget to apply them. A complete query,
with its own `MATCH`/`RETURN` and result shape, still does not belong here.

`workload_grant.go`'s `WorkloadGrantAdmitted` is the one exception to "emits a
Cypher fragment": it is a pure Go decision over rows a caller already fetched
(a workload's `repo_id` plus its DEFINES-linked repository ids), not predicate
text. NornicDB (grant decisions for the `entity` and `deployment` Workload
lookups) is made in Go for this reason: a multi-line
`AND ( ... OR EXISTS {...} )` scoped WHERE group is unreliable on the pinned
v1.3.3 image -- it can drop the whole WHERE, including an unrelated id/name
anchor on the same MATCH -- so the grant moved out of the query text entirely
(#6786). The retired `ScopedWorkloadWhereClause` predicate is gone; the
single-line `IN`-disjunction `WorkloadScopePredicate` (`infra_scope_grant.go`,
SHAPE-A) is unaffected and still belongs here as a fragment emitter.

`WorkloadSelectorCandidateBound`, `ErrWorkloadSelectorCandidatesExceedBound`,
and `WriteWorkloadSelectorOverflow` live beside it so the `entity` and
`deployment` name lookups fail closed at the same bound with the same wire
answer: 409 Conflict and fixed text telling the caller to retry with a workload
id. The error text carries no row count. For a scoped caller the name read
carries `WorkloadScopePredicate`, so the bound counts granted rows only and
ungranted workloads can neither cause the 409 nor be inferred from it. The text
stays count-free for unscoped callers, and as defense in depth in case a
backend ignores the predicate.

## Exported surface

The exported surface is described in [doc.go](doc.go). Root `query` aliases the
types and wraps the functions so existing imports keep their current API.

## Dependencies

The package uses the Go standard library plus four internal packages. Two
are stdlib-only leaves: `internal/scope`, for the `scope.CollectorKind` the
`CollectorListReadinessStore` port carries, and `internal/environment`, which
`hostname_environment.go` reads. `internal/query/auth` supplies the
`AuthContext` that `RepositoryAccessFilterFromContext` reads and (#6642) the
`auth.AllowsPermissionFeature` predicate `RequirePermissionFeature`
calls; `auth` itself imports only the standard library and the
stdlib-only `internal/governanceaudit` (for `ActorClassForAuth`, #6642).
`internal/storage/cypher`, which `edge_materialization_coverage.go` reads,
is not a leaf: it brings `internal/graph`, `internal/projector`,
`internal/reducer` and `internal/telemetry` into this package's transitive
closure, an edge that predates #6642. None of these edges creates a cycle.
This is also why `unauthorizedResponse`/`writePermissionDeniedEnvelope`
moved here instead of into `auth` for #6642: `auth` cannot import
this package back (a cycle), so the response-writing side of the auth seam
lives on this side of the one-way edge. `GraphQuery` and `ContentStore` are
consumer-owned ports; concrete adapters remain outside this package.

A new import here is a contract change, not a detail. The point of this package
is that a family can depend on it for types without inheriting a runtime: the
handler span lives in `tracing` rather than here for exactly that reason.

## Telemetry

This package emits no metrics, spans, or logs. Handlers and storage adapters
retain their existing telemetry.

No-Observability-Change: moving these contracts does not change the handler or
adapter call paths that emit telemetry. The row-value decoders this package now
forwards to are pure functions with no instrumentation, in their old home and
in their new one.

## Performance

The row-value decoders no longer live here. #6597 moved them down into the
`querycontract/rowvalue` leaf, and this package kept `StringVal`, `BoolVal`,
`IntVal`, `StringSliceVal` and `FloatVal` as forwarders so its callers compile
unchanged. That put a *second* forwarding wrapper in front of five functions the
query read paths call constantly: a root call site now reads
`query.X -> querycontract.X -> rowvalue.X`, where before #6597 it stopped at
`querycontract.X`. `FloatVal` has no exported root wrapper; root reaches it
through two unexported ones, `floatVal` in `compare/handler.go` and
`relationshipFloatVal` in `repository_compat.go`, so its chain is the same three
hops deep.

Counted by walking the AST for call expressions, so a name appearing in a
comment does not inflate the figure, `StringVal` was called from 202 of the 880
non-test root files when the first four moved to this package, `IntVal` from 89,
`StringSliceVal` from 74, and `BoolVal` from 43. Those four counts are the
snapshot from when they moved and are deliberately not refreshed; the header
comment in `rowvalue/decode.go` -- a free-floating block after the imports, not
the package comment, which lives in `rowvalue/doc.go` -- carries the same
`StringVal` metric measured later (195 of 866), and the gap between the two is
families leaving root, which is what this epic is for. `FloatVal` is the small one: 11 call sites across 3
root files, 10 of them through `floatVal` and 1 through `relationshipFloatVal`.

The question that raises is whether the *extra* call frame costs anything on a
hot row-decode loop. For four of the five it does not. For `StringVal` the
answer is more specific than the old two-hop text claimed, and it is written out
below rather than smoothed over.

No-Regression Evidence: `cd go && go build -gcflags='-m'
./internal/query/querycontract/... ./internal/query/` at this head, re-run with
`-gcflags='-m=2'` for the costs. Every *forwarder* hop inlines, and each adds
exactly 5 to the inlined cost against the inliner's budget of 80 -- that holds
for all four hops of the collapsing helpers and for `StringVal`'s two forwarder
hops (62 -> 67). The leaf `StringVal` itself is the exception, the first row
below, and is explained after the table:

| helper | `rowvalue` leaf | `querycontract` forwarder | root forwarder |
| --- | --- | --- | --- |
| `StringVal` | **cost 95 — cannot inline** (`rowvalue/decode.go:64`) | cost 62 (`response_shaping_helpers.go:93`) | cost 67 (`neo4j.go:103`) |
| `BoolVal` | cost 33 (`rowvalue/decode.go:78`) | cost 38 (`response_shaping_helpers.go:98`) | cost 43 (`neo4j.go:108`) |
| `IntVal` | cost 40 (`rowvalue/decode.go:94`) | cost 45 (`response_shaping_helpers.go:104`) | cost 50 (`neo4j.go:113`) |
| `StringSliceVal` | cost 65 (`rowvalue/decode.go:115`) | cost 70 (`response_shaping_helpers.go:110`) | cost 75 (`neo4j.go:118`) |
| `FloatVal` | cost 46 (`rowvalue/decode.go:140`) | cost 51 (`response_shaping_helpers.go:116`) | cost 56 (`compare/handler.go`, `repository_compat.go:31`) |

For `BoolVal`, `IntVal`, `StringSliceVal` and `FloatVal` all three hops collapse.
The `-m` run reports `inlining call to rowvalue.BoolVal` at
`response_shaping_helpers.go:99:25` and the same for `IntVal` (`:105:24`),
`StringSliceVal` (`:111:32`) and `FloatVal` (`:117:26`) — the new hop; then
`inlining call to querycontract.BoolVal` at `neo4j.go:109:30`, `IntVal` at
`neo4j.go:114:29`, `StringSliceVal` at `neo4j.go:119:37` and `FloatVal` at
`compare/handler.go` and `repository_compat.go:32:31` — the old hop; then the
root wrapper itself at each caller (16 sites for `BoolVal`, 43 for `IntVal`, 42
for `StringSliceVal`, 10 for `floatVal` and 1 for `relationshipFloatVal`). A
decode site for those four emits the same code it did before the move.

Every call-site count in this section is bound to the head that measured it and
to the two packages that command builds. They fell when this branch rebased onto
`d3d4c2d3e`: `inlining call to StringVal` went from 391 sites to 282,
`querycontract.StringVal` from 323 to 214, `BoolVal` from 21 to 16, `IntVal`
from 52 to 43, `StringSliceVal` from 60 to 42, `floatVal` from 12 to 10 and
`relationshipFloatVal` from 2 to 1, and the whole `-m` run from 25702 lines to
20134. No caller was deleted: #6060 moved those families out of root into their
own subpackages, which the documented command does not build. Every cost in the
table above -- what the no-regression argument actually rests on -- is unchanged.

`StringVal` does not fully collapse, and it did not before this move either.
`rowvalue.StringVal` reports `cannot inline StringVal: function too complex:
cost 95 exceeds budget 80` — the `fmt.Sprintf` fallback that renders a present
non-string is what pushes it over. One real call frame therefore survives at
every `StringVal` decode site. That is not a regression, because the move
changed no code. Re-measured at this head, `git diff -M 514534567 HEAD --
go/internal/query/querycontract/rowvalue.go
go/internal/query/querycontract/rowvalue/decode.go` reports `similarity index
62%` over two hunks: the `package querycontract` -> `package rowvalue` clause,
and a rewrite of the file's header comment block. Both are comments and a
package clause -- **no statement or expression changed**, which is the property
this argument needs, because gc computes inline cost from the function body. The
identical body cost 95 and was equally uninlinable when it lived in this
package. (The 62% and the second hunk are lower than a bare move would give
because the fix commits rewrote that header comment; the filename differs from
the old text because #6597's own rename commit moved `rowvalue.go` to
`decode.go` for naming rule 2.) What #6597 added is the `querycontract.StringVal`
forwarder, and that one *does* inline (cost 62), as does the root wrapper at
`neo4j.go:103` (cost 67): `inlining call to StringVal` fires at 282 sites and
`inlining call to querycontract.StringVal` at 214. Before and after, a caller
emits exactly one call to the decoder and no wrapper frames.

The number to watch is `StringSliceVal`'s root wrapper at cost 75. Five points
of headroom is one more forwarder hop; a third wrapper in that chain would stop
inlining and add a real frame. The other root wrappers have 13 to 37 points of
room, `StringVal`'s at `neo4j.go:103` being the next tightest.

No benchmark is cited because there is no runtime delta to measure: the two
forwarder hops that #6597 and #6060 added disappear at compile time, and the
one frame that survives survived before them.

No-Observability-Change: the five decoders emit no metric, span, or log, in this
package's forwarders or in the `rowvalue` leaf they call -- they are pure
functions over a map. The handlers that call them keep their existing
`eshu_dp_api_request_duration_seconds` timing and their `query.*` spans, and
because every wrapper hop inlines away, no span boundary, attribute, or log line
moves. An operator sees exactly the signals they saw before.

## Collector-list readiness

`CollectorListReadinessStore` is a consumer-owned port, the same category as
`GraphQuery` and `ContentStore`. With the state enum, counts, envelope, and the
two `Build...` functions it answers one question a gated supply-chain list
cannot answer on its own: whether a zero-row page means "nothing matched" or
"the feeding collector is switched off".

What is deliberately NOT here is the attach step. Deciding whether to run the
probe, running it against a live store, and writing the result into a response
body is request-time orchestration, and it stays in package `query`. Two
reviewers independently flagged an earlier version of this move for putting that
behaviour in the dependency-neutral leaf, and they were right: a family package
that wants the envelope calls `BuildCollectorListReadiness` and owns its own
attach.

The one behavioural rule worth restating, because it is easy to invert: a
non-empty page is classified `ready_with_results` without consulting the probe
at all. Returned rows are themselves proof the collector ran, so a stale or
failing probe must never downgrade a page that already carries evidence.

## Performance and observability of the authz seam

The scoped-token access filter and its predicate builders moved here, and every
repository-shaped read path calls them, so the question is whether the move cost
anything on those paths.

No-Regression Evidence: it did not, because the emitted query is unchanged.
String literals extracted per function with `go/parser` before and after the move
are byte-identical across all 13 manifest-pinned symbols (356 literals), the 25
coverage-tracked symbols, and the 6 filter methods that RETURN Cypher fragments
compared across the package boundary. Only Go identifiers changed
(`access.graphCondition` became `access.GraphCondition`), which is why 47
source-text digests moved while no query did. A second method agrees: sweeping
the diff for changed lines carrying Cypher keywords returns only method-name
capitalisations, with the surrounding `MATCH`/`WHERE`/`LIMIT` text untouched.
`go build ./...`, `go vet ./...` and
`go test ./internal/query/... ./internal/mcp ./internal/queryplan -count=1` all
exit 0.

No-Observability-Change: the filter emits no metric, span, or log. The graph
reads it bounds travel through the shared bounded read policy and carry that
policy's `neo4j.query` span exactly as before; failures render through the
shared error contract. Moving where the predicate is built changed no operator
signal.

## Gotchas / invariants

Capability registration is ordered and rejects duplicate initialization in the
contract tests. The low-level compatibility setter remains last-write-wins for
existing root-package tests. Unknown capabilities still panic when building a
truth envelope, and an unknown required profile still defaults to
`local_full_stack`. Once root declares the canonical capability order, an
incomplete, duplicated, or unknown entry fails closed instead of returning a
partial inventory.

`K8sSelectCandidate` carries selector presence separately from selector value.
Family code must preserve absent, present-empty, and present-nonempty states
when converting it into matcher input.

No-Regression Evidence: `go test ./internal/query/... -count=1` passed after the
final boundary edit. During the scratch proof, the complete query-playbook
family and its four test files moved under `internal/query/playbook`; its route,
catalog-order, resolver, recursive root-query tests passed, followed by
`go build ./...` and `go vet ./...`, both at exit 0. The family move was then
reverted, leaving only this contract boundary. Mutation runs proved the unknown-
capability panic test fails if the root truth wrapper stops delegating and the
selector tri-state test fails if candidate presence is dropped.

## Verification

From `go/`, run `go test ./internal/query/... -count=1`, `go build ./...`, and
`go vet ./...`. From the repository root, run
`scripts/verify-package-docs.sh` and the B-7 golden-corpus proof selected by
the parent package instructions.

## Related docs

- [Source layout](../../../../docs/public/reference/source-layout.md)
- [HTTP API](../../../../docs/public/reference/http-api.md)
- [Package restructure design](../../../../docs/internal/design/package-restructure.md)

## Performance and observability

No-Regression Evidence: the hot file this change touches is
`go/internal/query/repository/catalog.go` (moved from `go/internal/query/catalog.go`
for #6060 lane-B B3), which does issue a real Cypher `MATCH`. The query
text, its parameters and its decode loop are byte-identical; the only edit turns
`CatalogWorkloadIdentityEntry` from a struct declaration into a type ALIAS onto
this package, so `querytestutil/content`'s `FakePortContentStore` can name it from outside
root. An alias preserves type identity, so no conversion, copy or extra
allocation appears on the row-decoding path. The same shape applies to the other
read models promoted here. Root suite on this branch: 8324 `=== RUN`, 0
`--- FAIL`, against `origin/main` 460c59481.

No-Observability-Change: no metric, span, log or status surface is added,
removed or renamed. Moving a type declaration between packages emits nothing,
and the aliases keep every existing call site on the same code path.
