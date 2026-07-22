# Evidence — #5584 route-serves-data registry

The performance-evidence gate flags
`route_serves_data_registry_routes.go` / `route_serves_data_registry_routes_2.go`
because their evidence-citation strings contain Cypher-shaped text
(`MERGE (m:TerraformModule ...)`, `MATCH (n:CloudResource)`). Those strings
are verbatim MARKERS quoted from existing writers and handlers so the #5584
gate can verify citations against real source — no query, writer, or
runtime path changed.

No-Regression Evidence: no runtime Cypher, SQL, index, worker, queue, or
handler behavior changes in this PR. The new `go/internal/mcp` files are
compile-time registry data plus test-time verification that reads committed
source files with `os.ReadFile` and `go/parser`; the only touched production
file (`read_surface_route_serves_data.go`) changed a doc comment. The gate
itself (`go test ./internal/mcp -run TestRouteServesDataRegistry -count=1`)
runs the full 19-route x 24-domain matrix in under one second on the local
toolchain, and the full `./internal/mcp/... ./internal/query/...` suites
pass unchanged (`ok` in 2.4s / 1.5s).

No-Observability-Change: no runtime code path is added or altered; the
registry and its checks are test-only surfaces, so no metrics, spans, logs,
or status outputs change.

Derivation rationale and the architect-review flags live in
`docs/internal/design/5584-route-serves-data-registry.md`.
