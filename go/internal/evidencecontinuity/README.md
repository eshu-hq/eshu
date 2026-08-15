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

The package imports the Go standard library, `gopkg.in/yaml.v3`, and
`internal/cigates` (registry loading, `MatchGlob`, `DornyFilters`). It reads
the capability matrix under `specs/`, the generated surface inventory under
`go/internal/capabilitycatalog/data/`, the CI gate registry
`specs/ci-gates.v1.yaml`, and the workflow
`.github/workflows/static-contract-gates.yml`.

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

The verifier also guards its own gate's reach (`gate_trigger_gap`): every
package a `go test` proof ref names must be spanned by the evidence-continuity
triggers in `specs/ci-gates.v1.yaml` AND by the `evidence` path filter in
`.github/workflows/static-contract-gates.yml`, and the spec file itself must
stay in both trigger sets. That anchor is what makes the check self-enforcing:
a blind spot can only be created by editing the spec or the trigger lists, and
all of those edits select this gate.

## Related docs

- `specs/evidence-continuity.v1.yaml`
- `specs/README.md`
- `docs/public/reference/capability-conformance-spec.md`
- `docs/public/reference/local-testing.md`
