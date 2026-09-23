# capability — scoped agent instructions

## Keep the blank query/contract import in lookup.go

`lookup.go` imports `go/internal/query/contract` blank. It is load-bearing:
the capability rows register themselves from that package's `init()`s, and
this import is the only thing that links them into a binary. Nothing fails to
compile without it -- measured: `go build ./internal/query/capability/` still
exits 0. What breaks is the first request, at run time:
`querycontract.BuildTruthEnvelope` panics with
`query capability "capability_catalog.list" missing from capability matrix`,
which `TestHandlerListServesCatalogAtItsOwnRoute` catches.

Keep it blank by convention, not because the compiler forces it. Naming an
exported `contract` symbol from here does compile -- measured, `go build`
exits 0 on a file that references `contract.CapabilityQueryPlaybooks`. The
reason to read the assembled registry through
`querycontract.CompatibilityCapabilityMatrix` instead is that the registry is
the intended seam: a direct symbol reference would put this package's read
path on registration's internals, and the next `contract/` reshuffle would
then reach in here. Treat that as a design call you may argue with, not as a
constraint the build enforces.

There is no cycle in either direction -- nothing under `contract/` or
`querycontract/` imports this package, and that holds for their test binaries
too, which is the check worth running because a test-only back-edge is exactly
what a plain `go list -deps` would miss:

    go list -deps -test ./internal/query/contract/      | grep query/capability
    go list -deps -test ./internal/query/querycontract/ | grep query/capability

Both are empty. Run them before claiming a cycle here; the claim they replace
was in this file, in `README.md` and in `doc.go`, and was wrong in all three.

## Registration belongs to contract, not here

Before moving any `capability_*.go` file into this package, check whether it
calls `register`. `capability_matrix.go`, `capability_matrix_ext.go`,
`capability_matrix_terraform.go` and `contract/capabilities.go` all do, and
all belong in `query/contract` despite their names. The filename is not the
family (#6642).

## Root still owns some capability names

`go/internal/query/capability_keys.go` stays in `package query`. Its five ids
are named by root's own handlers. Only `CatalogKey`, which both sides name,
lives here — exported, and spelled without the package-name stutter that
`CatalogCapability` would carry.

## Its test lives at root

`capabilities_test.go` exercises the handler through root's `APIRouter`, so it
cannot move here without cycling. Add handler-internal tests here; add
routing-level tests beside the router.
