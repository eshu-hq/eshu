# collector-git

## Purpose

`collector-git` is the Git collection service binary. It runs repository sync,
snapshot parsing, fact emission, and collector service wiring for the Git-owned
ingestion path.

## Ownership Boundary

The command owns process startup, configuration loading, telemetry setup,
service construction, and signal-aware shutdown. Collection and parsing logic
stays in internal collector/parser packages; durable storage stays behind
storage ports; graph truth stays with projector and reducer runtimes.

## Exported Surface

See `doc.go`. This is a command package; external callers use the binary, not a
library API. Tests exercise service construction and command behavior.

## Telemetry

Startup wires the runtime telemetry used by the collector path. Package-level
metrics and spans are emitted by internal services, not by README-level command
inventory.

## Gotchas / Invariants

- Rebuild the binary before local runtime validation.
- Keep command flags and environment variables aligned with public CLI/runtime
  docs when they change.
- Do not move graph writes into this command; Git intake commits facts and work.

## Focused Tests

```bash
cd go
go test ./cmd/collector-git -count=1
go run ./cmd/eshu docs verify ../go/cmd/collector-git --limit 1000 \
  --fail-on contradicted,missing_evidence
```

## Related Docs

- `docs/public/deployment/service-runtimes.md`
- `docs/public/reference/local-testing.md`
- `docs/public/architecture.md`
