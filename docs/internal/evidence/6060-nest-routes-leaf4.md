# 6060 nesting leaf 4: routes leaf (route-to-caller)

## What moved

`go/internal/query/codequery/route_to_caller.go` (274 lines) and
`route_to_caller_graph.go` (463 lines) collapsed into
`go/internal/query/codequery/route_handlers.go` (handler, impact assembler,
four grandfathered reads, forwarders) plus the `routes/` leaf:

- `routes/entry.go` — `Capability`, `Request` (+ `Normalize` /
  `Validate`), `AllowedByScope`, `Route` (+ `RouteFromRow` /
  `SelectRoute` / `RouteMap` / `HandlerMap`).
- `routes/graph.go` — `JoinRouteRows`, `RelationshipRows` (label now a
  parameter), `DirectionRows`, `LabelAllowed`.
- `routes/impact.go` — `SplitRelationships`, `EmptyImpact`,
  `MergeMaps`, and the six scoped-access helpers (exported as
  forwarder targets).
- `routes/test.go` — contract tests for the pure functions.

## Pin handling

- Four `grandfathered_non_hot.go` digests reproduce exactly (keys
  repathed `route_to_caller_graph.go` → `routes.go`, values frozen);
  verified by `go test ./internal/queryplan/`.
- One yaml `source_sha256` recomputed with the repo go/parser
  extraction (`DirectionRows`, now a leaf function; count stays 1 —
  still a single `.Run`); four reason-only entries repathed.
- `intLiteral` deleted; the leaf uses `strconv.Itoa` (identical
  rendered Cypher).

## No-Regression Evidence

Every Cypher literal moved verbatim; only the surrounding Go changed
(explicit graph, context, and grant parameters). Proof:

- `go test ./internal/query/codequery/routes/ -count=1` — ok (9 tests).
- `go test ./internal/query/codequery/ -count=1 -run
  'RouteToCaller|CallChain'` — 50 pass, 1 skip (live NornicDB proof,
  env-gated as before).
- `go test ./internal/queryplan/ -count=1` — ok (frozen digests
  reproduce, recomputed sha matches).
- `go vet ./internal/query/codequery/...` — clean.

## No-Observability-Change

No telemetry, span, metric, or log line changed: the handler emits
the same envelopes through the same writers; the leaf adds no
instrumentation of its own.
