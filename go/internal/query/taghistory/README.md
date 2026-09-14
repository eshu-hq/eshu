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
| `page.go` | The four statements — `FirstPageCypher`, `AfterKeyCypher`, `NullTailCypher` and the unscoped `OffsetCypher` — plus `Key`, `Row`, `WindowRow`, `ReadWindow` and `ReadOffsetWindow`. |
| `refill.go` | `RefillScopedPage`, `ScopedPage` and `MaxRefillReads` — the refill loop, its fixed window size and its read cap. |
| `builtfrom.go` | `BuiltFromCypher` (RETURNs `DISTINCT`), `LookupBuiltFromRepositories`, the enforced key/fan-out bounds, `GrantCounts`, and the per-row grant decision. |
| `cursor.go` | The keyset continuation token: `Cursor`, `EncodeCursor`, `DecodeCursor`, and `Audience`/`AudienceOf`. SEALED with the deployment DEK under an AAD binding the route, the version, the `image_ref` and the caller's grant set, and it carries no row position. |

## Why the join runs in Go

`ContainerImageTagObservation` carries no source-repository key, so a scoped
caller is bound through the reducer's `ContainerImage-[:BUILT_FROM]->Repository`
edge. The obvious single statement — matching the observation and the
BUILT_FROM edge in one query — returned **zero rows** on the pinned NornicDB
build and on upstream v1.3.1 for a seed whose correct answer was two rows, while
the two single-clause reads returned exactly the seeded edges on both. So each
window runs one keyset statement and then `BuiltFromCypher`, and the join
happens in Go.

Do not collapse them into a multi-clause read. See
`docs/internal/evidence/6564-tag-history-grant-binding.md`.

## Two things that are load-bearing, not cosmetic

**The refill, and its FIXED window size.** A grant-filtered page reads
successive windows until it holds `limit` VISIBLE rows, the history ends, or
`MaxRefillReads` windows have been read. Without the refill, `limit - count`
told a scoped caller exactly how many rows of another tenant's history the
filter had withheld from that window. The window is `MaxLimit` rows regardless
of the caller's `limit`, and that is the second half of the same fix: with
`limit`-sized windows the span one request scanned was `4*limit`, so at
`limit=1` a `count: 0` page said "these four consecutive observations are
someone else's". Fixed windows make it a constant 800 raw rows. Do not
reintroduce a `limit`-sized window as an optimisation.

**The cursor token, and its seal.** `next_cursor` is a KEYSET token naming one
row's `(first_observed_at, uid)`, not a row position, and it is SEALED with the
deployment DEK (AES-256-GCM under a route-specific AAD). Both halves are
load-bearing and each closed a separate P1. The offset it replaced was a
position in the pre-filter history, so decoding a token read the frontier the
filter had advanced past. The unsealed key that replaced it was worse than it
looked: a caller could mint one naming any start, and a fully-withheld capped
page answers with the key 800 raw rows later, so the answer was a step function
a caller could binary-search to recover every withheld row's key. Sealing makes
the reachable start set `{page one} ∪ {tokens this server issued}`. Do not
"simplify" the seal away, and do not describe the token as opaque by convention
— it is opaque by construction now. Where no DEK is configured, grant-filtered
paging fails closed (`ErrCursorSealingUnavailable`) rather than downgrading.
The residue left is counts only, on `Cursor`'s doc comment and on every
caller-facing surface.

**Why the AAD carries an audience.** `{tokens this server issued}` was still too
wide a start set, because it said nothing about who a token was issued TO. A
sealed cursor's plaintext carries no scope, grant or principal, so once the
envelope opens nothing downstream can tell it came from somewhere else — only
the AEAD binding can refuse it. That mattered most for unscoped callers, who
keep the `offset` parameter and can therefore mint a token at an arbitrary raw
position; handing one to a scoped caller put it at a start it never paged to.
`AudienceOf` folds the caller's grant set into the AAD, narrowing the set to
`{page one} ∪ {tokens issued against these same grants}` — and any caller
holding those grants sees the same rows, so nothing in that set discloses
anything.

Operationally this behaves like the key rotation above: a grant change between
two pages ends the walk with a 400 and the client restarts from page one. That
is the intended answer rather than a rough edge, since the filter's output
changed mid-walk and splicing two visibilities into one result would be worse.
It is bound to the grant SET and deliberately not to the credential or the
principal, so a token rotation for an unchanged grant set keeps paging and a
cursor the API issued still opens on the standalone MCP server holding the same
DEK.

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
