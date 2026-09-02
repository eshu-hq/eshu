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
| `#6408` projection-placeholder scrubber | `entity_repo_identity.go` | unexported function forwarder |
| Visualization packet, builder and merge | `visualization_packet.go`, `visualization_packet_merge.go` | exported type and const aliases, function forwarders |
| Entity-name search | `entity_name_search.go` | exported type aliases, unexported const and sentinel-error aliases |
| Content-index readiness | `content_index_readiness.go` | exported error alias, function forwarder |
| Evidence-citation handles | `evidence_citation_handle.go` | unexported type aliases in `evidence_citation.go`, plus the exported `EvidenceCitationHandle` alias root already published in `evidence_citation_public.go` |
| Language alias table and coverage maps | `language_registry.go` | unexported function forwarders |

Root's compatibility shape is not uniform, and the difference matters when
adding to this list. A sentinel error compared with `errors.Is` has to be the
same value re-exported, never a re-declared one, or the comparison silently
goes false and a caller takes its fallback path with nothing failing. A type
alias carries the type but not access to unexported fields, so a root caller
that reached into builder internals needs an accessor rather than an alias.

Two related symbols deliberately did not move. `hydrateResolvedEntityRepoIdentity`
stays in root because it carries a complete `MATCH`/`RETURN` statement, which
`AGENTS.md` keeps out of this leaf; only the pure scrubber it calls moved.
`supportedLanguages`, the accepted-language set, stays in root's
`language_registry.go` and is still the file to edit when adding a language.

The authorization seam emits Cypher *fragments* -- `WHERE` predicate text a
caller splices into its own query -- and that is the one carve-out to the
no-Cypher rule, recorded the same way in `AGENTS.md`. The bounds and the
predicate that enforces them stay together on purpose: hand a caller the grant
bounds without the predicate and it can forget to apply them. A complete query,
with its own `MATCH`/`RETURN` and result shape, still does not belong here.

## Exported surface

The exported surface is described in [doc.go](doc.go). Root `query` aliases the
types and wraps the functions so existing imports keep their current API.

## Dependencies

The package uses the Go standard library plus two standard-library-only leaves:
`internal/scope`, for the `scope.CollectorKind` the `CollectorListReadinessStore`
port carries, and `internal/query/queryauth`, for the `AuthContext` that
`RepositoryAccessFilterFromContext` reads. Neither brings a transitive
dependency and neither creates a cycle. `GraphQuery` and `ContentStore` are consumer-owned
ports; concrete adapters remain outside this leaf package.

A new import here is a contract change, not a detail. The point of this package
is that a family can depend on it for types without inheriting a runtime: the
handler span lives in `queryspan` rather than here for exactly that reason.

## Telemetry

This package emits no metrics, spans, or logs. Handlers and storage adapters
retain their existing telemetry.

No-Observability-Change: moving these contracts does not change the handler or
adapter call paths that emit telemetry. The graph row-value decoders are pure
functions with no instrumentation, before the move and after it.

## Performance

Moving the row-value decoders here put a forwarding wrapper in front of five
functions the query read paths call constantly. Counted by walking the AST for
call expressions, so a name appearing in a comment does not inflate the figure,
`StringVal` was called from 202 of the 880 non-test root files when the first
four moved, `IntVal` from 89, `StringSliceVal` from 74, and `BoolVal` from 43.
`FloatVal` arrived later and is the small one: 11 call sites across 8 root
files, every one of them reaching it through the unexported
`relationshipFloatVal` rather than an exported wrapper. Those first four counts
are the snapshot from when they moved and are deliberately not refreshed; the
comment in `rowvalue.go` carries the same StringVal metric measured later
(195 of 866), and the gap between the two is families leaving root, which is
what this epic is for. The question that
raises is whether the extra call frame costs anything on a hot row-decode loop.

It does not: the compiler removes it entirely.

No-Regression Evidence: `cd go && go build -gcflags='-m' ./internal/query/`
reports `can inline StringVal`, `can inline BoolVal`, `can inline IntVal` and
`can inline StringSliceVal` for the four root wrappers; `inlining call to
querycontract.BoolVal`, `... IntVal` and `... StringSliceVal` where each wrapper
calls into this package; and `inlining call to StringVal` at each caller
(`neo4j.go:307,309,311,313,317` among others). Both hops collapse at compile
time, so a decode site emits the same code it did before the move. No benchmark
is cited because there is no runtime delta to measure -- the indirection does not
survive compilation.

The same run covers `FloatVal`: `can inline relationshipFloatVal`, then both
`inlining call to relationshipFloatVal` and `inlining call to
querycontract.FloatVal` at all 11 call sites (`service_story_limits.go:60`
shows each hop twice, once per call on that line). The two-hop shape is
identical to the other four, and it collapses the same way.

No-Observability-Change: the five decoders emit no metric, span, or log, before
this move and after it -- they are pure functions over a map. The handlers that
call them keep their existing `eshu_dp_api_request_duration_seconds` timing and
their `query.*` spans, and because both wrapper hops inline away, no span
boundary, attribute, or log line moves. An operator sees exactly the signals
they saw before.

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
