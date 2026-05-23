# AGENTS.md — internal/truth guidance for LLM assistants

## Read first

1. `go/internal/truth/README.md` — purpose, exported surface, invariants
2. `go/internal/truth/model.go` — `Layer`, `Contract`, `ParseLayer`; the
   entire surface fits in one file
3. `go/internal/reducer/registry.go` — how `Contract` is used during
   reducer domain registration

## Invariants this package enforces

- **`LayerCanonicalAsset` is output-only** — `Contract.Validate` at
  `model.go:61` explicitly rejects `LayerCanonicalAsset` in `SourceLayers`.
  Never add it as a source layer in a reducer registration.
- **Non-empty, duplicate-free `SourceLayers`** — `model.go:53` and `:57`
  enforce both. A `Contract` with an empty or duplicate `SourceLayers` slice
  fails validation at domain registration.
- **`ParseLayer` trims whitespace** — `model.go:30` trims before validating.
  Raw layer strings from config or wire formats must go through `ParseLayer`,
  not direct `Layer(raw)` casts.

## Common changes and how to scope them

- **Add a new truth layer** — add a constant in `model.go`, extend the
  `Validate` switch (`model.go:39`), and update `ParseLayer`. Then update
  every reducer domain that declares `SourceLayers`. Run
  `go test ./internal/truth ./internal/reducer -count=1`. Tests
  TestParseLayerRejectsUnknownValue and TestParseLayerAcceptsKnownValues
  cover the parser gate.

- **Add a `Contract` method** — keep it pure value logic with no I/O.
  Add a test in `model_test.go` before implementing.

## Failure modes and how to debug

- Symptom: reducer domain registration panics or returns an error about
  `canonical_asset` in source layers → cause: caller passed
  `LayerCanonicalAsset` in `Contract.SourceLayers` → fix: use only
  `LayerSourceDeclaration`, `LayerAppliedDeclaration`, or
  `LayerObservedResource` as source layers.

- Symptom: `ParseLayer` returns `unknown truth layer "..."` → cause: raw
  value does not match any of the four known layer strings → fix: verify
  the config or wire value against the constants in `model.go`.

## Anti-patterns specific to this package

- **Casting raw strings directly to `Layer`** — always use `ParseLayer` for
  external input. Direct casts bypass whitespace trimming and the known-set
  check.
- **Adding runtime state** — this package must remain a pure value library.
  No init functions, no global vars, no I/O.
