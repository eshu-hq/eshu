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
| `builtfrom.go` | `BuiltFromCypher`, `LookupBuiltFromRepositories`, the enforced key/fan-out bounds, `GrantCounts`, and the per-row grant decision. |
| `cursor.go` | The opaque continuation token: `Cursor`, `EncodeCursor`, `DecodeCursor`. |

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

**The opaque cursor.** `next_cursor` is a token bound to the `image_ref` and
`limit` it was issued for, not a raw offset. Without it, the advance between
cursors told the caller the same thing the refill just stopped `count` from
telling them.

Both bounds on `BuiltFromCypher` are enforced in code, not merely registered in
`go/internal/queryplan/testdata/query-source-coverage.yaml`: the statement
returns one row per BUILT_FROM edge, so a multi-source image returns more rows
than keys. Overflow fails the read closed. Adding a Cypher `LIMIT` instead would
drop edges the caller is entitled to and could turn a granted row into a
withheld one.

## Dependencies

Standard library plus `querycontract` (`GraphQuery`, `RepositoryAccessFilter`,
`StringVal`/`BoolVal`). Never the query root.
