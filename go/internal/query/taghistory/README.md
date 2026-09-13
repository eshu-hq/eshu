# taghistory

Bounded graph reads and grant-binding page assembly behind
`GET /api/v0/images/tag-history` (MCP `list_container_image_tag_history`).

The query root keeps the HTTP handler — capability gating, selector parsing,
the response envelope, telemetry. This package keeps everything that touches
the graph or decides what a scoped caller may see, so the two statements, the
bounds derived from them, and the join that enforces the grant live together.

## What is here

| File | Owns |
| --- | --- |
| `page.go` | `Cypher` (the image_ref-anchored observation read), `Row`, `ReadWindow`, and `RefillScopedPage` — the refill loop and its read cap. |
| `builtfrom.go` | `BuiltFromCypher` (RETURNs `DISTINCT`), `LookupBuiltFromRepositories`, the enforced key/fan-out bounds, `GrantCounts`, and the per-row grant decision. |
| `cursor.go` | The continuation token: `Cursor`, `EncodeCursor`, `DecodeCursor`. Reversible and unauthenticated. |

## Why the join runs in Go

`ContainerImageTagObservation` carries no source-repository key, so a scoped
caller is bound through the reducer's `ContainerImage-[:BUILT_FROM]->Repository`
edge. The obvious single statement — matching the observation and the
BUILT_FROM edge in one query — returned **zero rows** on the pinned NornicDB
build and on upstream v1.3.1 for a seed whose correct answer was two rows, while
the two single-clause reads returned exactly the seeded edges on both. So each
window runs `Cypher` and then `BuiltFromCypher`, and the join happens in Go.

Do not collapse them into a multi-clause read. See
`docs/internal/evidence/6564-tag-history-grant-binding.md`.

## Two things that are load-bearing, not cosmetic

**The refill.** A grant-filtered page reads successive windows until it holds
`limit` VISIBLE rows, the history ends, or `MaxRefillReads` windows have been
read. Without it, `limit - count` told a scoped caller exactly how many rows of
another tenant's history the filter had withheld from that window.

**The cursor token.** `next_cursor` is a token bound to the `image_ref` and
`limit` it was issued for, not a raw offset. Without it, the advance between
cursors told the caller the same thing the refill just stopped `count` from
telling them. It is NOT sealed: the encoding is reversible and carries no MAC,
so a caller that decodes it recovers the raw frontier. That is an OPEN defect
with a replacement being designed, not an accepted limitation — never describe
the token as tamper-rejecting, and do not build further on its raw-offset
payload.

**The `DISTINCT` in `BuiltFromCypher`.** BUILT_FROM edge identity is
`{scope_id, evidence_source}`, so one image↔repository pair carries one edge per
scope and evidence source. Without `DISTINCT` the statement returned one row per
EDGE and a full page in a two-source, two-scope deployment blew past
`BuiltFromMaxRows` — a 500 for a scoped caller entitled to every row on it. The
only consumer needs set membership, so collapsing parallel edges drops nothing.

Both bounds on `BuiltFromCypher` are enforced in code, not merely registered in
`go/internal/queryplan/testdata/query-source-coverage.yaml`. Overflow fails the
read closed, and its error carries no counts to the caller: both counts describe
the raw pre-filter window across every tenant. Adding a Cypher `LIMIT` instead
would drop edges the caller is entitled to and could turn a granted row into a
withheld one.

## Dependencies

Standard library plus `querycontract` (`GraphQuery`, `RepositoryAccessFilter`,
`StringVal`/`BoolVal`). Never the query root.
