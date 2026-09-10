# story

Relationship-story assembly for the code family (`codequery`), nested
under the `relationships` leaf it builds on.

## What lives here

- `data.go` — the story payload assembly (`Data`) and its shapers:
  direction counts, truncation flags, handles, scope mode, depth
  bounds, and the coverage markers.
- `resolution.go` — target resolution and the grant-bound candidate
  reads (`GrantedCandidates`, `SearchEntitiesForGrant`).
- `class.go` — the class-hierarchy and override builders and shapers.
- `graph.go` — the direct graph builders, the grant predicates, and
  the content-fallback row shaping.
- `nornicdb.go` — the NornicDB story builders, the anchor preflight,
  and the projection normalization.

## What stays in `codequery`

`codequery/story_handlers.go` holds the HTTP handlers, the target
resolution, and the grant plumbing. `codequery/story_reads.go` holds
the graph, class, and override readers. `codequery/story_nornicdb.go`
holds the pinned NornicDB readers and the hot-manifest cypher builders
with their query text intact. `codequery/story_forwarders.go` keeps
the pre-move spellings that forward onto this leaf.

## Invariants

- Eight story readers are queryplan-pinned: their bodies never change
  without a manifest update in the same change — re-freezing a digest
  to match an edit is not a fix. New logic goes in this leaf behind
  the existing pins.
- The hot-manifest cypher builders keep their query text: the
  `query_fragment` containment check binds the fragment to the source
  symbol, so only helpers move out.
- Grant predicates are fail-closed: an unattributable row is dropped,
  never widened.
- This package may import its parent `relationships` leaf but never
  `codequery` or root `query`.
