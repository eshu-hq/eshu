<!-- SPDX-License-Identifier: MIT -->
<!-- Copyright (c) 2025-2026 eshu-hq -->

# C4 no-port census record: 6 static W8b routes (Refs eshu-hq/eshu-api#101)

Lane branch `drive/9669-c4-noport-record`, base `4788aaada`. Scope: record
only — no deletion, no deletion PR, no traffic shift, no behavior change. This
file lands the no-port census for the 6 W8b routes named in
eshu-hq/eshu-api#101: `GET collector-extraction-readiness` x2,
`GET fact-schema-versions` x2, `GET surface-inventory`, and
`POST visualizations/derive`.

Disposition for all 6: **no-port** — each handler is Profile-only (or empty
struct) construction in `go/cmd/api/wiring_router.go` and its body reads only
an exact static source. No graph, content, or store read on any of the 6
paths. No interface mint: the record cites existing handlers, existing static
sources, and existing freeze tests only.

Wiring proof (all Profile-only / empty struct):

- `go/cmd/api/wiring_router.go:309` —
  `Visualization: &query.VisualizationHandler{}` (empty struct, no fields at
  all).
- `go/cmd/api/wiring_router.go:327` —
  `ExtractionReadiness: &query.CollectorExtractionReadinessHandler{Profile: queryProfile}`.
- `go/cmd/api/wiring_router.go:328` —
  `FactSchemaVersions: &query.FactSchemaVersionHandler{Profile: queryProfile}`.
- `go/cmd/api/wiring_router.go:336` —
  `SurfaceInventory: &query.SurfaceInventoryHandler{Profile: queryProfile}`.

## ExtractionReadiness — GET x2

- Routes: `GET /api/v0/collector-extraction-readiness`,
  `GET /api/v0/collector-extraction-readiness/{family}`.
- Handler: `go/internal/query/collector_extraction_readiness.go`
  (`CollectorExtractionReadinessHandler` struct declaration, Profile-only);
  `Mount` at `:54-57`, list body at `:59-80`, family body at `:82-117`.
- Exact static source: `extraction.Catalog()` at handler `:64`
  (`go/internal/extraction/catalog.go` `Catalog()` definition, iterates the package-level
  `catalog` at `:76` and evaluates each profile) and `extraction.Lookup` at
  handler `:97` (`catalog.go:115`, linear scan of the same static slice).
- No graph/content/store read: the struct holds only `Profile QueryProfile`;
  imports are `fmt`, `net/http`, `strconv`, `strings`,
  `go/internal/extraction`, `go/internal/scope`. The file's own doc comment
  (`:25-28`) states the data is static policy classification and the handler
  reads no runtime, graph, or registry state. `rg` over the file returns no
  code reference to any graph, content, store, reader, or ReadStore symbol.
- Substitute freeze: OpenAPI path contract
  `go/internal/query/openapi/paths/status/collector_extraction_readiness.go`
  + core handler tests
  `go/internal/query/collector_extraction_readiness_test.go`
  (`TestCollectorExtractionReadinessListReturnsCatalog`,
  `TestCollectorExtractionReadinessListHonorsLimit`,
  `TestCollectorExtractionReadinessListRejectsBadLimit`,
  `TestCollectorExtractionReadinessFamilyDrilldown`,
  `TestCollectorExtractionReadinessFamily_LocalLightweightReturnsFamilyData`,
  `TestCollectorExtractionReadinessFamilyNotFound`) + satellite
  check-parity byte-fragment expectation (satellite pins byte fragments of the
  served list/family envelopes; in-tree half is the MCP resolve-route pin
  `go/internal/mcp/tools_collector_extraction_readiness_test.go:
  TestResolveRouteMapsListCollectorExtractionReadiness /
  TestResolveRouteMapsGetCollectorExtractionReadiness` and
  `go/internal/mcp/collector_extraction_readiness_test.go:
  TestCollectorExtractionReadinessToolsResolveToQueryRoutes`).

## FactSchemaVersions — GET x2

- Routes: `GET /api/v0/fact-schema-versions`,
  `GET /api/v0/fact-schema-versions/{fact_kind}`.
- Handler: `go/internal/query/fact_schema_version.go`
  (`FactSchemaVersionHandler` struct declaration, Profile-only); `Mount` at `:65-68`,
  list body at `:70-91`, detail body at `:93-127`.
- Exact static source: `facts.SupportedSchemaVersions()` at `:151`
  (`go/internal/facts/schema_version.go` `SupportedSchemaVersions()` definition, copies the precomputed
  `supportedSchemaVersionRegistry` at `schema_version.go:118`); detail leg
  uses `facts.SchemaVersion` (`:144`) and `facts.ClassifySchemaVersion`
  (`:164`), both pure lookups against the same static registry.
- No graph/content/store read: the struct holds only `Profile QueryProfile`;
  imports are `fmt`, `net/http`, `sort`, `strconv`, `strings`,
  `go/internal/facts`. The file's own doc comment (`:25-29`) states the data
  is the static in-binary registry and the handler reads no runtime, graph,
  or registry state. `rg` over the file returns no code reference to any
  graph, content, store, reader, or ReadStore symbol.
- Substitute freeze: OpenAPI path contract
  `go/internal/query/openapi/paths/status/fact_schema_version.go` + core
  handler tests `go/internal/query/fact_schema_version_test.go`
  (`TestFactSchemaVersionListReturnsRegistry`,
  `TestFactSchemaVersionListHonorsLimit`,
  `TestFactSchemaVersionListRejectsBadLimit`,
  `TestFactSchemaVersionDetailReturnsSupportedVersion`,
  `TestFactSchemaVersionDetailClassifiesCandidate`,
  `TestFactSchemaVersionDetail_LocalLightweightReturnsVersionData`,
  `TestFactSchemaVersionDetailUnknownKindReturns404`) + satellite
  check-parity byte-fragment expectation (satellite pins byte fragments of
  the served list/detail envelopes; in-tree half is the MCP resolve-route pin
  `go/internal/mcp/fact_schema_version_test.go:
  TestFactSchemaVersionToolsResolveToQueryRoutes`).

## SurfaceInventory — GET x1

- Route: `GET /api/v0/surface-inventory`.
- Handler: `go/internal/query/surface_inventory.go`
  (`SurfaceInventoryHandler` struct declaration, Profile-only construction at wiring; the
  `sync.Once` + cached `inventory` fields hold only the decoded embedded
  artifact, never a reader); `Mount` at `:57-59`, list body at `:64-99`,
  artifact load at `:49-54`.
- Exact static source: `capabilitycatalog.LoadSurfaceInventory()` at handler
  `:51` (`go/internal/capabilitycatalog/surfaces_load.go` `LoadSurfaceInventory()` definition), which unmarshals
  the `//go:embed data/surface-inventory.generated.json` bytes (`:24`) — a
  committed, generated artifact, not a store.
- No graph/content/store read: the struct holds `Profile` plus the
  once/cache fields; imports are `net/http`, `sync`, `capabilitycatalog`,
  `querycontract`, `tracing`. The file's own comments (`:21-24`, `:26-31`)
  state the load reads a compiled-in artifact, not a store, so there is no
  reader verdict to map. `rg` over the file returns no code reference to any
  graph, content, store, reader, or ReadStore symbol.
- Substitute freeze: OpenAPI path contract
  `go/internal/query/openapi/paths/catalog/surface_inventory.go` + core
  handler tests `go/internal/query/surface_inventory_test.go`
  (`TestSurfaceInventoryHandlerListsWithExactTruth`,
  `TestSurfaceInventoryHandlerFiltersByCategoryAndReadiness`,
  `TestSurfaceInventoryHandlerReturnsCollectorContracts`,
  `TestSurfaceInventoryHandlerRejectsBadLimit`,
  `TestSurfaceInventoryReadinessParity`) + the generator drift gate in
  `go/cmd/capability-inventory` (fresh build compared byte-identical against
  `capabilitycatalog.RawSurfaceArtifact`) + satellite check-parity
  byte-fragment expectation (satellite pins byte fragments of the served
  inventory envelope; in-tree half is the MCP resolve-route pin
  `go/internal/mcp/dispatch_surface_inventory_test.go:
  TestSurfaceInventoryToolResolvesToAPIRoute`).

## Visualization derive — POST x1

- Route: `POST /api/v0/visualizations/derive` (query-shaped POST: derivation
  over the caller-supplied body, no durable writes).
- Handler: `go/internal/query/visualization/handler.go`
  (`visualization.Handler` struct declaration, empty struct — zero fields, aliased as
  `query.VisualizationHandler` in `go/internal/query/visualization_alias.go`);
  `Mount` at `:34-36`, derive body at `:38-52`, pure dispatch at `:54-83`.
- Exact static source: pure derive — `BuildServiceStoryPacket`,
  `BuildEvidenceCitationPacket`, `BuildIncidentContextPacket` over the
  caller-supplied `source_response`; the truth envelope
  (`:95-106`) is built from the caller-supplied source truth only.
- No graph/content/store read: the struct is empty; imports are
  `encoding/json`, `fmt`, `net/http`, `strings`,
  `querycontract/evidence`, `query/incident/model`, `querycontract`. The
  file's own doc comment (`:18-20`) states the handler never queries graph,
  content, or reducer state. `rg` over the file returns no code reference to
  any graph, content, store, reader, or ReadStore symbol.
- Substitute freeze: OpenAPI path contract
  `go/internal/query/openapi/paths/evidence/visualization_packets.go` + core
  handler tests `go/internal/query/visualization/handler_test.go`
  (`TestHandlerDeriveMountsRouteAndBuildsServiceStoryPacket`,
  `TestHandlerDeriveCarriesSourceProfileIntoDerivationTruth`,
  `TestHandlerDeriveRejectsUnknownView`,
  `TestHandlerDeriveRejectsMalformedSourceResponse`) and
  `go/internal/query/visualization_packet_surface_test.go`
  (`TestVisualizationDeriveRouteBuildsServiceStoryPacket`,
  `TestVisualizationDeriveRouteSupportsEvidenceCitationAndIncidentContext`,
  `TestVisualizationDeriveRouteReturnsUnsupportedPacketForEmptyKnownView`,
  `TestVisualizationDeriveRouteRejectsUnknownView`,
  `TestOpenAPISpecIncludesVisualizationDeriveRoute`) + satellite check-parity
  byte-fragment expectation (satellite pins byte fragments of the derived
  packet envelope; in-tree halves are the MCP resolve-route pin
  `go/internal/mcp/visualization_packet_surface_test.go:
  TestVisualizationPacketToolRegisteredAndRoutesToDeriveSurface`, the
  HTTP/MCP envelope-parity pin `TestVisualizationPacketMCPMatchesHTTPEnvelope`
  (`equalJSON` on Data and Truth), and
  `TestSecondGenerationVisualizationPacketParityFromCanonicalData` in
  `go/internal/mcp/answer_parity_gen2_test.go`).

## Proof

Docs-only change: no behavior change, so no red-first cycle applies; the
freeze tests above are the green-before/green-after. Focused proof at this
base (exits recorded):

- `go build ./...` run from `go/` — exit 0.
- `go test -count=1` on `go/internal/query`,
  `go/internal/query/visualization`, `go/internal/extraction`,
  `go/internal/facts`, `go/internal/capabilitycatalog`,
  `go/internal/mcp`, `go/cmd/api` — all exit 0.
