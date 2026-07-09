# AGENTS.md - cmd/ifa guidance

## Read first

1. `README.md` - command purpose and subcommand behavior.
2. `main.go` - subcommand dispatch and the P0 `-version` shell.
3. `coverage.go`, `expectations.go` - P1 subcommand wrappers.
4. `go/internal/ifa/AGENTS.md` - library contract.

## Invariants

- The command is a thin shell over `internal/ifa`; keep conformance,
  derivation, and coverage-reconciliation logic in the library package. New
  subcommands parse flags, load inputs from disk, call into `internal/ifa`, and
  render output — nothing more.
- Do not add collector, parser, graph, or database dependencies to this
  command.
- Keep output deterministic so `make prove`-style integration and CI can
  compare it byte-for-byte.
- `ifa coverage` must not hard-fail on the `ifa-contract-layer` gate's own
  "not blocking" proof-gate finding; that gate is deliberately kept advisory
  and the finding is surfaced through the goldengate.Report instead. Do not
  copy `cmd/replay-coverage-gate/main.go`'s unconditional proof-gate hard-fail
  here without re-deciding that gate's blocking status first.

## Verification

```bash
cd go && go test ./cmd/ifa -count=1
```
