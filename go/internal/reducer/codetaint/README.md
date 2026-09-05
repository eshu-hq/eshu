# internal/reducer/codetaint

## Purpose

Materializes the `code_taint_evidence` and `code_interproc_evidence` reducer
domains: value-flow taint findings attached to their Function as
`CodeTaintEvidence` graph nodes, and cross-function value-flow findings
projected as `TAINT_FLOWS_TO` edges between Function nodes (issue #6061).

It bundles both families into one package instead of two siblings
(`codetaint`/`codeinterproc`) because the two subjects are interleaved at the
file level, not the dependency level, so a clean split does not exist yet.
There is no true import cycle between the families: no
`code_interproc_*.go` file references any `CodeTaintEvidence*` symbol (the
only hit is a doc comment cross-reference in
`code_interproc_evidence_materialization.go`). The sibling `valueflow`
package's value-flow fixpoint solver (`value_flow_fixpoint_evidence_loader.go`,
a different family, not this one) imports this package for its evidence
writer/ledger/uid-namespace surface — a one-directional leaf-to-leaf
dependency, not a cycle, since this package never imports `valueflow` back.
The reducer root's `code_value_flow_stale_cleanup_runner.go` reaching into
this package's exported symbols is a normal root-imports-leaf relationship.
The real obstacle is that
`code_taint_evidence_typed_decode.go` — a taint-prefixed file — implements
the decode/quarantine functions for BOTH fact kinds:
`DecodeCodeInterprocEvidenceInput` and
`ExtractCodeInterprocEvidenceRowsWithQuarantine` are interproc functions that
live in that file, and `code_interproc_evidence_materialization.go` (the
would-be `codeinterproc` handler) calls
`ExtractCodeInterprocEvidenceRowsWithQuarantine` from it. Splitting the two
families into separate packages today would require first splitting
`code_taint_evidence_typed_decode.go` so each family's decode/quarantine
logic lives under its own name; bundling both here instead keeps the move
mechanical. A later split is possible once someone separates those
functions out of that one file.

## Ownership boundary

**Owns:** `CodeTaintEvidenceMaterializationHandler` and
`CodeInterprocEvidenceMaterializationHandler` (the two reducer intent
handlers), their loader/writer ports, the typed-decode + quarantine seam for
both fact kinds, the row/edge projection functions, and the projected-node /
projected-edge ledgers and their startup backfillers.

**Does not own:** the value-flow fixpoint solver
(`ValueFlowFixpointEvidenceLoader`/`ValueFlowFixpointEvidenceProjector` in
the sibling `valueflow` package) — that is a different family (durable cross-repo summaries
solved into a `Program`) that happens to produce `CodeInterprocEvidenceInput`
rows and calls through this package's `ExtractCodeInterprocFixpointEvidenceRows`
and `SourceUIDsFromRows`/`UnresolvedCodeInterprocEndpointCount` exports. Also
does not own `code_value_flow_stale_cleanup_runner.go` (root) — the
generation-scoped stale-evidence sweep that calls through this package's
writer/ledger interfaces and `CodeTaintEvidenceSource()`/
`CodeInterprocEvidenceSource()` accessors.

## Exported surface

| symbol | what it is |
|---|---|
| `CodeTaintEvidenceMaterializationHandler` / `CodeInterprocEvidenceMaterializationHandler` | the two reducer intent handlers |
| `CodeTaintEvidenceLoader` / `CodeTaintEvidenceWriter` | taint loader/writer ports |
| `CodeInterprocEvidenceLoader` / `CodeInterprocEvidenceFactLoader` / `CodeInterprocEvidenceWriter` | interproc loader/writer ports (two loader shapes: typed-input for the fixpoint solver, raw-envelope for the materialization handler) |
| `CodeTaintEvidenceInput` / `CodeInterprocEvidenceInput` | the decoded per-finding row shapes |
| `ExtractCodeTaintEvidenceRows` / `ExtractCodeInterprocEvidenceRows` / `ExtractCodeInterprocFixpointEvidenceRows` | pure row/edge projection (fixpoint uses a separate uid namespace so it cannot clobber direct fact rows) |
| `ExtractCodeTaintEvidenceRowsWithQuarantine` / `ExtractCodeInterprocEvidenceRowsWithQuarantine` | the production decode+quarantine path the handlers call |
| `DecodeCodeTaintEvidenceInput` / `DecodeCodeInterprocEvidenceInput` | the single-envelope typed decode, exported for the root's shared codedataflow benchmark |
| `CodeTaintEvidenceProjectedNodeLedger` / `CodeInterprocProjectedEdgeLedger` | anchored-delete-by-uid ledgers (issue #4893) |
| `CodeTaintEvidenceProjectedNodeBackfiller` / `CodeInterprocProjectedEdgeBackfiller` | one-time startup ledger backfills |
| `CodeTaintEvidenceSource()` / `CodeInterprocEvidenceSource()` / `CodeInterprocFixpointEvidenceSource()` | evidence-source tag accessors, used by root's stale-cleanup runner and `cmd/reducer` wiring |
| `CodeTaintEvidenceDomainDefinition()` / `CodeInterprocEvidenceDomainDefinition()` | `DomainDefinition` constructors for root's additive-domain registration |
| `SourceUIDsFromRows()` / `UnresolvedCodeInterprocEndpointCount()` | shared with the root fixpoint loader for its own ledger recording and structured logging |
| `GraphQueryRunner` / `CodeValueFlowBackfillStateMarker` | locally-declared ports (see Dependencies) |

The reducer root wires `CodeEvidenceHandlers.CodeTaintEvidence*` /
`CodeInterproc*` (`defaults_handlers.go`) to this package's interfaces and
constructs the two handlers in `defaults_additive_domains_incident_code.go`.
`cmd/reducer` constructs the concrete Postgres/Cypher writers and ledgers and
the two backfillers (`canonical_graph_writers.go`).

## Dependencies

`internal/reducer/contract` (the `Intent`/`Result`/`Domain` shapes, aliased
`reducercontract`), `internal/reducer/factdecode` (quarantine partitioning
and telemetry recording), `internal/reducer/payloadcore` (deref/trim/convert
helpers), `internal/reducer/schemadecode` (the typed-payload decode seam),
`internal/facts`, and the generated `sdk/go/factschema` packages. No
dependency on the reducer root, and none of the root's other family
subpackages.

Two root-owned interfaces this package's backfillers need
(`GraphQueryRunner`, the graph read port; `CodeValueFlowBackfillStateMarker`,
the durable per-source completion marker) are **locally redeclared** in
`graph_ports.go` rather than imported: both are shared by other families
still in the reducer root (`GraphQueryRunner` by five others, plus the
sibling `valueflow` package's own separate local redeclaration;
`CodeValueFlowBackfillStateMarker` by the `projected_source_edge_backfill`
family too), so they are not this package's to own, and importing the root
to reach them would violate the "a family never imports the reducer root"
rule. Go interfaces are satisfied structurally, so the same concrete
implementations `cmd/reducer` wires into root's other families also satisfy
these local declarations with no logic duplicated. `derefFloat64` is
similarly kept as a small local unexported copy (root's version,
`supply_chain_impact_match.go`, is real logic for the unrelated
`vulnerability.cve` domain — not worth a leaf-package hoist for one 4-line
nil-guard).

## Telemetry

`eshu_dp_reducer_input_invalid_facts_total{domain, fact_kind}` (a malformed
required field quarantined through `factdecode.RecordQuarantinedFacts`),
`Result.SubSignals["input_invalid_facts"]`, and the standard
`eshu_dp_reducer_executions_total` / `eshu_dp_reducer_run_duration_seconds`
/ `eshu_dp_postgres_query_duration_seconds` for handler and writer execution.
The two backfillers emit one structured log each ("code taint evidence
projected node backfill complete" / "code interproc projected edge backfill
complete") and no metric of their own. Unchanged by this move: same metric
names, same emission sites, only the package that owns the code moved.

## Gotchas / invariants

- **The ledger record must happen before the graph write.** Both
  `RecordProjectedNodes`/`RecordProjectedEdges` calls are ordered before the
  corresponding `Write*` call in both handlers, so the ledger stays a
  superset of graph state; the anchored-delete retract on the next
  generation depends on that invariant (issue #4893).
- **`GraphQueryRunner` and `CodeValueFlowBackfillStateMarker` are
  intentionally re-declared here, not imported.** Do not "fix" this by
  importing the reducer root — see Dependencies above. If a future move
  hoists either to a shared leaf package, replace both declarations with an
  import of that leaf, not with a root import.
- **`ExtractCodeInterprocFixpointEvidenceRows` uses a separate uid
  namespace from `ExtractCodeInterprocEvidenceRows`** so a fixpoint-solved
  edge can never collide with (and silently overwrite) a direct-fact edge in
  the graph writer's `MERGE`. Do not unify the two uid derivations.
- **`DecodeCodeTaintEvidenceInput`/`DecodeCodeInterprocEvidenceInput` are
  exported only for the root's `codedataflow_bench_test.go`** (a shared
  benchmark file that also measures the unrelated function-summary/source
  and shell-exec families and could not move here). Treat them as an
  internal decode step, not a public API other callers should reach for.

## Related docs

- `go/internal/reducer/README.md` — the root package and its subpackage inventory
- `docs/internal/design/package-restructure.md` — the #6061 restructure
- `docs/public/observability/telemetry-coverage.md` — the coverage rows for these two domains

No-Regression Evidence: #6061 moves the code_taint_evidence and
code_interproc_evidence materialization handlers, their loader/writer ports,
typed decode + quarantine, row/edge projection, and projected-node/-edge
ledgers plus backfillers out of the reducer root into this new package,
without changing any field, exported behavior, or call order. The two
root-owned interfaces the backfillers need (`GraphQueryRunner`,
`CodeValueFlowBackfillStateMarker`) are locally redeclared with the identical
method set rather than imported, so every existing concrete implementation
still satisfies them with no new indirection. `DerefInt`/`DerefStringTrimmed`
were hoisted to `payloadcore` (alongside the existing `DerefBool`/
`DerefString`) because both this package and the root-staying
`code_function_summary_typed_decode.go` needed them. Every outward caller —
`cmd/reducer` (`canonical_graph_writers.go`,
`code_value_flow_stale_cleanup_wiring.go`, `value_flow_wiring.go`),
`internal/reducer` root (`defaults_handlers.go`,
`defaults_additive_domains_incident_code.go`,
`code_value_flow_stale_cleanup_runner.go`,
`value_flow_fixpoint_evidence_loader.go`, `supply_chain_impact_match.go`) —
was updated to the qualified `codetaint.` symbol in the same commit.
Measured against baseline `86daa9eee` on `feat/6061-codetaint` (based on
`feat/6061-generationcheck`): from `go/`, with `GOROOT` unset and `GOCACHE`
pointed at this worktree, `go build ./...`, `go vet ./...`,
`go test ./internal/reducer/... -count=1` (15 subpackages, codetaint's own
suite included), and `go test ./cmd/reducer ./internal/storage/postgres
./internal/query -count=1` each exited 0 on the branch. `git diff --check`
exited 0. Binary output was not compared and no such claim is made here.

No-Observability-Change: this move adds no queue domain, worker, lease,
graph or Postgres operation, runtime setting, metric instrument, metric
label, span, or status surface. The metric names, emission sites, and
structured-log messages listed under Telemetry above are unchanged; only the
package that owns the code moved.
