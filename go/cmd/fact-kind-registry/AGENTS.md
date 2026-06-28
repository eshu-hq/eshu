# AGENTS.md - cmd/fact-kind-registry guidance

## Read first

1. `README.md` - command purpose and boundaries.
2. `main.go` - generator and validation logic.
3. `specs/fact-kind-registry.v1.yaml` - source-of-truth registry input.
4. `go/internal/facts/AGENTS.md` - generated artifact consumer guidance.

## Invariants

- The command is build-time only. Do not add runtime storage, graph, network, or
  telemetry dependencies.
- Generation must be deterministic byte-for-byte.
- Validation must compare the spec against live fact family helpers, not only
  against the previous generated artifact.
- Keep generated Go and Markdown derived from the same parsed registry.

## Common changes

- Adding a fact kind means updating `specs/fact-kind-registry.v1.yaml`, running
  `scripts/generate-fact-kind-registry.sh`, and checking the verifier failure
  mode with focused tests.

## Failure modes

- A new fact kind missing from the YAML should fail
  `scripts/verify-fact-kind-registry.sh`.
- A stale generated artifact should fail with a `stale` message.

## What not to change without review

- Do not make the command infer lifecycle owner, reducer domain, projection
  hook, read surface, or truth profile silently. Those are explicit contract
  fields.
