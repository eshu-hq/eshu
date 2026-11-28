# go/coreserve — versioned shared auth serving library

Nested module `github.com/eshu-hq/eshu/go/coreserve` (eshu-hq/eshu#11618):
the core serving seam (`CoreServeDeps`, `CoreAuthWiring`,
`NewCoreServeMux` plus the auth-family handler and field types) as
identity aliases over `go/internal/query`, so eshu-api and core-api
consume the same pinned implementation and exactly one binary computes a
given auth family.

- No copied query tree, no conversion layer, no per-leaf handler forks.
- `NewCoreServeMux` delegates to `query.NewCoreServeMux` (pure
  delegation, no new runtime path — see the no-observability-change note
  in `doc.go`).
- The serving binary still builds `CoreAuthWiring` itself: secret-sealed
  material must never cross package `query` (or this package, which
  never imports `internal/secretcrypto`).
- v0.1.0 does NOT export `component.Policy`, `telemetry.Instruments`,
  `status.AnswerNarrationStatus`, `APIRouter`, or `ExtraMounts`:
  external callers leave those `CoreServeDeps` fields zero (nil/empty),
  the documented safe defaults. Widening needs a follow-up version.

## Versioning

Tag sequence (coordinator cuts tags; this branch creates neither tags
nor the PR):

1. Merge with the parent require pinned to the pseudo-version of the
   merge commit.
2. Coordinator tags the parent module first (`go/v0.1.0` at the merge
   commit — no `go/*` tag exists yet), since the `coreserve` require
   resolves through the parent module.
3. Bump the `coreserve` require to that parent tag.
4. Tag `go/coreserve/v0.1.0`; consumers add
   `require github.com/eshu-hq/eshu/go/coreserve v0.1.0`.
   Committed `go.mod`/`go.sum` carry NO `replace` directives.

## Verification

```bash
cd go/coreserve && go test ./... -count=1 && go vet ./...
cd go && go test ./internal/query -count=1
```

`rg -n replace go/coreserve/go.mod` must print nothing in the committed
tree. The eshu-api RED consumer (`coreserveconsumer`) turns green only
post-tag; it is not claimed in this step.
