# AGENTS.md — internal/facts/contract

Scoped instructions for this package. Read `README.md` and `doc.go`
first; the root `AGENTS.md` still applies.

## The one rule that matters

This package MUST stay dependency-neutral: no imports from
`go/internal/facts`, any nested fact family, or any collector,
reducer, projector, query, or storage package. Nested families
cannot import the facts root (see `go/internal/facts/AGENTS.md`), and
reducer packages declare bindings here, so any upward import either
cycles or couples declaration sites that must stay independent.

## Invariants

- **`Declare` and `Bind` are identity functions.** Never add
  normalization, registration, or side effects to them. Declarations
  are discovered by parsing source, not by running initializers; a
  registry-by-init design would require executing the scanned
  packages, which the generator must never do.
- **The model carries metadata only.** Adding a field here means the
  generator's literal scanner, validators, shard renderers, and every
  declaration may need the matching change in the same slice. Keep
  field spellings aligned with the legacy registry semantics they
  mirror.
- **Wire spellings are contract.** `Disposition` and `AuthorityMode`
  string values land in generated shards and the catalog; renaming one
  is a contract change, pinned by `contract_test.go`.
- **No I/O, no state, no telemetry.** This package is on no runtime
  path. Validation belongs in `go/cmd/fact-kind-registry`, not here.

## Change guidance

After changing the model, run the generator
(`bash scripts/generate-fact-kind-registry.sh`) from the repo root and
its verification (`bash scripts/verify-fact-kind-registry.sh`), then
the package tests from `go/` (`go test ./internal/facts/contract/`).
