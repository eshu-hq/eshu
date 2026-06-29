# AGENTS — replaycoverage

Scoped rules for editing the C-1 replay coverage gate core. Load
`eshu-golden-corpus-rigor`, `eshu-diagnostic-rigor`, and `golang-engineering`.

## Invariants

- **Four registries, one enumeration.** `EnumerateSupported` is the single place
  the supported set is derived. Add a registry source here, keyed
  `<kind>:<name>`, and cover it with a `surfaces_test.go` case. Never widen the
  surface-inventory scope past the `implemented` lane — other lanes do not assert
  production readiness, so requiring a scenario for them would over-claim.
- **Compose, do not fork.** Collectors, read surfaces, and claims come from the
  generated `capabilitycatalog` / `facts` registries that the capability-inventory
  drift gate already owns. Do not re-enumerate live code here.
- **Existence, not greenness.** `Resolver` proves a scenario artifact exists; the
  scenario's greenness is proven by the gate named in `proof_gate`. Do not make
  this gate run pipelines, hit a backend, or need credentials.
- **No silent green.** A missing manifest is an empty manifest (everything
  uncovered), never an error that skips the gate. Blank fields, invalid scenario
  types, duplicate surfaces, and covered+exempt conflicts are hard load errors.
- **Advisory by default.** `Blocking=false` is the shipped state. The single
  blocking flag is the only knob that turns gaps into failures. Keep that the
  only severity control — do not add per-finding `Required` overrides.
- **Determinism.** Enumeration sorts by registry then key; the report and stale
  list are sorted; `MarshalReport` is byte-stable with a trailing newline. No
  timestamps or wall-clock in the artifact.

## When the manifest changes

The manifest is the curated lockstep contract. A new implemented collector,
parser, read surface, or claim with no manifest entry is *meant* to show up as
uncovered — that is the keystone working. Do not paper over a real gap by adding
an exemption without a reason an operator would accept.

## Tests

`go test ./internal/replaycoverage/ -count=1`. Every new branch needs a focused
test, and negative tests must fail when the production assertion is removed.
