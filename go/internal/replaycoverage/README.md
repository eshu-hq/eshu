# replaycoverage

`replaycoverage` is the assertion core of the **C-1/C-8/C-9/C-10 replay coverage
manifest + lockstep gate** ([#4173](https://github.com/eshu-hq/eshu/issues/4173),
[#4187](https://github.com/eshu-hq/eshu/issues/4187),
[#4188](https://github.com/eshu-hq/eshu/issues/4188),
[#4189](https://github.com/eshu-hq/eshu/issues/4189), epic
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
Surfaces require baseline by default; each `scenario_requirements` row must
include baseline and opts a surface into additional C-8 depth dimensions.
`Reconcile` classifies every required
surface/scenario_type pair as `covered`, `uncovered`, `unresolved` (manifest
entry, missing artifact), or `exempt`, and reports stale manifest drift.

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

## Advisory → blocking

`Findings` renders the reconciliation as `goldengate.Finding`s. Local runs can
stay **advisory** when `Blocking=false`: every gap is reported but does not fail
the command. CI now passes the single blocking flag after the C-2..C-10 burn-down,
so every uncovered / unresolved / stale finding is required and coverage cannot
regress. `BuildReport` emits the v2 coverage-report artifact, and `RenderDashboard`
turns it into the committed, docs-discoverable **C-7 dashboard**
(`docs/public/reference/replay-coverage.md`): the overall %, a per-axis table, the
per-scenario-type C-8 table, the named gap list grouped by axis, and the
covered-surface table with each surface's scenario type, artifact kind, proof
gate, and artifact ref. The dashboard is regenerated by the gate run and held in
lockstep by `TestCommittedDashboardIsCurrent` (refresh with
`go test ./cmd/replay-coverage-gate/ -update-dashboard`), so the burn-down stays
honest in every PR diff.

## Tests

```bash
cd go && go test ./internal/replaycoverage/ -count=1
```
