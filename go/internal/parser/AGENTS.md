# AGENTS.md - internal/parser

`internal/parser` owns deterministic source parsing, parser registry dispatch,
pre-scan behavior, SCIP support, and language adapter boundaries. Parser output
feeds durable facts; nondeterminism here corrupts downstream truth.

## Read First

1. `README.md` - pipeline position, registered languages, SCIP path, exported
   surface, and invariants.
2. `registry.go` - `Registry`, `Definition`, defaults, and lookup behavior.
3. `engine.go` - `Engine`, `ParsePath`, pre-scan workers, and dispatch.
4. `runtime.go` - shared tree-sitter runtime and grammar cache.
5. `scip_support.go` when touching SCIP.
6. Language-owned README before editing an extracted adapter or helper package.
7. `go/internal/content/shape` before emitting a new entity key.
8. `go/internal/telemetry/instruments.go` before adding parse metrics.

## Mandatory Invariants

- Parser output MUST be deterministic for the same source bytes.
- New entity keys, relationship keys, or metadata fields MUST be consumed by
  `internal/content/shape`, backed by fixtures, and reflected in fact/content
  contracts in the same branch.
- `Registry` MUST remain immutable after construction. Lookup methods return
  cloned definitions.
- `NewRegistry` MUST reject duplicate parser keys, extensions, and exact names.
- `DefaultRegistry` MUST panic on invalid default definitions.
- `NewRuntime()` SHOULD be shared across engines and parse calls. Do not create
  a runtime per file or goroutine.
- `ParsePath` normalizes repo and file paths to absolute paths before parsing.

## Change Routing

- New language adapter: add a unique `Definition`, parser function, dispatch
  case, pre-scan case if needed, fixtures, README update, and tests.
- New entity key: update adapter output, collector snapshot buckets when
  materialized, `shape.Materialize`, projector label map when graph-projected,
  and fixture assertions.
- New SCIP language: update `scip_support.go`, language priority, binary
  lookup expectations, and SCIP fixture tests.
- Pre-scan change: update the language pre-scan function, sort results, and add
  determinism tests.

## Anti-Patterns

- Do not import collector, projector, storage, or query packages from parser.
- Do not let child parser packages import the parent parser package.
- Do not emit entity keys that `shape.Materialize` ignores.
- Do not iterate maps directly into output. Collect, sort, then emit.
- Do not return partial output with nil or unrelated errors when parsing fails.
- Do not add timestamps, random values, process-local counters, or filesystem
  machine state to parse payloads.

## Do Not Change Without A Design Record

- Existing production-covered extension assignments in `defaultDefinitions`.
- SCIP language priority for mixed-language repositories.
- Registry mutability or shared runtime concurrency model.

## Required Proof

- Run focused adapter tests for the touched language.
- Run `go test ./internal/parser -count=1`.
- Run related collector/content/projector tests when parser output shape changes.
- Run `go run ./cmd/eshu docs verify ../go/internal/parser --limit 1200 --fail-on contradicted,missing_evidence`
  for docs changes.
- Parser performance changes require before/after timing and observability
  evidence.
