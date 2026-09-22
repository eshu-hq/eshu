# Spine repoint: authz and capability forwarders onto querycontract (#6642 PR 1b)

PR 1b of the [query target tree](../design/6642-query-target-tree.md). Moves no
file and changes no query shape.

PR 1 ([#6977](https://github.com/eshu-hq/eshu/pull/6977)) repointed the envelope
pair, `requiredProfile` and `acceptsEnvelope`. It did not clear the rest of the
spine. Three unexported root symbols remained, each a one-line forwarder onto an
exported twin that already existed in `querycontract`:

| symbol | declared in | root files naming it | twin |
| --- | --- | ---: | --- |
| `capabilityUnsupported` | `handler.go` | 25 | `querycontract.CapabilityUnsupported` |
| `repositoryAccessFilterFromContext` | `repository_authz.go` | 30 | `querycontract.RepositoryAccessFilterFromContext` |
| `repositoryAccessFilter` (a type) | `repository_authz.go` | 24 | `querycontract.RepositoryAccessFilter` |

`repository_authz.go` carried a comment recording the deliberate decision to
keep them — "Go has no function aliases, so the ~185 call sites keep this
unexported name rather than being rewritten." #6642's no-retained-aliases rule
overturns that, because each of these names pins its callers to package `query`
and so blocks every family that later leaves root.

The exported `RepositoryAccessFilter` spelling stays. It is a deliberate
impact-seam alias named in exported forwarder signatures (#6060), not a
compat shim, and nothing in this issue retires it.

`startQueryHandlerSpan` is the fifth symbol in the plan's spine table and is
**not** touched here: the `tracing` -> `span` rename belongs to
[#6818](https://github.com/eshu-hq/eshu/issues/6818), whose PR has already
merged.

No-Regression Evidence: every changed call site resolves to the identical
function body the deleted forwarder already delegated to, so there is no runtime
delta to measure on any query path. The one signature change,
`relationshipEdgesCypher(entry, access querycontract.RepositoryAccessFilter)`,
swaps an alias for the type it already aliased; the Cypher string the function
returns is byte-identical. Backend go1.27 darwin/arm64. Measured on this branch
from `go/`: `go build -gcflags=-e ./internal/query/...` exit 0; `go vet
./internal/query/...` exit 0; `go test ./internal/query/... ./internal/queryplan/...
-count=1` exit 0, 54 packages ok. No Cypher statement text, anchor, index, batch
size, worker count, lease, or timeout changed.

No-Observability-Change: no span, metric, log key, or status field is added,
removed, or renamed. `handler_tracing.go` is untouched.

## Queryplan digests re-pinned

Ten digests moved across eleven pinned lines — `(*InfraHandler).searchResources`
is pinned twice — in `query-source-coverage.yaml`, `hot-cypher.yaml` and
`grandfathered_non_hot.go`. Each was re-derived by iterating the binding tests
to a fixed point, never by editing a hash by hand.

`go/internal/query/codequery/metrics/edges.go` carries pre-existing gofumpt
drift and is deliberately absent from this diff, so its digest does not move.

## The digest surface spans two packages

`go test ./internal/queryplan/...` is not sufficient to prove the manifest is
consistent. `go/internal/query/queryplan_legacy_production_binding_test.go`
loads the same `testdata/hot-cypher.yaml` and binds `relationshipEdgesCypher`,
a symbol the `queryplan` package's own tests never check. This change left
`./internal/queryplan/...` fully green while `QP-RELATIONSHIPS-EDGES` was still
stale, and only the recursive `./internal/query/...` run caught it.

Any later PR in this series that touches a pinned symbol must run both
surfaces. Running one and reporting it as "the queryplan gate" is a false green.
