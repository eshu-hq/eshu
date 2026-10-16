# Agent instructions: rank

Read `doc.go` and `README.md` first.

## Invariants

- Exactly-once rests on the two cursor checks in `Walk` (basis fingerprint
  and served-set digest), never on score bits. Do not replace them with a
  `(score, id)` keyset: raw scores differ by up to 4 ulp across
  architectures, and a keyset would skip or repeat rows silently.
- The served-set digest must stay combinable (lane-wise addition), because
  `Advance` and `TrimWalk` mint the cursor for a page prefix without the
  window. Changing it breaks the MCP budget trim.
- `WindowSize` is the walk bound and is wire-public (it appears in
  `rank_walk.window_size` and the OpenAPI schema). Past it the walk ends; do
  not add a silent fallback to offset rows.
- No file here may put the query text or a cursor value into a span
  attribute, metric label, or log line. Labels are the closed `tool` and
  `outcome` sets.
- The package does no I/O and imports no ranker. Fetching and scoring the
  window belong to the route handlers.
- A change to the cursor layout bumps the `rc1.` prefix and `cursorVersion`.
- A `graph_name` or `lexical` window keeps its own row order. Never sort it:
  its pages must equal the offset pages of the same lane.
- The lane enters both the cursor and the basis. A route must pick the lane
  the way its offset request does; a walk must never force a lane.

## Verification

From `go/`:

```
go test ./internal/query/search/rank -count=1 -race
```
