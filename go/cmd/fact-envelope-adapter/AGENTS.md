# AGENTS.md - cmd/fact-envelope-adapter guidance

## Read first

1. `README.md` - command purpose and generated output boundary.
2. `main.go` - generator source and deterministic rendering.
3. `go/internal/factenvelope/AGENTS.md` - generated artifact consumer guidance.

## Invariants

- The command is build-time only. Do not add storage, graph, network, queue, or
  telemetry dependencies.
- Generated output must be deterministic byte-for-byte.
- Every field copied between envelope shapes must be visible in the generator
  source rather than hand-written in extensionhost, reducer, or projector code.

## Common changes

- When adding an envelope field, update the generator, run `go generate
  ./internal/factenvelope`, and keep the command's stale-output test green.

## Failure modes

- A stale generated file means a caller may silently drop a new envelope field.
- A generator change that renames public SDK JSON fields is a protocol change
  and must be handled in `sdk/go/collector`, not here.

## What not to change without review

- Do not make this command infer payload validation or schema-version policy
  from live data. It owns only adapter source generation.
