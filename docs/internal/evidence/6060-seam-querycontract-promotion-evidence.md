# Seam Promotion To querycontract — No-Regression Evidence (#6060)

The hot-path evidence gate flagged two files:

- `go/internal/query/querycontract/entity_repo_identity.go`
- `go/internal/query/repository_coverage.go`

That gate is content-based as well as path-based: a file containing Cypher is
flagged whenever it changes, including when the change relocates it without
touching a statement. That is the case here, and this records the proof rather
than asking a reader to take it.

## What changed

`hydrateResolvedEntityRepoIdentity` moved from root into `querycontract` and was
exported. Root keeps a forwarder. The language taxonomy (`LanguageAliases`,
`CanonicalLanguage`, `NormalizedLanguageVariants`, `CoverageLanguageMaps`) moved
the same way, which is what touches `repository_coverage.go` — its call sites
now name the leaf package.

The move exists because a handler-family subpackage cannot call an unexported
root symbol, and cannot import root at all without a cycle through root's own
compatibility aliases.

## No-Regression Evidence: the query text is byte-identical

Every backtick literal containing a Cypher or SQL keyword was collected across
the whole `go/internal/query` tree at `origin/main` and at this branch's head,
comments stripped, indentation normalised, and compared as multisets. The check
is tree-wide rather than per-file on purpose: a per-file diff cannot show that a
statement is unchanged once its file has moved.

```
base origin/main: 496 query literals (462 distinct)
head:             496 query literals (462 distinct)

IDENTICAL — every query literal preserved, none added
exit 0
```

Run twice, independently, by two parties. No statement is added, removed, or
reordered; no anchor, predicate, `LIMIT`, or projection changes.

Because the statements are unchanged, the planner sees the same shapes against
the same anchors with the same selectivity. No latency figures are quoted, and
quoting them would imply a comparison this change does not make — identical
statements against an unchanged schema do identical work, so a measured delta
would be sampling noise presented as a result.

No DDL, index, constraint, batch size, worker count, lease or queue setting
appears in the diff.

## No-Observability-Change: the operator-facing surface is unchanged

No span, metric, log field, status field, or runtime setting is added, removed,
or renamed. An operator diagnosing a slow entity-context read sees the same
signals under the same names with the same cardinality.

## Why this is safe

`hydrateResolvedEntityRepoIdentity` carries the `#6408` projection-placeholder
scrubber, which is why the promotion matters beyond tidiness. `#6408` is a live
bug: a second-hop node property reached through `OPTIONAL MATCH` returns the
literal text of its own projection expression, so `repo_id` comes back as the
string `"r.id"`. The scrubber string-matches the backend's output against four
expression shapes reconstructed by hand in Go.

An earlier pass copied that scrubber into the family package alongside a dozen
small helpers. Promoting it instead leaves exactly one implementation, so the
eventual fix for #6408 has one place to land. Two copies would have guaranteed
the fix reached one of them while the other kept matching the old four shapes
and kept passing a fabricated repository ID through, with nothing failing.

The queryplan source-coverage manifest entry moves with the symbol. It is a
typed `non_hot` entry (`class: keyed_support`), not a re-freeze of a
`grandfatheredNonHotSourceDigests` row — the practice `queryplan/README.md`
forbids and #6409 tracks an instance of. Its digest changes because the symbol
relocated and was exported; the parity proof above is what says the
`keyed_support` / `max_keys: 101` classification still describes the same
statement.
