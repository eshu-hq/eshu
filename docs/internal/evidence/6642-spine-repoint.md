# Spine repoint: envelope forwarders onto querycontract (#6642 PR 1)

PR 1 of the [query target tree](../design/6642-query-target-tree.md). Moves no
file and changes no query shape.

`requiredProfile` and `acceptsEnvelope` were unexported forwarders in
`go/internal/query/envelope_aliases.go` onto `querycontract.RequiredProfile` and
`querycontract.AcceptsEnvelope`. This repoints their call sites at the exported
twins and deletes the forwarders. `requiredProfile` is the most widely shared
unexported symbol in the package — 19 of the plan's destinations need it — so
every family that later leaves root would otherwise lose it on the way out.

The diff touches files that contain Cypher text, which is why the
content-based performance-evidence gate selects it. No query was added,
removed, or reshaped: the edits replace an unqualified call with its
package-qualified equivalent, and the callee is the same function in both cases.

No-Regression Evidence: the changed call sites resolve to the identical
function body (`querycontract.RequiredProfile`, `querycontract.AcceptsEnvelope`)
that the deleted forwarders already delegated to, so there is no runtime delta
to measure on any query path. Backend go1.27 darwin/arm64. Measured on the
branch from `go/`: `go build ./internal/query/` exit 0; `go vet ./internal/query/`
clean; `go test ./internal/query/... -count=1` 53 packages ok;
`go test ./internal/queryplan/ -count=1` ok. No Cypher statement text, anchor,
index, batch size, worker count, lease, or timeout changed.

No-Observability-Change: no span, metric, log key, or status field is added,
removed, or renamed. `handler_tracing.go` and its package-local
`queryHandlerTracer` test seam are untouched.

## Queryplan digests re-pinned

`queryplan` binds production symbols by content hash, so editing a function body
inside a pinned symbol invalidates the manifest even though nothing moved. Six
digests were re-derived, across seven pinned lines (one symbol is pinned twice),
in `query-source-coverage.yaml`, `hot-cypher.yaml` and `grandfathered_non_hot.go`,
by iterating the binding tests to a fixed point. `handler-hot-cypher.yaml` holds
no digest this change touches.

This is the first live instance of the trap the target-tree plan records, and it
arrived on a change that moved no file at all — the inverse of the predicted
shape, where a `git mv` leaves the hash valid and the key stale.
