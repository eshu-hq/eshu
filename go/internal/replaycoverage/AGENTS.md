# AGENTS — replaycoverage

Scoped rules for editing the C-1/C-8/C-9/C-13 replay coverage gate core. Load
`eshu-golden-corpus-rigor`, `eshu-diagnostic-rigor`, and `golang-engineering`.
Add `cypher-query-rigor` and `concurrency-deadlock-rigor` when touching the
depth-requirement derivation (retractable node types, projection/ordering,
crash) — its denominators come from the graph-write and reducer-drain surfaces
those skills own.

## Invariants

- **Registry enumeration stays centralized.** `EnumerateSupported` is the single place
  the supported set is derived. Add a registry source here, keyed
  `<kind>:<name>`, and cover it with a `surfaces_test.go` case. Never widen the
  surface-inventory scope past the `implemented` lane — other lanes do not assert
  production readiness, so requiring a scenario for them would over-claim.
- **Compose, do not fork.** Collectors, API/MCP read surfaces, CLI read surfaces,
  and claims come from the generated `capabilitycatalog` / `facts` registries and
  the B-12 snapshot that sibling gates already own. Do not re-enumerate live code
  here.
- **Existence, not greenness.** `Resolver` proves a scenario artifact exists; the
  scenario's greenness is proven by the gate named in `proof_gate`. Do not make
  this gate run pipelines, hit a backend, or need credentials.
- **No silent green.** A missing manifest is an empty manifest (everything
  uncovered), never an error that skips the gate. Blank fields, invalid artifact
  kinds, invalid scenario types, duplicate surface+scenario_type pairs,
  requirements that drop baseline, and covered/required+exempt conflicts are
  hard load errors.
- **Two severity tiers, one knob.** `Blocking=false` is local advisory mode; CI
  passes the single blocking flag for **baseline (breadth)** coverage. The
  C-8/C-13 **depth** classes are advisory-first: `isBlockingScenarioType` keeps
  every non-baseline finding advisory even under `-blocking`, so the gate lists
  the missing surface/scenario_type pairs (the C-14 worklist) without failing CI.
  Keep severity keyed on `Blocking` × scenario_type — do not add per-finding
  `Required` overrides. Flip a depth class to blocking only when its backlog has
  burned down (a deliberate follow-up, not an inline edit).
- **Depth requirements are derived and lockstep, not hand-listed.**
  `DeriveRequirements` derives the depth class per applicable surface — fault per
  collector, cost per projection, ordering per shared-conflict-key projection
  (≥2 projection hooks), delta per retractable node type and static retractable
  edge type, crash for the drain. The retractable node and edge types live in
  `specs/replay-depth-requirements.v1.yaml` and MUST stay byte-equal to
  `cypher.RetractableNodeEntityLabels()` and `cypher.RetractableEdgeTypes()`
  (lockstep tests enforce both). Never hand-maintain a parallel denominator that
  can silently drift from the code that does the retraction — that reintroduces
  the #4186 blindness one layer up.
- **The language-parser scoreboard does not gate.** `BuildLanguageScoreboard`
  (C-11, #4364) is a visibility-only artifact over the
  `language-feature-parity-ledger`; it is deliberately kept out of
  `EnumerateSupported` and `Findings`. A ledger language is satisfied either by
  a corpus-only `language_exemptions` row or by an exact
  `parser:<language>` baseline `parser_fixture` coverage row in the replay
  manifest. Tree-sitter languages *can* have a fixture (that is C-12 / #4365), so
  a not-yet-covered language is an honest `uncovered` row on the scoreboard,
  never a manifest `exemption` and never a blocking finding. Only mark a language
  exempt in `language_exemptions` when its files genuinely appear in the staged
  golden-corpus repos.
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
