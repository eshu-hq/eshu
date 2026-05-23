# AGENTS.md - internal/parser

`internal/parser` owns registry dispatch, tree-sitter runtime caching,
repository pre-scan orchestration, language wrapper boundaries, and SCIP index
parsing. Its README and `doc.go` are the package contract; keep this file to
agent-only guardrails.

## Read First

1. `README.md` and `doc.go` for the current parser contract.
2. `registry.go`, `engine.go`, and `runtime.go` for dispatch, path
   normalization, worker merging, and runtime reuse.
3. `scip_support.go` before touching SCIP.
4. The child language README and `AGENTS.md` before touching a language-owned
   adapter.
5. `go/internal/content/shape` before adding entity, relationship, or metadata
   keys.

## Mandatory Guardrails

- Parser output MUST be deterministic for identical source bytes. Sort every
  map-derived row or pre-scan result before returning it.
- Parser payload shape is downstream truth input. New or renamed keys MUST move
  with fixtures, content shaping, fact/content contracts, and graph/query docs
  when those surfaces consume the key.
- `Registry` stays immutable after construction. `NewRegistry` rejects
  duplicate parser keys, extensions, and exact names; `DefaultRegistry` panics
  if defaults become invalid.
- Share `NewRuntime()` across engines and parse calls. Do not create a runtime
  per file, worker, or goroutine.
- Parent wrappers own registry lookup, absolute path normalization, runtime
  parser construction, and content metadata attachment. Child parser packages
  MUST NOT import the parent package.
- Parser code MUST NOT import collector, projector, reducer, storage, query, or
  telemetry packages unless the architecture owner explicitly moves the
  boundary.
- Do not return partial payloads with nil or unrelated errors after parse
  failure. Do not add timestamps, random values, process-local counters, or
  machine-local filesystem state to payloads.

## Change Scope

- New language: add a unique `Definition`, dispatch case, parser function,
  pre-scan path when needed, fixtures, package docs, and tests.
- New SCIP language: update `scip_support.go`, language priority, binary lookup
  expectations, and SCIP fixture tests.
- Pre-scan changes require deterministic merge tests, especially for worker
  output order.
- Changing production-covered extension ownership, SCIP priority, registry
  mutability, or runtime concurrency requires architecture-owner approval and
  performance/concurrency evidence.

## Required Proof

- Code changes use TDD first.
- Run focused adapter tests for the touched language plus
  `go test ./internal/parser -count=1`.
- If payload shape changes, also run the affected collector, content, projector,
  reducer, and query tests.
- Docs-only changes require `git diff --check`, `scripts/verify-package-docs.sh`,
  and `go run ./cmd/eshu docs verify ../go/internal/parser --limit 1400 --fail-on contradicted,missing_evidence`.
- Parser throughput changes require before/after timing and an observability
  marker naming the existing parse-duration or failure signal, or the new signal
  added with the change.
