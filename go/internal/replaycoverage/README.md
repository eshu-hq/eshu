# replaycoverage

`replaycoverage` is the assertion core of the **C-1/C-8/C-9/C-10/C-13 replay
coverage manifest + lockstep gate**
([#4173](https://github.com/eshu-hq/eshu/issues/4173),
[#4187](https://github.com/eshu-hq/eshu/issues/4187),
[#4188](https://github.com/eshu-hq/eshu/issues/4188),
[#4189](https://github.com/eshu-hq/eshu/issues/4189),
[#4366](https://github.com/eshu-hq/eshu/issues/4366), epic
[#4172](https://github.com/eshu-hq/eshu/issues/4172)). It answers one question:
**does every surface and required scenario-depth class Eshu claims to support
have a green, credential-free, Docker-free replay scenario — and will CI notice
when a new one doesn't?**

It is the typed, unit-tested logic; the orchestration that loads the registries,
writes the report, and sets the exit code lives in
[`cmd/replay-coverage-gate`](../../cmd/replay-coverage-gate).

## What it reconciles

| Required surface (source of truth) | Coverage key | Baseline artifact kind |
| --- | --- | --- |
| surface-inventory implemented-lane collectors | `collector:<name>` | cassette |
| fact-kind registry read surfaces | `read_surface:<surface>` | api/mcp golden |
| B-12 CLI query shapes | `cli_surface:<command>` | cli golden |
| parser-backing ledger parsers | `parser:<name>` | parser fixture |
| capability-matrix positive claims | `capability:<id>` | capability claim |
| product-claims public ledger rows | `product_claim:<id>` | product claim |
| authorization-catalog live permission families | `authz_family:<family>:<mode>` | authz scoped route |

`EnumerateSupported` flattens the seven registries into a deterministic
`SupportedSurface` set. `LoadManifest` reads the curated
`specs/replay-coverage-manifest.v1.yaml` that maps each surface to the scenario
that covers it (plus audited exemptions). Every coverage entry has both an
artifact kind (`scenario`: cassette, parser_fixture, api_mcp_golden, cli_golden,
correlation, capability_claim, product_claim, authz_scoped_route, go_test, or
proof_artifact) and a depth class (`scenario_type`: baseline, delta_tombstone,
fault, ordering, crash, or cost).
`Reconcile` classifies every required
surface/scenario_type pair as `covered`, `uncovered`, `unresolved` (manifest
entry, missing artifact), or `exempt`, and reports stale manifest drift.

## Depth requirements per applicable surface (C-13)

Breadth surfaces require `baseline`. The depth classes are required **per
applicable surface**, derived (not opted into one at a time) so a
delete/crash/fault hole cannot recur unseen for any other piece — the #4186
class. `DeriveRequirements` reads the source registries:

| Depth class | Applicable surface | Lockstep source |
| --- | --- | --- |
| `fault` | every collector boundary | implemented `collector:*` (surface-inventory) |
| `cost` | every projection | distinct `reducer_domain` (fact-kind registry) |
| `ordering` | every shared-conflict-key projection | `reducer_domain` written by ≥2 projection hooks |
| `delta_tombstone` | every retractable graph node type | `cypher.RetractableNodeEntityLabels()` |
| `delta_tombstone` | every static retractable graph edge type | `cypher.RetractableEdgeTypes()` |
| `crash` | the reducer drain | the drain singleton |

The retractable node types, static retractable edge types, and reducer drain are declared in
`specs/replay-depth-requirements.v1.yaml` (`LoadDepthRequirements`);
`EnumerateDepthSurfaces` turns them plus the fact-kind projections into
`retractable_node:*`, `retractable_edge:*`, `projection:*`, and
`reducer_drain:*` surfaces, and the derived requirements are unioned with the
manifest's explicit `scenario_requirements`. Lockstep tests keep
`retractable_node_types` byte-equal to the cypher retract label registry and
`retractable_edge_types` byte-equal to the static cypher edge-retract registry,
so adding a retractable label or edge type makes the gate **demand** a delta
scenario instead of the gap going unseen. The missing surface/scenario_type pairs
the gate lists are the C-14 ([#4367](https://github.com/eshu-hq/eshu/issues/4367))
backfill worklist.

## Why a manifest (and not just the registries)

The natural keys differ across registries and artifacts: the `collector:aws`
surface is exercised by the cassette under `testdata/cassettes/awscloud`. No
single registry can express that mapping, so the manifest is the curated,
reviewable bridge. It composes with — does not fork — the existing
capability-inventory drift gate and the B-12 golden snapshot: collectors, read
surfaces, CLI surfaces, parsers, capability claims, product claims, and
authorization families all come from the same generated registries, snapshot,
and ledgers those gates already own.

## Existence, not greenness

The `Resolver` checks that a scenario artifact exists (a cassette dir, a parser
fixture file, a Go test file/package, a proof contract/evidence file, or an rc-*
/ HTTP/MCP/CLI query shape in the B-12 snapshot). For
`capability_claim` entries it resolves the `ref` against the capability matrix
and requires every profile row to name verification, with at least one supported
or experimental profile and refusal proof for unsupported profiles. For
`product_claim` entries it resolves the `ref` against the public product claim
ledger and requires deterministic proof metadata; the `capability-inventory`
docs gate validates the exact source quote, surfaces, proof signals, and semantic
posture. For `authz_scoped_route` entries it resolves the `ref` against
`specs/authorization-replay-coverage.v1.yaml` and requires the focused query test
proof row. It deliberately does **not** run the scenario. Greenness is proven by the sibling
gate named in each manifest entry's `proof_gate` (`golden-corpus-gate`, replay
tier, Go race tests, parser fixture tests, `capability-inventory`,
`capability-inventory-docs`, `authz-scoped-route-tests`, or capability-budget
proof). That split keeps this gate fast and credential-free while never claiming
a green it did not observe.

C-13 binds those proof names back to `specs/ci-gates.v1.yaml`.
`ValidateRequiredProofGates` rejects any manifest or authorization proof-ledger
`proof_gate` that is unknown, has no local command, or has neither a CI workflow
nor an explicit `local_only_reason`. `RunGate` also treats invalid proof-gate
metadata as `unresolved`, so a direct package caller cannot count a scenario as
covered when the registered proof is stale or unenforceable.

## Language-parser scoreboard (C-11)

The seven reconciled registries above gate the build. The **language-parser
coverage scoreboard** ([#4364](https://github.com/eshu-hq/eshu/issues/4364)) is a
separate, **visibility-only** artifact that does *not* gate. Its denominator is
every language in `specs/language-feature-parity-ledger.v1.yaml` (loaded by
`LoadLanguageLedger`, 32 languages today), so no language Eshu claims to parse is
silently absent from the coverage count. `BuildLanguageScoreboard` classifies
each language against the manifest's `language_exemptions` list and exact
`parser:<language>` baseline `parser_fixture` coverage rows:

- **exempt** — the language is exercised end-to-end by the golden-corpus 20-repo
  corpus (its files flow `sync -> discover -> parse -> emit`), the same
  structural reason as the `collector:git` exemption, so a dedicated parser
  fixture would re-record a path the corpus already replays;
- **fixture** — `specs/replay-coverage-manifest.v1.yaml` maps the matching
  `parser:<language>` surface to a committed parser-fixture scenario proven by
  `parserfixture-tests`;
- **uncovered** — no parser-fixture replay scenario yet: the C-12
  ([#4365](https://github.com/eshu-hq/eshu/issues/4365)) fixture-backfill
  worklist.

It is kept out of `EnumerateSupported` and `Findings` on purpose: the tree-sitter
languages genuinely *can* have a fixture (that is C-12's job), so they are honest
`uncovered` gaps, not exemptions — and listing 23 honest gaps must not turn the
blocking gate red. The single `Blocking` knob stays the only severity control;
the scoreboard is rendered into the JSON report and the C-7 dashboard's
"Language parser coverage" section, and `TestLoadRealManifestLanguageExemptionsMatchLedger`
binds every committed exemption and exact parser fixture to real ledger
languages.

## Advisory → blocking

`Findings` renders the reconciliation as `goldengate.Finding`s. Local runs can
stay **advisory** when `Blocking=false`: every gap is reported but does not fail
the command. CI passes the single blocking flag after the C-2..C-10 burn-down, so
every uncovered / unresolved / stale **baseline (breadth)** finding is required
and breadth coverage cannot regress. The C-8/C-13 **depth** classes are
advisory-first (`isBlockingScenarioType`): every non-baseline finding stays
advisory even under `-blocking`, so the gate enumerates and lists the missing
surface/scenario_type pairs (the C-14 worklist) without failing CI, until C-14
burns them down and a later ticket flips depth to blocking. `BuildReport` emits
the v3 coverage-report artifact (the v3 bump added the language-parser
scoreboard), and `RenderDashboard`
turns it into the committed, docs-discoverable **C-7 dashboard**
(`docs/public/reference/replay-coverage.md`): the overall %, a per-axis table, the
per-scenario-type C-8 table, the C-11 language-parser scoreboard, the named gap
list grouped by axis, and the
covered-surface table with each surface's scenario type, artifact kind, proof
gate, and artifact ref. The dashboard is regenerated by the gate run and held in
lockstep by `TestCommittedDashboardIsCurrent` (refresh with
`go test ./cmd/replay-coverage-gate/ -update-dashboard`), so the burn-down stays
honest in every PR diff.

## Tests

```bash
cd go && go test ./internal/replaycoverage/ -count=1
```
