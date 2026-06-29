# AGENTS — replay-coverage-gate

Scoped rules for the C-1 coverage gate command. Load `eshu-golden-corpus-rigor`,
`eshu-diagnostic-rigor`, and `golang-engineering`. The reconciliation logic lives
in `internal/replaycoverage`; this command only loads inputs, runs the gate,
writes the report, and sets the exit code.

## Rules

- **Keep it thin.** New reconciliation behavior belongs in
  `internal/replaycoverage` with a focused unit test, not in `run`. This command
  is exercised by `main_test.go` through the real loaders.
- **Blocking is the shipped CI default.** `-blocking` still defaults to false for
  local exploratory runs, but CI must pass `--blocking`; C-2..C-6 have burned
  the gaps down and new supported surfaces must land with replay scenarios.
- **Always write the report.** The coverage report is written before the blocking
  exit check, so the C-7 dashboard always has data even on a failing run. Keep it
  that way.
- **Compose, don't fork.** Inputs come from `capabilitycatalog.LoadSurfaceInventory`
  (embedded), `facts.FactKindRegistry`, `capabilitycatalog.LoadMatrix`, and the
  parser ledger loader. Do not re-enumerate live code or duplicate the
  capability-inventory gate.
- **No credentials, no Docker, no backend.** This gate is static: registries +
  manifest + on-disk artifact existence + the committed snapshot. If a change
  here needs a running service, it belongs in a different gate.

## Verify

```bash
cd go && go test ./cmd/replay-coverage-gate/ ./internal/replaycoverage/ -count=1
bash scripts/test-verify-replay-coverage-gate.sh
```
