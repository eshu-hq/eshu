# replay-coverage-gate

The **C-1/C-8/C-9/C-10 replay coverage manifest + lockstep gate**
([#4173](https://github.com/eshu-hq/eshu/issues/4173),
[#4187](https://github.com/eshu-hq/eshu/issues/4187),
[#4188](https://github.com/eshu-hq/eshu/issues/4188),
[#4189](https://github.com/eshu-hq/eshu/issues/4189), epic
[#4172](https://github.com/eshu-hq/eshu/issues/4172)). It is the keystone of the
replay-coverage-completeness epic: it proves that every surface and required
scenario-depth class Eshu claims to support has a green, credential-free,
Docker-free replay scenario — and fails CI on any supported-but-uncovered
surface/scenario_type pair.

This command is the thin orchestrator; the typed, unit-tested reconciliation
logic lives in [`internal/replaycoverage`](../../internal/replaycoverage).

## What it does

1. Loads the source-of-truth registries: the embedded surface inventory and
   fact-kind registry (the same generated artifacts the capability-inventory
   drift gate owns — composed, not forked), the B-12 CLI query-shape catalog, the
   parser-backing ledger, the capability matrix, the public product-claim
   ledger, and the authorization catalog.
2. Enumerates the supported surfaces (implemented-lane collectors, read surfaces,
   CLI read surfaces, parsers, positive capability claims, public product
   claims, and live authorization permission families in both in-grant and
   out-of-grant modes).
3. Reconciles each against `specs/replay-coverage-manifest.v1.yaml` and the
   on-disk / snapshot scenario artifacts. Each coverage entry carries an artifact
   kind (`scenario`) and a C-8 depth class (`scenario_type`: baseline,
   delta_tombstone, fault, ordering, crash, or cost). `capability_claim` entries
   resolve against the capability matrix itself and require every profile row to
   carry a verification reference. `product_claim` entries resolve against
   `specs/product-claims.v1.yaml` and require deterministic proof metadata.
   `authz_scoped_route` entries resolve against
   `specs/authorization-replay-coverage.v1.yaml` and require a concrete query
   test proof row.
4. Writes a JSON coverage report and the committed, docs-discoverable C-7
   Markdown dashboard (`docs/public/reference/replay-coverage.md`), and prints
   per-registry and per-scenario-type satisfied percentages.
5. Exits non-zero only in `-blocking` mode when a required surface/scenario_type
   pair is uncovered, unresolved, or a manifest entry is stale.

## Run

```bash
cd go && go run ./cmd/replay-coverage-gate \
  -specs-dir ../specs \
  -snapshot ../testdata/golden/e2e-20repo-snapshot.json \
  -repo-root .. \
  -report-out /tmp/replay-coverage-report.json
```

Add `-blocking` to fail on any gap. CI passes this flag so breadth or depth
coverage regressions block; omit it only for local exploratory reports.

## Flags

| Flag | Default | Purpose |
| --- | --- | --- |
| `-specs-dir` | `specs` | directory holding the registry specs |
| `-snapshot` | `testdata/golden/e2e-20repo-snapshot.json` | B-12 snapshot for correlation/query-shape scenarios |
| `-manifest` | `<specs-dir>/replay-coverage-manifest.v1.yaml` | the curated coverage manifest |
| `-repo-root` | `.` | root that cassette/parser-fixture refs resolve against |
| `-report-out` | (none) | path to write the JSON coverage report |
| `-dashboard-out` | (none) | path to write the Markdown coverage dashboard (the committed C-7 artifact) |
| `-blocking` | `false` | fail on any uncovered/unresolved/stale surface |

The committed dashboard is held in lockstep by `TestCommittedDashboardIsCurrent`;
regenerate it after a coverage-moving change with
`go test ./cmd/replay-coverage-gate/ -update-dashboard`.

## Greenness is proven elsewhere

This gate verifies a scenario artifact **exists**; it does not run it. Each
manifest entry names the `proof_gate` that runs the scenario and proves it green
(`golden-corpus-gate`, replay tier, Go race tests, parser fixture tests,
`capability-inventory`, `capability-inventory-docs`, `authz-scoped-route-tests`,
or capability-budget proof). Keeping existence here and greenness there is what
makes this gate fast and credential-free.
