# AGENTS.md - prefilter

## Read first

1. `doc.go` for the soundness contract.
2. `README.md` for the derivation and the proof list.
3. Parent `../AGENTS.md` for the `repositoryartifacts` invariants.

## Invariants

- Soundness is the contract: gate false must imply no match, for all inputs.
  A change that makes a gate stricter must prove it against Go's own matcher,
  not against an expectation written from the change.
- Derive from `Regexp.String()` only. Do not add token lists, per-pattern
  tables, or overrides. If a pattern cannot be gated soundly, leave it
  ungated.
- Fold with `unicode.SimpleFold` orbits. Never use `strings.ToLower` or an
  ASCII-only fold for a `(?i)` literal.
- Standard library only. No logging, telemetry, I/O or package state.

## Verification

`go test -race ./internal/query/repositoryartifacts/...` runs the property,
mutation and consumer equality suites. Before changing the derivation, run
`go test -fuzz FuzzGateIsSound -fuzztime 60s ./internal/query/repositoryartifacts/prefilter`.
Run `scripts/verify-package-docs.sh` when this package changes.
