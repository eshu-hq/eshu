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

It is a blank import and must stay one. Read the assembled registry through
`querycontract.CompatibilityCapabilityMatrix`, never by naming a `contract`
symbol: the registry is the seam, and a symbol reference would put this
package's read path on registration's internals.

There is no cycle in either direction -- nothing under `contract/` or
`querycontract/` imports this package. Check before claiming otherwise:
`go list -deps ./internal/query/contract/ | grep query/capability` is empty.

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
