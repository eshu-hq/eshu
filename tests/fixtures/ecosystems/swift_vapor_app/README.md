# swift_vapor_app

Golden-corpus fixture for the Swift Vapor route-handler dead-code detector
(#5337, #5378). The `swift.vapor_route_handler` root is gated on a per-file
`import Vapor`, so the two discrimination files carry the identical `use:`
shape with only the import differing:

- `Sources/App/routes.swift` — POSITIVE: `import Vapor` present, so the
  `app.get("health", use: healthCheck)` registration marks `healthCheck` as a
  `swift.vapor_route_handler` dead-code root. Nothing calls `healthCheck`, so the
  root is what keeps it out of the dead-code candidate buckets.
- `Sources/App/plain.swift` — FOIL: no `import Vapor`, so the byte-identical
  `use: statusReport` shape does NOT root `statusReport`; being unreferenced and
  unrooted, it must surface as a dead-code candidate.

These two pinned files are byte-for-byte load-bearing for the golden
discrimination below and are never edited to add richness. Corpus-quality
enrichment (#5569) instead adds a controller/model/middleware layer alongside
them, in the same app-shaped convention as
`tests/fixtures/dogfood/swift_real_repo`, so the fixture reads as more than a
two-file route-handler sliver:

- `Sources/App/Models/Widget.swift` — a `Content`-backed model plus an
  in-memory store.
- `Sources/App/Controllers/WidgetController.swift` — a `RouteCollection`
  controller with grouped `use:` route handlers.
- `Sources/App/Middleware/RequestLoggingMiddleware.swift` — a `Middleware`
  conformance.

None of the added files reference `healthCheck`, `statusReport`, or any other
symbol from the two pinned discrimination files, so they add new,
independently-classified dead-code candidates without moving the pinned pair
between buckets.

## Golden gate coverage & Ifá determination

The B-12 snapshot (`testdata/golden/e2e-20repo-snapshot.json`, HTTP query shape
`POST /api/v0/code/dead-code/investigate?golden_scope=swift_vapor_app`) pins the
discrimination live: `statusReport` (foil) must appear in the `cleanup_ready`
bucket with classification `unused`, and `healthCheck` (positive) must appear in
`suppressed` with classification `excluded`. Both object-matches are closed on
`(name, classification)`, so over-rooting the foil or under-rooting the real
handler fails the gate. Parser-tier proof:
`TestSwiftVaporGoldenFixtureDiscriminatesRouteHandler`.

Ifá materialized-edge coverage is **N/A**. `swift.vapor_route_handler` is a
content-store dead-code verdict that governs query-time suppression; it writes no
reducer/graph edge and adds no `reducer.MaterializedEdgeFamilies()` domain, so
there is no Ifá materialized-edge row to add.
