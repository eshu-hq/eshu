# internal/reducer/code/taint

## Purpose

Materializes the `code_taint_evidence` and `code_interproc_evidence` reducer
domains: value-flow taint findings attached to their Function as
`CodeTaintEvidence` graph nodes, and cross-function value-flow findings
projected as `TAINT_FLOWS_TO` edges between Function nodes (issue #6061).

It bundles both families into one package instead of two siblings
(`taint`/`interproc`) because the two subjects are interleaved at the
file level, not the dependency level, so a clean split does not exist yet.
There is no true import cycle between the families: no
`code_interproc_*.go` file references any `CodeTaintEvidence*` symbol (the
only hit is a doc comment cross-reference in
`code/taint/interproc_evidence_materialization.go`). The sibling `value`
package's value-flow fixpoint solver (`code/value/fixpoint_evidence_loader.go`,
a different family, not this one) imports this package for its evidence
writer/ledger/uid-namespace surface — a one-directional leaf-to-leaf
dependency, not a cycle, since this package never imports `value` back.
The value-flow stale cleanup runner (`code/value/cleanup/runner.go`)
reaching into this package's exported symbols is another one-directional
leaf-to-leaf dependency.
The real obstacle is that
`code/taint/evidence_typed_decode.go` — a taint-prefixed file — implements
the decode/quarantine functions for BOTH fact kinds:
`DecodeInterprocEvidenceInput` and
`ExtractInterprocEvidenceRowsWithQuarantine` are interproc functions that
live in that file, and `code/taint/interproc_evidence_materialization.go` (the
would-be `codeinterproc` handler) calls
`ExtractInterprocEvidenceRowsWithQuarantine` from it. Splitting the two
families into separate packages today would require first splitting
`code/taint/evidence_typed_decode.go` so each family's decode/quarantine
logic lives under its own name; bundling both here instead keeps the move
mechanical. A later split is possible once someone separates those
functions out of that one file.

## Ownership boundary

**Owns:** `EvidenceHandler` and
`InterprocEvidenceHandler` (the two reducer intent
handlers), their loader/writer ports, the typed-decode + quarantine seam for
both fact kinds, the row/edge projection functions, and the projected-node /
projected-edge ledgers and their startup backfillers.

**Does not own:** the value-flow fixpoint solver
(`FixpointEvidenceLoader`/`FixpointEvidenceProjector` in
the sibling `value` package) — that is a different family (durable cross-repo summaries
solved into a `Program`) that happens to produce `InterprocEvidenceInput`
rows and calls through this package's `ExtractInterprocFixpointEvidenceRows`
and `SourceUIDsFromRows`/`UnresolvedInterprocEndpointCount` exports. Also
does not own `code/value/cleanup/runner.go` — the
generation-scoped stale-evidence sweep that calls through this package's
writer/ledger interfaces and `EvidenceSource()`/
`InterprocEvidenceSource()` accessors.

## Exported surface

| symbol | what it is |
|---|---|
| `EvidenceHandler` / `InterprocEvidenceHandler` | the two reducer intent handlers |
| `EvidenceLoader` / `EvidenceWriter` | taint loader/writer ports |
| `InterprocEvidenceLoader` / `InterprocEvidenceFactLoader` / `InterprocEvidenceWriter` | interproc loader/writer ports (two loader shapes: typed-input for the fixpoint solver, raw-envelope for the materialization handler) |
| `EvidenceInput` / `InterprocEvidenceInput` | the decoded per-finding row shapes |
| `ExtractEvidenceRows` / `ExtractInterprocEvidenceRows` / `ExtractInterprocFixpointEvidenceRows` | pure row/edge projection (fixpoint uses a separate uid namespace so it cannot clobber direct fact rows) |
| `ExtractEvidenceRowsWithQuarantine` / `ExtractInterprocEvidenceRowsWithQuarantine` | the production decode+quarantine path the handlers call |
| `DecodeEvidenceInput` / `DecodeInterprocEvidenceInput` | the single-envelope typed decode, exported for the root's shared codedataflow benchmark |
| `ProjectedNodeLedger` / `InterprocProjectedEdgeLedger` | anchored-delete-by-uid ledgers (issue #4893) |
| `ProjectedNodeBackfiller` / `InterprocProjectedEdgeBackfiller` | one-time startup ledger backfills |
| `EvidenceSource()` / `InterprocEvidenceSource()` / `InterprocFixpointEvidenceSource()` | evidence-source tag accessors, used by root's stale-cleanup runner and `cmd/reducer` wiring |
| `EvidenceDomainDefinition()` / `InterprocEvidenceDomainDefinition()` | `DomainDefinition` constructors for root's additive-domain registration |
| `SourceUIDsFromRows()` / `UnresolvedInterprocEndpointCount()` | shared with the root fixpoint loader for its own ledger recording and structured logging |
| `GraphQueryRunner` / `BackfillStateMarker` | locally-declared ports (see Dependencies) |

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

Two interfaces this package's backfillers need are **locally redeclared** in
`graph_ports.go` rather than imported. `GraphQueryRunner` (the graph read
port) is owned by the reducer root and shared by five other root families,
plus the sibling `value` package's own separate local redeclaration;
importing the root to reach it would violate the "a family never imports the
reducer root" rule. `BackfillStateMarker` (the durable
per-source completion marker) mirrors `value.BackfillStateMarker`, and
`value` imports this package, so importing it back would be a cycle. Go interfaces are satisfied structurally, so the same concrete
implementations `cmd/reducer` wires into root's other families also satisfy
these local declarations with no logic duplicated. `derefFloat64` is
similarly kept as a small local unexported copy (root's version,
`match.go`, is real logic for the unrelated
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
- **`GraphQueryRunner` and `BackfillStateMarker` are
  intentionally re-declared here, not imported.** Do not "fix" either with
  an import — see Dependencies above (a root import for the first, an import
  cycle through `value` for the second). If a future move puts either in
  a leaf package both sides can import, replace that declaration with the
  import.
- **`ExtractInterprocFixpointEvidenceRows` uses a separate uid
  namespace from `ExtractInterprocEvidenceRows`** so a fixpoint-solved
  edge can never collide with (and silently overwrite) a direct-fact edge in
  the graph writer's `MERGE`. Do not unify the two uid derivations.
- **`DecodeEvidenceInput`/`DecodeInterprocEvidenceInput` are
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
interfaces the backfillers need (`GraphQueryRunner`,
`BackfillStateMarker`, both root-owned at the time; the marker
later moved to `value` under #6609) are locally redeclared with the identical
method set rather than imported, so every existing concrete implementation
still satisfies them with no new indirection. `DerefInt`/`DerefStringTrimmed`
were hoisted to `payloadcore` (alongside the existing `DerefBool`/
`DerefString`) because both this package and the function-summary decoder
(then the root's `code_function_summary_typed_decode.go`, now
`code/function/summary/decode.go`) needed them. Every outward caller —
`cmd/reducer` (`canonical_graph_writers.go`,
`code_value_flow_stale_cleanup_wiring.go`, `value_flow_wiring.go`),
`internal/reducer` root (`defaults_handlers.go`,
`defaults_additive_domains_incident_code.go`,
`code_value_flow_stale_cleanup_runner.go`,
`code/value/fixpoint_evidence_loader.go`, `match.go`) —
was updated to the qualified `taint.` symbol in the same commit.
Measured against baseline `86daa9eee` on `feat/6061-codetaint` (based on
`feat/6061-generationcheck`): from `go/`, with `GOROOT` unset and `GOCACHE`
pointed at this worktree, `go build ./...`, `go vet ./...`,
`go test ./internal/reducer/... -count=1` (15 subpackages, taint's own
suite included), and `go test ./cmd/reducer ./internal/storage/postgres
./internal/query -count=1` each exited 0 on the branch. `git diff --check`
exited 0. Binary output was not compared and no such claim is made here.

No-Observability-Change: this move adds no queue domain, worker, lease,
graph or Postgres operation, runtime setting, metric instrument, metric
label, span, or status surface. The metric names, emission sites, and
structured-log messages listed under Telemetry above are unchanged; only the
package that owns the code moved.

**Exported-stutter drop addendum (issue #6061, PR #6645):** the package clause
changed from `codetaint` to `taint` in the move PR, but every exported
identifier still repeated the `Code`/`CodeTaint`/`CodeInterproc` package-name
stutter (for example `taint.CodeTaintEvidenceWriter` -> `taint.EvidenceWriter`,
`taint.CodeInterprocEvidenceWriter` -> `taint.InterprocEvidenceWriter`), which
`docs/internal/naming.md`'s no-package-name-stutter rule bans and its
move-leaves-names-better rule requires a move to fix.
This addendum drops that stutter, following the sibling `code/value` package's
own package-clause rename addendum precedent above it in this file's history.
The `Code`/`CodeTaint` prefix was dropped from every taint-family exported
name (`CodeTaintEvidenceInput` -> `EvidenceInput`,
`CodeTaintEvidenceMaterializationHandler` -> `EvidenceHandler`,
`CodeTaintEvidenceProjectedNodeLedger` -> `ProjectedNodeLedger`,
`CodeTaintEvidenceProjectedNodeBackfiller` -> `ProjectedNodeBackfiller`,
`CodeTaintEvidenceProjectedNodeBackfillReader` -> `ProjectedNodeBackfillReader`,
`DecodeCodeTaintEvidenceInput` -> `DecodeEvidenceInput`,
`ExtractCodeTaintEvidenceRows(WithQuarantine)` ->
`ExtractEvidenceRows(WithQuarantine)`, `CodeTaintEvidenceSource` ->
`EvidenceSource`, `CodeTaintEvidenceDomainDefinition` ->
`EvidenceDomainDefinition`); the `Code` prefix was dropped from every
`CodeInterproc*` name while keeping the `Interproc` infix
(`CodeInterprocEvidenceInput` -> `InterprocEvidenceInput`,
`CodeInterprocEvidenceLoader`/`FactLoader`/`Writer` ->
`InterprocEvidenceLoader`/`FactLoader`/`Writer`,
`CodeInterprocEvidenceMaterializationHandler` -> `InterprocEvidenceHandler`,
`CodeInterprocFixpointEvidenceSource` -> `InterprocFixpointEvidenceSource`,
`CodeInterprocProjectedEdgeLedger`/`Backfiller`/`BackfillReader` ->
`InterprocProjectedEdgeLedger`/`Backfiller`/`BackfillReader`,
`DecodeCodeInterprocEvidenceInput` -> `DecodeInterprocEvidenceInput`,
`ExtractCodeInterprocEvidenceRows(WithQuarantine)` ->
`ExtractInterprocEvidenceRows(WithQuarantine)`,
`ExtractCodeInterprocFixpointEvidenceRows` ->
`ExtractInterprocFixpointEvidenceRows`,
`UnresolvedCodeInterprocEndpointCount` -> `UnresolvedInterprocEndpointCount`);
and `ProjectedTaintEdgeRow`/`ProjectedTaintNodeRow` dropped to
`ProjectedEdgeRow`/`ProjectedNodeRow`, matching the un-prefixed backfill row
shapes already used elsewhere. The package's own locally-redeclared
`CodeValueFlowBackfillStateMarker` interface (`graph_ports.go`) dropped to
`BackfillStateMarker`, matching the sibling `value.BackfillStateMarker` it
mirrors; the reducer root's *unrelated* `CodeValueFlowBackfillStateMarker`
type alias (`compat_projection.go`, aliasing `value.BackfillStateMarker`
itself, not this package's local interface) keeps its spelling — it was never
this package's identifier to rename. `SourceUIDsFromRows` and
`GraphQueryRunner` had no stutter and are unchanged. No wire string, evidence
source, domain, entity key, telemetry name, or behavior changed — only Go
identifiers. Every outward caller — `cmd/reducer` (`canonical_graph_writers.go`,
`code_value_flow_stale_cleanup_wiring.go`, `value_flow_wiring.go`),
`internal/reducer` root (`defaults_handlers.go` field TYPES only, field names
unchanged; `defaults_additive_domains_incident_code.go`,
`code_value_flow_stale_cleanup_runner.go`), and `code/value`
(`fixpoint_evidence_loader.go`) — was updated to the new qualified `taint.`
spelling in the same commit; test doubles and doc comments across the repo
that named the old spelling were updated alongside. Measured from `go/`, with
`GOROOT` unset: `go build ./...`, `go vet ./internal/reducer/...
./cmd/reducer/... ./internal/storage/...`, and `go test
./internal/reducer/... ./cmd/reducer/... ./internal/storage/postgres/...
./internal/storage/cypher/... ./internal/replay/...
./internal/backendconformance/... -count=1` all exited 0 on this branch.

No-Observability-Change: this rename adds no queue domain, worker, lease,
graph or Postgres operation, runtime setting, metric instrument, metric
label, span, or status surface. Every string value named under Telemetry
above — evidence-source tags, structured-log messages and fields, metric
names — is byte-identical; only Go identifiers changed.
