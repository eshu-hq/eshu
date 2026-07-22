# goldengate

The pure, importable **assertion core** of Eshu's B-7 golden end-to-end corpus
gate ([#3800](https://github.com/eshu-hq/eshu/issues/3800)). It owns the typed
contract of the B-12 golden snapshot and every `Evaluate*` function that turns an
observed value plus that contract into a `Finding` — with **no I/O**.

## Why it is its own package

The assertions used to live in `package main` inside `cmd/golden-corpus-gate`,
so nothing else could import them. Two consumers need the *same* logic:

- **`cmd/golden-corpus-gate`** — the in-repo gate. Wires a live pipeline (real
  Postgres, real NornicDB over Bolt, a running `eshu-api` / `eshu-mcp-server`),
  reads observed values from those systems, and feeds them to these functions.
- **`go/conformance`** — the out-of-tree contributor conformance suite
  ([#4112](https://github.com/eshu-hq/eshu/issues/4112) / R-10). Replays a
  committed cassette through the offline materialization seam with **zero
  provider credentials and zero Docker**, derives the same observed values in
  memory, and feeds them to these *same* functions.

Because both call the identical `Evaluate*` logic, the contributor's
credential-free proof and the in-repo gate cannot drift — there is no forked
assertion copy to keep in sync. `cmd/golden-corpus-gate/shared.go` re-exports
these symbols under package-local names so the gate's existing call sites and
tests are unchanged.

## What it contains

| File | Holds |
|------|-------|
| `snapshot.go` | `Snapshot` and its nested contract types (`GraphSnapshot`, `CountRange`, `RequiredCorrelation`, `RequiredNode`, `RequiredSelfLoop`, `DrainAssertions`, `DrainBound`, `QueryShapes`, `QueryShape`, `AbsentWhenPresent`) plus `LoadSnapshot`. |
| `report.go` | `Finding` and `Report` — the pass/fail accumulator with the required/advisory split. |
| `evaluate.go` | `DrainCounts` and every `Evaluate*` function (drains, required correlations, edge/node properties, required/present nodes, required self-loops, node/edge counts, query shape, API/MCP/CLI parity, timing). |
| `query_shape_paths.go` | Bounded deep JSON path/value assertions for query shapes, including array traversal with `[]`. |

## Assertion semantics worth knowing

- **Required vs advisory.** A required finding that fails makes `Report.Failed()`
  true; an advisory finding is reported but never blocks. The minimal gate blocks
  only on a configured subset and reports the rest advisory.
- **Edge properties are absence-zero.** Among the evidence-narrowed matching
  edges, *any* edge missing the property (or carrying a non-allowed value) fails.
  No matching edges passes vacuously — the companion `MinimumCount` finding
  guards existence.
- **Node properties are presence-positive.** At least `MinimumCount` nodes must
  carry a non-empty (optionally pinned) value. A label legitimately contains
  property-less nodes (a `LICENSE` has no language), so the gate asserts a floor
  of tagged nodes rather than the absence of any untagged node.
- **Self-loops are closed-range, not floor-only.** `RequiredSelfLoop` pins the
  count of `(n:Label {NodeProperty: NodePropertyValue})-[:Relationship]->(n)`
  edges to `[MinimumCount, MaximumCount]`. A floor alone cannot separate
  "genuine recursion survives" from "every declaration became a spurious
  self-loop" (the [#5332](https://github.com/eshu-hq/eshu/issues/5332) class of
  bug) since both push the same observed count up — the ceiling is what catches
  the regression. `NodeProperty`/`NodePropertyValue` scope the match to one
  language/family sharing a node label so it is not conflated with another's
  self-loop count.
- **Query path assertions are explicit.** `RequiredJSONPaths` and
  `RequiredJSONValues` walk only the dot paths named by the snapshot. A `[]`
  suffix traverses a non-empty array, which lets the dead-code replay library
  assert evidence citations and confidence labels without an unbounded response
  scan.
- **Mutual-exclusion assertions catch disclosure-vs-served contradictions.**
  `RequiredAbsentWhenPresent` (`AbsentWhenPresent`) fails only when a sibling
  JSON path is present AND a domain marker path (e.g.
  `evidence_boundaries[].domain`) resolves the matching domain value in the
  SAME response — the class of bug that shipped twice on
  [#5472](https://github.com/eshu-hq/eshu/issues/5472): a tool claims a domain
  is absent while a sibling field in its own response actually serves it. Both
  sides passing vacuously when the sibling is absent keeps existing, honest
  boundary disclosures green; the companion existence assertions
  (`RequiredJSONPaths`/`RequiredJSONValues` on `evidence_boundaries[].domain`)
  still guard that a real boundary is disclosed in the first place.
- **CLI parity is explicit metadata.** `query_shapes.cli` rows must name their
  CLI argv and truth class. `EvaluateQuerySurfaceParity` checks every
  `parity_with` peer exists and carries the same truth class, so API/MCP/CLI
  shared query surfaces cannot drift silently in the committed snapshot.
- **An empty `Report` is a failure.** A gate that asserted nothing has not proven
  anything.

## Testing

Pure unit tests live beside the code (`evaluate_test.go`, `report_test.go`,
`property_test.go`, `snapshot_test.go`). They run in every default
`go test ./...` pass — no backend, no network:

```bash
cd go && go test ./internal/goldengate/... -count=1
```
