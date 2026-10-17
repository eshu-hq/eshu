# AGENTS.md - cmd/fact-kind-registry guidance

## Read first

1. `README.md` - command purpose and boundaries.
2. `main.go` - generation pipeline, live-family cross-checks, and registry
   rendering.
3. `discover.go` - static `go/parser` discovery and the restricted literal
   grammar evaluator.
4. `evidence.go` - reducer-consumption evidence scan and kind-symbol
   learning.
5. `validate_contract.go` - contract guards (ownership, bindings,
   producers, dispositions, stale disclosures) and the legacy-spec
   derivation.
6. `validate.go` - registry validation (payload_schema containment,
   lifecycle marker semver, family metadata, and per-kind override checks).
7. `render_contract.go` / `render_catalog.go` - generated YAML, shards,
   catalog pages, output-set validation, and stale/missing/extra checks.
8. `go/internal/facts/contract/doc.go` - the declaration model contract.
9. `go/internal/facts/AGENTS.md` - generated artifact consumer guidance.

## Invariants

- The command is build-time only. Do not add runtime storage, graph, network, or
  telemetry dependencies.
- Discovery must stay parse-only (`go/parser`/`go/ast`/`go/token`). Never
  switch to `go/packages`, `go/types`, or any loader that can execute scanned
  package initialization.
- Generation must be deterministic byte-for-byte.
- Validation must compare declarations against live fact family helpers and
  live reducer evidence, not only against the previous generated artifact.
- Keep every generated output derived from the same parsed declarations.
- Diagnostics name the declaration file and line, the conflicting source
  location when relevant, and the corrective action (`ACTION:`).

## Common changes

- Adding a fact kind means extending the owning family's
  `contract.Declare` declaration (and `contract.Bind` declarations for new
  reducer consumption), running `scripts/generate-fact-kind-registry.sh`,
  and checking the verifier failure mode with focused tests. Never edit
  `specs/fact-kind-registry.v1.yaml` or the `contracts.d` shards by hand.

## Failure modes

- A new fact kind missing from its family declaration should fail
  `scripts/verify-fact-kind-registry.sh`.
- A stale, missing, or extra generated artifact should fail with a
  `stale`/`missing`/`extra` message naming the regeneration command.
- A binding whose symbol disappears or whose kinds lose reducer evidence
  should fail validation at the binding's file and line.

## What not to change without review

- Do not make the command infer lifecycle owner, reducer domain, projection
  hook, read surface, or truth profile silently. Those are explicit contract
  fields.
