# evidencecontinuity

## Purpose

`internal/evidencecontinuity` owns the static verifier for
`specs/evidence-continuity.v1.yaml`. It keeps evidence-centric public
capabilities tied to explicit source, projection, API, MCP, empty-state, and
negative evidence-loss proof.

## Ownership boundary

This package validates contract coverage only. It does not query Postgres,
NornicDB, Neo4j, MCP, HTTP handlers, collectors, or reducers, and it does not
prove runtime behavior directly.

## Exported surface

`doc.go` carries the godoc contract. The exported surface is the contract schema
(`Contract`, `Row`, proof structs), `SurfaceIndex`, the stable `Finding` and
`FindingKind` taxonomy, and repository entry points `ValidateRepository`,
`LoadContract`, `LoadSurfaceIndex`, `Validate`, and `FormatFindings`.

## Dependencies

The package imports only the Go standard library plus `gopkg.in/yaml.v3`. It
reads the capability matrix under `specs/` and the generated surface inventory
under `go/internal/capabilitycatalog/data/`.

## Telemetry

No runtime telemetry is emitted. This verifier runs as a local and CI gate, and
failures are reported through deterministic finding codes.

## Gotchas / invariants

Rows must reference known capability IDs, generated API routes, generated MCP
tools, API routes declared for the same capability in the evidence-continuity
matrix, and MCP tools declared on the same capability in the capability matrix.
The negative evidence-loss cases stay closed over empty, missing, stale,
truncated, and inaccessible evidence. Go proof references must use exact
anchored test names that resolve to real `_test.go` declarations; broad regexes
and prose are not accepted.

## Related docs

- `specs/evidence-continuity.v1.yaml`
- `specs/README.md`
- `docs/public/reference/capability-conformance-spec.md`
- `docs/public/reference/local-testing.md`
