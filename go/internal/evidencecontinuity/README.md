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
`.github/workflows/static-contract-gates.yml`. On top of those packages, every
input `ValidateRepository` reads must stay in both trigger sets, because an edit
to any of them can change what this gate reports:

- the contract spec `specs/evidence-continuity.v1.yaml`
- `specs/capability-matrix.v1.yaml` and the `specs/capability-matrix/` fragments
- the generated surface inventory
  `go/internal/capabilitycatalog/data/surface-inventory.generated.json`
- the CI gate registry `specs/ci-gates.v1.yaml` and the workflow
  `.github/workflows/static-contract-gates.yml`, the two files the check itself
  reads to decide the coverage above

Those anchors are what make the check self-enforcing: an edit that could create
a blind spot also selects the gate that would catch it.

The anchor list is the whole of the claim, and it is deliberately explicit
rather than "the spec". An earlier version anchored only the contract spec
while the validator also read the capability matrix, so a capability-id rename
passed this gate green and surfaced later as `unknown_capability` on an
unrelated pull request. The surface inventory is anchored for the same reason
even though `go/internal/capabilitycatalog/**` covers it today: that trigger is
demanded by the package check, which probes only `_test.go` files in the package
root, so narrowing it to `*_test.go` would keep the package check green and drop
`data/` from the gate's reach. The registry and the workflow are anchored for the
same reason once more: nothing else in the repo requires a gate to trigger on
either file — `checkPathFilterCoverage` compares the two sets against each other,
which stays satisfied when a path leaves both — so dropping one would pass every
gate and leave the next trigger narrowing unwatched. A family read as a directory
carries two differently named probe paths, because a single probe cannot tell a
directory-wide glob from a filename-narrowed one such as
`specs/capability-matrix/a*.yaml`. If `ValidateRepository` grows a new input, add
it to `validatorInputs` in the same change.

## Related docs

- `specs/evidence-continuity.v1.yaml`
- `specs/README.md`
- `docs/public/reference/capability-conformance-spec.md`
- `docs/public/reference/local-testing.md`
