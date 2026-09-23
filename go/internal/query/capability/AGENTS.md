# capability — scoped agent instructions

## Do not import query/contract from here

The capability rows in `go/internal/query/contract` register themselves at
init time and depend on shared contract types this package also uses. An
import edge from here to there cycles. Read the registry through
`querycontract.CompatibilityCapabilityMatrix` instead.

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
