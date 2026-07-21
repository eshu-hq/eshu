# swift_vapor_app

Golden-corpus fixture for the Swift Vapor route-handler dead-code detector
(#5337, #5378). The `swift.vapor_route_handler` root is gated on a per-file
`import Vapor`, so the two files carry the identical `use:` shape with only the
import differing:

- `Sources/App/routes.swift` — POSITIVE: `import Vapor` present, so the
  `app.get("health", use: healthCheck)` registration marks `healthCheck` as a
  `swift.vapor_route_handler` dead-code root. Nothing calls `healthCheck`, so the
  root is what keeps it out of the dead-code candidate buckets.
- `Sources/App/plain.swift` — FOIL: no `import Vapor`, so the byte-identical
  `use: statusReport` shape does NOT root `statusReport`; being unreferenced and
  unrooted, it must surface as a dead-code candidate.
