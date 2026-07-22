# AGENTS.md — internal/truth guidance for LLM assistants

## Read first

1. `go/internal/truth/README.md` — purpose, exported surface, invariants
2. `go/internal/truth/model.go` — `Layer`, `Contract`, `ParseLayer`; the
   entire surface fits in one file
3. `go/internal/reducer/registry.go` — how `Contract` is used during
   reducer domain registration
4. `go/internal/truth/deployment_tiers.go` — `DeploymentTruthTier`,
   `ClassifyDeploymentTruthTier`; every tier-emitting query surface
   (`trace_deployment_chain`, `supply_chain_impact`, service story) calls
   through this classifier
5. `docs/public/reference/deployment-truth-tiers.md` — the tier vocabulary
   contract: what qualifies per tier, the no-invented-tiers rule, and the
   legacy-reason-to-tier mapping

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

- **Add a deployment truth tier** — the vocabulary is closed
  (`docs/public/reference/deployment-truth-tiers.md`'s "No-invented-tiers
  rule"). Do not add a tier string without: a new constant in
  `deployment_tiers.go`, a `rank()` slot in strict descending evidence-
  strength order, an entry in `AllDeploymentTruthTiers`, and an update to
  that doc. Run `go test ./internal/truth -count=1`; tests
  `TestDeploymentTruthTierConstantsExhaustive` and
  `TestAllDeploymentTruthTiersOrder` cover the exhaustiveness and ordering
  gates.

- **Change `ClassifyDeploymentTruthTier`'s precedence** — every
  tier-emitting consumer (`go/internal/query/impact_trace_deployment_resources.go`,
  `supply_chain_impact`, service story) reads through this one function, so
  a precedence change is a cross-surface behavior change. Update
  `docs/public/reference/deployment-truth-tiers.md`'s "What qualifies (and
  what does not)" section in the same change, and re-run the golden-corpus
  gate's deployment-truth-tier pins (`testdata/golden/e2e-20repo-snapshot.json`,
  `trace_deployment_chain` shapes).

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
