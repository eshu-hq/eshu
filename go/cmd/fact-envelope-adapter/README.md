# fact-envelope-adapter

## Purpose
Generates the shared fact-envelope adapter used by extensionhost, reducer, and
projector code. The generated file maps public collector SDK facts into durable
internal envelopes and adapts durable envelopes into factschema decode
envelopes.

## Ownership boundary
This command owns only build-time generation and stale-output verification. It
does not own public SDK schema generation, payload validation, fact storage, or
graph/reducer behavior.

## Exported surface
This is a command package. Run it with `go generate ./internal/factenvelope` or
directly with `go run ./cmd/fact-envelope-adapter -check`.

## Dependencies
The command uses only the Go standard library. The generated artifact imports
`internal/facts`, `sdk/go/collector`, and `sdk/go/factschema`.

## Telemetry
No runtime telemetry is emitted. The command runs in local and CI generation
gates only.

## Gotchas / invariants
The SDK fact's wire field names intentionally differ from durable internal
field names. Keep that translation explicit in the generated source, and do not
rename SDK JSON tags from this command.

## Related docs
- `go/internal/factenvelope/README.md`
- `docs/internal/design/contract-system-v1.md`
