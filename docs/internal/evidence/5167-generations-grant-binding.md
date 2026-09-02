# Freshness generations: grant bound in the query

Root-Cause Evidence: `GET /api/v0/freshness/generations` accepts `scope_id`,
`repository`, `collector_kind`, `source_system`, `generation_id` and `status`.
A first attempt at #5167 F-6 guarded only the two selector fields in the
handler. Review (codex P1 on PR #6434) showed the guard was defective: a caller
leaves `scope_id` and `repository` empty, passes another tenant's
`generation_id`, and the guard returns true because both checked fields are
blank. `listGenerationLifecycleQuery` then filters on that generation alone and
returns its scope, queue counts and latest failure message.

To be exact about what that did and did not mean: no scoped caller reaches this
handler today. The route is listed in
`go/internal/query/auth_scoped_routes_pending_row_filtering.go`, so the auth
middleware answers a scoped token with 403 before dispatch. The defective guard
was therefore not a live cross-tenant read; it was the mechanism intended to
make the route safe to allowlist, and it would not have held. That is why the
check moved into the query rather than being repaired in the handler.
Reproduced as a regression test asserting the grant reaches the filter for a
`generation_id`-only query.

No-Regression Evidence: the change adds one predicate to an existing
`WHERE` clause on a query that already joins `ingestion_scopes` for its
`repository` selector, so it introduces no new join and no new table. For a
shared-key caller the predicate short-circuits on `$8::boolean = false` before
either array is examined, leaving the operator-facing plan unchanged; a test
asserts that caller is left unbounded rather than silently narrowed. For a
scoped caller the added work is two `= ANY(...)` comparisons against grant
arrays already carried in the request context. Package timings either side of
the change on the same host: `internal/query` 5.478s, `internal/status`
0.311s, `internal/storage/postgres` 9.023s, all passing, with `go vet` exit 0.

Observability Evidence: no new signal is added and none is removed. The route
keeps its existing handler span and contract-error path, and a refused read is
shape-identical to a missing one by design, so it is deliberately NOT
distinguishable in the response. An operator diagnosing "a scoped token sees
fewer generations than expected" reads the caller's grant, which is already
visible in the auth context the request carries.

## Why the predicate is on the row, not the selector

Two failures came from the same mistake in the first attempt.

Under-blocking: the guard compared only the fields a caller might leave empty,
so every other filter reached rows unbounded.

Over-blocking: Eshu authorizes a repository-kind scope when
`ingestion_scopes.source_key` matches an allowed repository, and the raw
`scope_id` normally differs from that key. A string comparison against
`AllowedScopeIDs` therefore denies a repository-granted token its own ingestion
scope. `admin_store_input_invalid_facts.go` records the same lesson from
PR #5252 / issue #4630.

Binding the grant to the row in SQL fixes both at once, and matches the shape
the dead-letter and invalid-facts readers already use.

## Follow-up: naming the grant bind positions

Root-Cause Evidence: review of PR #6434 noted that the grant predicate added
`$8`-`$10` to `listGenerationLifecycleQuery` while the parameter-order comment
above the constant still stopped at `$7`. Two tests treat those positions as
contract -- `TestListGenerationLifecyclePassesGrantToQuery` asserts each by
index, `TestFreshnessGrantPredicatesArePresentInTheShippedSQL` asserts the
predicate text -- so a renumbering would bind the grant to the wrong argument
with nothing in the file to contradict it. `resolveChangedSinceScopeQuery`
already documented `$3`-`$5`, so only the generations constant was missing them.

No-Regression Evidence: the change is eight Go doc-comment lines above the
constant. No SQL string is touched, so the query text on the wire, its plan
cache key, and every bind argument are byte-identical to the merged
`d11c88dac`. An in-SQL comment block was written first and reverted for exactly
this reason: those lines live inside the raw-string constant and would have been
shipped in the query text on every call for no runtime benefit.
`go test ./internal/storage/postgres -run 'PassesGrantToQuery|PresentInTheShippedSQL' -count=1`
passes (exit 0) before and after.

Observability Evidence: no signal is added or removed. A comment above a
constant emits nothing at runtime.
