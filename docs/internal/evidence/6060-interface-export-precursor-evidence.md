# Interface Export Precursor — No-Regression Evidence (#6060)

The hot-path evidence gate flagged three files in this branch:

- `go/internal/query/repository_context_counts.go`
- `go/internal/query/repository_deployable_unit_relationships.go`
- `go/internal/query/repository_story_counts.go`

That gate is content-based as well as path-based: a file that *contains* Cypher
is flagged when it changes at all, including when the change never touches a
query. That is the case here, and this note records the proof rather than
asking a reader to take it on trust.

## What changed in those files

Only type names. `repositoryReadModelSummary` became `RepositoryReadModelSummary`
and `repositoryRelationshipReadModel` became `RepositoryRelationshipReadModel`,
because the interfaces returning them were exported. Every changed line in all
three files is a parameter type, a return type, or a composite literal type.

## No-Regression Evidence: the query text is byte-identical

Extracted every backtick string literal containing a Cypher or SQL keyword from
each file, at `origin/main` and at this branch's head, and compared them as
ordered lists:

| File | Query literals (base) | Query literals (head) | Identical |
| --- | --- | --- | --- |
| `repository_context_counts.go` | 4 | 4 | yes |
| `repository_deployable_unit_relationships.go` | 2 | 2 | yes |
| `repository_story_counts.go` | 3 | 3 | yes |

Sweeping the whole branch diff for added or removed lines carrying a query
keyword returns only Go `return` statements, matched because `RETURN` is a
Cypher keyword and `return` is a Go one. No query, no anchor, no predicate, no
`LIMIT`, and no index-relevant clause is added, removed, or reordered anywhere
in this branch.

Because the statements are unchanged, the planner sees the same shapes against
the same anchors with the same selectivity. There is no before/after latency
number to report here, and reporting one would imply a comparison this change
does not make: identical statements against an unchanged schema execute the
same work, and a measured delta would be sampling noise presented as a result.

Backend and schema state are untouched: no DDL, no index, no constraint, no
batch size, no worker count, no lease or queue setting appears in this diff.
The diff is confined to `go/internal/query`, which holds no schema.

## No-Observability-Change: the operator-facing surface is the same

No span, metric, log field, log message, status field, or runtime setting is
added, removed, or renamed. An operator diagnosing a slow repository-context
read at 3 AM sees exactly the signals they saw before, with the same names and
the same cardinality.

One response field does change value, and it changes toward being more
diagnosable rather than less. `code_symbol.go`'s symbol-search fallback used to
report `source_backend: "postgres_content_store"` — the same label the batched
path reports — while answering a materially different query
(`SearchEntitiesByName`, plain name matching, versus the match-mode-aware
symbol search). It now reports `postgres_content_store_name_fallback`. A caller
reading the truth envelope can now tell which query answered; previously it
could not. The field is `{"type": "string"}` in the schema those endpoints use,
with no enum, so the new value breaks no declared contract.

## Why this is safe

The change this branch exists to make is that fourteen interfaces in
`go/internal/query` had at least one unexported method. Go qualifies an
unexported interface method name by its declaring package, so an optional
capability assertion of the form `store, ok := content.(fooStore)` silently
returns `ok=false` once the interface and its implementer sit in different
packages. No compile error, no failing test — execution falls through to the
fallback. Five of those fallbacks are slower but correct, two return materially
different or missing data, and seven return an error.

Exporting the method sets closes that class before any package boundary moves
for the #6060 family splits. Twelve new tests assert the fast path is actually
taken, using call-counting fakes wired through the real production entry
points; two interfaces already had equivalent coverage. Each was proven to fail
before it passed, by forcing the assertion false and confirming the test goes
red for the documented reason.
