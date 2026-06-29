# replay-coverage-gate

The **C-1 replay coverage manifest + lockstep gate**
([#4173](https://github.com/eshu-hq/eshu/issues/4173), epic
[#4172](https://github.com/eshu-hq/eshu/issues/4172)). It is the keystone of the
replay-coverage-completeness epic: it proves that every surface Eshu claims to
support has a green, credential-free, Docker-free replay scenario — and fails CI
(once blocking) on any supported-but-uncovered surface.

This command is the thin orchestrator; the typed, unit-tested reconciliation
logic lives in [`internal/replaycoverage`](../../internal/replaycoverage).

## What it does

1. Loads the four source-of-truth registries: the embedded surface inventory and
   fact-kind registry (the same generated artifacts the capability-inventory
   drift gate owns — composed, not forked), the parser-backing ledger, and the
   capability matrix.
2. Enumerates the supported surfaces (implemented-lane collectors, read surfaces,
   parsers, positive capability claims).
3. Reconciles each against `specs/replay-coverage-manifest.v1.yaml` and the
   on-disk / snapshot scenario artifacts.
4. Writes a JSON coverage report (C-7 input) and prints per-registry satisfied
   percentages.
5. Exits non-zero only in `-blocking` mode when a surface is uncovered,
   unresolved, or a manifest entry is stale.

## Run

```bash
cd go && go run ./cmd/replay-coverage-gate \
  -specs-dir ../specs \
  -snapshot ../testdata/golden/e2e-20repo-snapshot.json \
  -repo-root .. \
  -report-out /tmp/replay-coverage-report.json
```

Add `-blocking` to fail on any gap. The shipped CI default is advisory.

## Flags

| Flag | Default | Purpose |
| --- | --- | --- |
| `-specs-dir` | `specs` | directory holding the registry specs |
| `-snapshot` | `testdata/golden/e2e-20repo-snapshot.json` | B-12 snapshot for correlation/query-shape scenarios |
| `-manifest` | `<specs-dir>/replay-coverage-manifest.v1.yaml` | the curated coverage manifest |
| `-repo-root` | `.` | root that cassette/parser-fixture refs resolve against |
| `-report-out` | (none) | path to write the JSON coverage report |
| `-blocking` | `false` | fail on any uncovered/unresolved/stale surface |

## Greenness is proven elsewhere

This gate verifies a scenario artifact **exists**; it does not run it. Each
manifest entry names the `proof_gate` that runs the scenario and proves it green
(`golden-corpus-gate`, the parser fixture tests). Keeping existence here and
greenness there is what makes this gate fast and credential-free.
