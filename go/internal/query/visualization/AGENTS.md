# Visualization Packet — Agent Instructions

Scope: `go/internal/query/visualization/` (package `visualization`).

## Ownership

This leaf owns the visualization-packet derivation route (#6642 Part A):
`handler.go` (`Handler`, `Mount`, dispatch), `packet.go` (the `Visualization*`
type aliases and builder forwarders onto `querycontract`), `decode.go` (the
`FromMap` adapters), `evidence.go`
(`BuildEvidenceCitationPacket`,
`BuildIncidentContextPacket`), and `story.go`
(`BuildServiceStoryPacket`), plus this package's own tests,
three moved in verbatim from root -- see README.md's Move evidence.

## Invariants

- MUST NOT import root package `query` -- root would import this package
  back for the compatibility aliases in `visualization_alias.go`, cycling.
  Reach root-only helpers through `querycontract` (the visualization builder
  implementation, content model, row/string/HTTP helpers) or `incident/model`
  (the incident-context read model the third builder consumes); if neither
  has what you need, it does not belong here -- ask before adding a new
  shared home.
- Every builder in `evidence.go` and `story.go` MUST stay a pure
  transformation of the source response it is handed: no graph, content, or
  reducer read. That is the whole point of the route -- a scoped caller
  reaches it with no tenant-scoped data to filter, because it never touches a
  backend.
- Node and edge IDs MUST stay derived from the underlying entity/handle
  identity via `visualizationNodeID`, never from iteration order --
  `TestServiceStoryVisualizationDeterministicOrdering` and its incident/
  citation siblings pin this.
- `VisualizationHandler` is exported at its declaration (as `Handler`)
  because root's `handler.go` `APIRouter` field and both cmd wiring files
  spell `query.VisualizationHandler`, through the alias in
  `visualization_alias.go`.
- `Packet`, `View`, and the `View*`
  constants are exported because the staying root
  `visualization_packet_surface_test.go` and internal/mcp's
  `answer_parity_gen2_test.go` spell them as `query.Packet` /
  `query.View*` through the aliases in `visualization_alias.go`.
- `BuildServiceStoryPacket`,
  `BuildEvidenceCitationPacketFromMap`, and
  `BuildIncidentContextPacketFromMap` keep root forwarders in
  `visualization_alias.go` because internal/mcp's `answer_parity_gen2_test.go`
  calls all three as `query.Build*` directly, and
  `cmd/golden-corpus-gate/snapshot_test.go` calls
  `query.BuildServiceStoryVisualizationPacket` to pin the service-story packet
  shape against the golden snapshot. Any change here must keep
  `go test ./cmd/golden-corpus-gate/... -count=1` green.
- `Node`, `Edge`, `Limits`,
  `Truncation`, `MaxNodes`, `MaxEdges`,
  and the unexported `visualizationBuilder`/`newVisualizationBuilder`/
  `unsupportedVisualizationPacket`/`visualizationNodeID` names stay
  package-local aliases in `packet.go` with no root forwarder: no root or
  internal/mcp caller outside this family names them (confirmed by the #6642
  census's EXT/IN symbol inventory).

## Test fixtures hoisted to querytestutil (#6608 rule)

A fixture this package's tests share with root's staying
`visualization_packet_surface_test.go` moved to `querytestutil` as an
exported helper (one definition; both sides call the identical qualified
name -- no forwarding wrapper on either side), rather than being duplicated.
Do not re-duplicate any of these back into a `_test.go` copy here or in root:

- `querytestutil.FreshTruth` -- root's `visualization_packet_surface_test.go`
  and this package's `packet_test.go`, `merge_test.go`, and
  `story_bench_test.go` all call it directly.
- `querytestutil.StoryResponseWithUpstream` -- same four call sites.
- `querytestutil.CitationResponse` -- root's
  `visualization_packet_surface_test.go` and this package's `packet_test.go`.
- `querytestutil.IncidentResponse` -- same two call sites.

`nodeIDSet` (`packet_test.go`) has exactly one consumer file -- the file that
declares it -- so it stayed unexported in the leaf rather than moving to
querytestutil; it is not a shared fixture in the #6608 sense.

## Naming

`docs/internal/naming.md` is law: no `visualization_packet_` file prefixes,
no `visualization/visualization_packet.go`, `VisualizationHandler` renamed to
`Handler` at its declaration. The `Visualization*` type aliases in
`packet.go` keep their pre-move spelling despite the package-name stutter
that spelling now carries (`visualization.Packet`): they are
plain forwarders onto `querycontract`'s identically-named types with zero
external consumers besides the root aliases named above, so renaming them
would only rename the query-root's own compatibility-alias RHS and every
`querycontract` cross-reference in this family's comments, for no reader
benefit. The root `visualization_alias.go` keeps every old exported spelling
for staying callers.
