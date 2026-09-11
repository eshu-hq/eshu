# Visualization Packet

## Purpose

The visualization-packet derivation route: `POST /api/v0/visualizations/derive`.
Derives a compact, bounded subgraph from an existing service-story,
evidence-citation, or incident-context response the caller already received
and was authorized to see.

## Ownership boundary

Owns the `Handler` struct, its HTTP dispatch, and the three derivation
builders (`BuildServiceStoryPacket`,
`BuildEvidenceCitationPacket`,
`BuildIncidentContextPacket`) plus their `FromMap` adapters.
Does not own the `Packet`/`VisualizationBuilder` type or bound
implementation (`querycontract`, promoted there for #6060 so a future
handler-family subpackage could build one without an import cycle), the
evidence-citation content model (`querycontract`), or the incident-context
read model (`incident/model`) -- those are separate leaves this package
calls into. Does not own `*TruthEnvelope` itself, only the truth this route's
own derivation reports.

## Layout

- `handler.go` -- `Handler`, `Mount`, `deriveVisualizationPacket`'s per-view
  dispatch, the request/response shapes, and the route's own derivation-truth
  builder.
- `packet.go` -- the `Visualization*` type aliases and thin function
  forwarders onto `querycontract`'s builder implementation
  (`View`, `Node`, `Edge`,
  `Packet`, `MaxNodes`, `MaxEdges`,
  `newVisualizationBuilder`, `unsupportedVisualizationPacket`,
  `visualizationNodeID`), used by every other file in this family.
- `decode.go` -- the `FromMap` adapters
  (`BuildEvidenceCitationPacketFromMap`,
  `BuildIncidentContextPacketFromMap`) that decode a canonical
  HTTP/MCP/CLI JSON map before delegating to the typed builder.
- `evidence.go` -- `BuildEvidenceCitationPacket` and
  `BuildIncidentContextPacket`, their node/anchor identity and
  label helpers, and their recommended-next-calls fallbacks.
- `story.go` -- `BuildServiceStoryPacket`, the service-anchor,
  evidence-graph, upstream, and downstream node/edge builders, and the
  confidence-to-truth-label mapping.
- Test files -- this package's own tests, moved in verbatim from root (see
  Move evidence); `packet_test.go`, `merge_test.go`, and
  `story_bench_test.go` construct fixtures hoisted to `querytestutil` (see
  AGENTS.md).

## Move evidence

The family moved here from the query root (`visualization_packet.go` and its
`visualization_packet_{handler,decode,evidence,story}.go` siblings, #6642
Part A, split off the #6060 lane A restructure); the non-test files are
destuttered (`visualization_packet_handler.go` -> `handler.go`, etc.) and
`VisualizationHandler` renamed to `Handler` at its declaration, with every
method name otherwise unchanged. Root keeps every pre-move exported spelling
through a stanza in the new `visualization_alias.go`: the `VisualizationHandler`
type alias (cmd/api's and cmd/mcp-server's wiring_router.go struct literals,
handler.go's `APIRouter` field), the `Packet`, `View`,
and `View*` aliases and the three `Build*`/`Build*FromMap`
forwarders (the staying `visualization_packet_surface_test.go` and
internal/mcp's `answer_parity_gen2_test.go` call these directly). Three of the
family's four test files (`visualization_packet_test.go`,
`visualization_packet_merge_test.go`, `visualization_packet_story_bench_test.go`)
moved in as `packet_test.go`, `merge_test.go`, `story_bench_test.go`;
`visualization_packet_surface_test.go` stays in root because it drives
`APIRouter.Mount` and `OpenAPISpec`, which are root-only.

Four fixtures both sides' tests needed (`freshTruth`, `storyResponseWithUpstream`,
`citationResponse`, `incidentResponse`) moved to
`querytestutil.FreshTruth`/`StoryResponseWithUpstream`/`CitationResponse`/
`IncidentResponse` with one definition each; root's
`visualization_packet_surface_test.go` calls the querytestutil spelling
directly rather than keeping a duplicate or a forwarding wrapper.
`nodeIDSet` had exactly one consumer file (the declaring one, now
`packet_test.go`) so it moved unexported rather than being hoisted.

No-Regression Evidence: baseline `origin/main` vs this branch -- `go build
./...` is clean; `go vet ./internal/query/... ./cmd/api ./cmd/mcp-server
./internal/mcp` reports no issues; `go test ./internal/query/... -count=1`,
`go test ./cmd/api ./cmd/mcp-server ./internal/mcp -count=1`, and `go test
./cmd/golden-corpus-gate/... -count=1` (the golden snapshot pins the
service-story packet shape through the root forwarder) pass with zero
failures; the root and
leaf test-name union (`go test ./internal/query/ -list '.*'` UNION `go test
./internal/query/visualization/ -list '.*'`) equals the pre-move `go test
./internal/query/ -list '.*'` list exactly -- 2878 names, no duplicate, no
drop -- and the same holds for `-list 'Benchmark.*'` (11 names, including
`BenchmarkBuildServiceStoryVisualizationPacketRetainedShape` moving from
root's list to the leaf's). This route issues no Cypher, so
`internal/queryplan`'s source-coverage manifest has no row naming it and
none needed re-pinning. `git diff origin/main --stat -- testdata/golden
testdata/cassettes` is empty.

No-Observability-Change: this route emits no span, metric, or log of its own
-- it is a pure in-process transformation with no graph, content, or
reducer read -- so there is nothing route-local to regress. The route path
(`POST /api/v0/visualizations/derive`) and its `visualization.packet_derivation`
truth capability string are unchanged.

## Related docs

- `docs/public/reference/visualization-packets.md`
- `go/internal/query/read-models.md`
