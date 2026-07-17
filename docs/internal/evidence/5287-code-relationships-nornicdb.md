# #5287 (part 1) — code-relationships entity-label identity: before/after

First tractable slice of the #5287 multi-clause sweep: the NornicDB
entity-label identity resolver used a top-level `UNION`, which the pinned build
mis-executes, rewritten to `CALL{…UNION…}` with a plain outer `RETURN`.

Scope note: an earlier draft also touched `buildTransitiveRelationshipRowsCypher`'s
NornicDB branch, but review proved that branch is **dead** on the runtime path —
NornicDB transitive relationships resolve through a Go-side BFS
(`nornicDBTransitiveRelationshipRows`), and `buildTransitiveRelationshipRowsCypher`
is only reached for non-NornicDB backends. That change was dropped; only the
live identity-resolver fix ships here. The harder shortestPath /
repo-traversal-filter reads (call-chain, route-to-caller, exposure-path) are
carved out on #5287 for dedicated design.

Backend: NornicDB `eshu-nornicdb-pr261:149245885258`
(commit `1492458852588c884c32f70d27ea2ee07086769c`), isolated Compose project
`eshu-5287`. Measured directly over Bolt-HTTP on a seeded `Function`/`CALLS`
call graph. Same-machine relative before/after (small representative corpus).

## Accuracy Evidence (behavior fix — corrected delta)

`nornicDBRelationshipEntityLabelCypher` resolves an entity's primary label by
id/uid, on the LIVE path (`nornicDBRelationshipMetadataRow` →
`nornicDBRelationshipEntityLabel`).

| read | before | after |
|---|---|---|
| entity-label identity | per-label arms joined by a **top-level `UNION`** return column-mangled rows — the arms' columns collapse into one row (e.g. a column value of `"labels\nUNION\nMATCH (e:Class…"` and 5-value rows), so label resolution is wrong | `CALL{…UNION…}` + plain outer `RETURN uid, id, labels` returns one clean row per matched entity (`["mid", <uuid>, ["Function"]]`); the caller's `len(rows)==1` vs `>1` disambiguation is preserved |

Each `UNION` arm keeps its single-label inline-property anchor (the NornicDB-safe
shape; a bare label-disjunction MATCH matches zero rows on this build). The
repo-scoped variant parses and executes inside `CALL{}`. This shape is also
correct on Neo4j; the rewrite is portable, not a backend-only workaround.

## Performance Evidence

Performance Evidence: the identity resolver's per-label `UNION` arms are
label+inline-property bounded with `LIMIT 2`; the `CALL{}` wrapper adds no
measurable cost (same bounded arms, one extra planning frame). No-Regression
Evidence: no latency or throughput regression — the query does the same bounded
per-label lookups, now returning correct (un-mangled) rows.

## Observability Evidence

No-Observability-Change: no new metric, span, or log field. The identity read
keeps its existing `GraphQuery.Run` span; the query shape changes but the
surfaced telemetry does not.

## Verification

- Live Bolt-HTTP before/after on the seeded `eshu-5287` stack (top-level UNION
  mangled vs `CALL{UNION}` clean, 1..24 arms, incl. the repo-scoped variant).
- `go test ./internal/query -run 'TestNornicDBRelationshipEntityLabel'` — a
  query-shape guard that fails on the old top-level-UNION shape.
