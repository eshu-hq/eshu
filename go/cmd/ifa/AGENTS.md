# AGENTS.md - cmd/ifa guidance

## Read first

1. `README.md` - command purpose and current P0 behavior.
2. `main.go` - CLI shell.
3. `go/internal/ifa/AGENTS.md` - library contract.

## Invariants

- The command is a thin shell over `internal/ifa`; keep conformance logic in the
  library package.
- Do not add collector, parser, graph, database, or network dependencies to the
  P0 command.
- Keep output deterministic so future `make prove` integration can compare it.

## Verification

```bash
cd go && go test ./cmd/ifa -count=1
```
