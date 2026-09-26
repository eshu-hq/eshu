# relationships

Entity relationship reads for the code family (`codequery`).

## What lives here

- `nornicdb.go` — the NornicDB row readers (`GraphRow`,
  `OneHopRelationships`, `MetadataRow`, `TransitiveRows`), the
  dialect pattern builders, the row limits, and placeholder
  normalization.
- `enrich.go` — file/repository enrichment of the core rows
  (`EnrichRows`).
- `identity.go` — entity-label resolution, the metadata builders,
  and the inheritance grant filter the story family builds on.
- `filters.go` — response shaping, direction/type filtering, and
  the contract capabilities.

## What stays in `codequery`

`codequery/relationship_handlers.go` holds the HTTP handler, the
content-fallback path, the repo-identity hydration, and the two pinned
graph-row readers. `codequery/relationship_forwarders.go` keeps the
pre-move spellings that forward onto this leaf.
`codequery/transitive_walk.go` holds the pinned one-hop read
and the transitive walk that injects it.
`codequery/entity_labels.go` holds the pinned label
read and forwarders. The name-target resolver already lives in
`codemodel`.

## Invariants

- Four graph-read methods are grandfathered: their bodies never
  change without a re-validation the ledger forbids — re-freezing is
  not a fix. New logic goes in this leaf behind the existing pins.
  #5167 is the sanctioned exception on record: binding the grant
  changed what the pinned readers query, so their digests were
  re-derived from the gate's own mismatch report in the same change.
- The row ceiling (`RowLimit`) and its truncation flags are
  exact-truth load-bearing: never present a clipped set without the
  flags.
  `FilterResponse` is where the handler's `outgoing_truncated` and
  `incoming_truncated` are normalized to always-present booleans (#7151):
  false for a direction the caller filtered out and for sources with no
  row ceiling (Neo4j collect, transitive walk).
- Cypher shape changes need backend-differential proof (NornicDB vs
  Neo4j row-set equivalence), not just unit tests.
- This package never imports `codequery` or root `query`.
- Repository grant (#5167): every read binds a scoped caller's grant
  in its own statement. `MetadataPredicate` binds the anchor's
  Repository; `OneHopRelationshipsCypher` and the two far-endpoint
  enrichment reads bind the neighbour's `repo_id` through
  `NeighbourGrantWhere`, in the anchoring MATCH's WHERE and ahead of
  ORDER BY/LIMIT. Never move a grant into Go after the read: on a hub
  the row ceiling would be spent on ungranted neighbours and the
  granted ones dropped (measured, see
  `docs/internal/evidence/5167-code-relationships-grant.md`). An
  unscoped caller renders no grant text, so its statements are
  byte-identical to the pre-#5167 ones.

## Truncation flags evidence (#7151)

No-Regression Evidence: the #7151 truncation flags change no Cypher statement
and move no query-plan digest. The NornicDB one-hop leaf already read limit+1;
the handler now forwards the flags it computed. Each capped content lookup
fetches one extra row (21 instead of 20) to detect its ceiling. The flags are
pinned by `go test ./internal/query ./internal/query/codequery -run
'Test(HandleRelationshipsNornicDBReturnsTruncationFlags|HandleRelationshipsContentFallbackReturnsTruncationFlags|HandleRelationshipsContentLookupsReportCeilingClip|ContentRelationshipSetLookupClipKeepsScanTelemetryFlag)'
-count=1`.

No-Observability-Change: the flags are response fields only. They add no graph
write, queue, worker, metric instrument, metric label, or span, and the entity
route's existing k8s scan telemetry (`ScanTruncated`) keeps its meaning.
