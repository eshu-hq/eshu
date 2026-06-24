---
name: telemetry-coverage-discipline
description: |
  Use when a change touches `go/internal/telemetry/instruments.go`,
  `go/internal/telemetry/contract.go` or any `contract_*.go`, the contract
  doc at `docs/public/observability/telemetry-coverage.md`, the public
  operator reference at `docs/public/reference/telemetry/index.md`, the
  static-analysis verifier `scripts/verify-telemetry-coverage.sh`, the CI
  workflow `.github/workflows/verify-telemetry-coverage.yml`, the operator
  dashboard `docs/public/observability/dashboards/eshu-operator-overview.json`,
  or when adding a new `eshu_dp_*` metric or a new pipeline stage. Also
  activate when investigating an operator report of "the metric we expect
  isn't there". Captures the Epic X discipline: the four-artifact
  contract (X1 doc + X2 verifier + X3 CI gate + X4 dashboard), the
  `No-Observability-Change:` marker discipline, the contributor runbook,
  and a pointer to the "Limitations Of The X2 Gate" section so future
  maintainers know what the verifier does and does not catch.
---

# telemetry-coverage-discipline

Use this skill whenever a change touches `go/internal/telemetry/instruments.go`,
`go/internal/telemetry/contract.go` or any `contract_*.go`, the X1 contract doc
at `docs/public/observability/telemetry-coverage.md`, the public operator
reference at `docs/public/reference/telemetry/index.md`, the X2 verifier
`scripts/verify-telemetry-coverage.sh`, the X3 CI workflow
`.github/workflows/verify-telemetry-coverage.yml`, the X4 dashboard
`docs/public/observability/dashboards/eshu-operator-overview.json`, or the
X4 generator `scripts/generate-operator-dashboard.sh`. Also activate when
adding a new `eshu_dp_*` metric, a new pipeline stage, or investigating an
operator-side signal gap.

This is the Epic X discipline. It exists because the **telemetry inventory
drift** class — metrics defined in code but never documented, or referenced
in the contract but never registered — has recurred in this repo. The
historical note in `docs/public/reference/telemetry/index.md:140-156`
describes the prior instances; the maintainer narrative at
`docs/internal/telemetry-discipline-precedent.md` ties the discipline to
those incidents. The discipline replaces the human audit with a CI gate.

## When To Use

- Adding a new `eshu_dp_*` metric, span name, or log key.
- Adding a new pipeline stage under `go/internal/collector/`,
  `go/internal/reducer/`, `go/internal/projector/`,
  `go/internal/correlation/`, `go/internal/content/shape/`, or
  `go/cmd/collector-*/`.
- Touching `go/internal/telemetry/instruments.go` or `contract.go`.
- Investigating an operator report of "the metric we expect isn't there".
- Reviewing a PR that touches any of the above.

## The Four Artifacts

| Stage | Artifact | Role |
| --- | --- | --- |
| X1 | `docs/public/observability/telemetry-coverage.md` | Single source of truth: every stage → metric or `No-Observability-Change:` marker. |
| X2 | `scripts/verify-telemetry-coverage.sh` + test mirror | Static-analysis verifier: diffs X1 against `go/internal/telemetry/instruments.go` and against new files. |
| X3 | `.github/workflows/verify-telemetry-coverage.yml` | CI gate: runs X2 on every PR and push to `main`. |
| X4 | `docs/public/observability/dashboards/eshu-operator-overview.json` + generator | Operator-visible artifact: 20-panel Grafana dashboard with the headline alarm row. |

The maintainer and contributor guide that ties these together is
`docs/internal/telemetry-discipline-precedent.md`. Read it before
investigating drift; it includes a contributor runbook for adding a new
metric and a "Limitations Of The X2 Gate" section that explains exactly
what the verifier catches and does not catch.

## The `No-Observability-Change:` Marker

The escape hatch. If existing signals already diagnose a new path, the new
X1 doc row's metric column reads literally:

```text
No-Observability-Change: <names the existing eshu_dp_* signals>
```

The marker is a **positive assertion**, not a TODO. A blank or "TODO"
metric column fails the X2 verifier. The verifier checks that the marker
prose names at least one real `eshu_dp_*` metric that exists in
`instruments.go`; see `scripts/verify-telemetry-coverage.sh` for the exact
check.

## Workflow When Adding A New Metric

1. **Decide the contract first.** Name, instrument type, labels (closed
   set, bounded cardinality), unit suffix, category. All before writing
   Go code.
2. **Register in `go/internal/telemetry/instruments.go`.** Add a typed
   field on the `Instruments` struct and a registration call inside
   `InitInstruments` using the appropriate `meter.<Type>(...)`
   constructor.
3. **Emit from the dispatcher.** Pick the chokepoint (the function or
   goroutine that owns the seam), not the leaves.
4. **Add a row to `docs/public/observability/telemetry-coverage.md`.** The
   `file:line` column points at the dispatcher; the
   `required metric name(s)` column lists the new metric.
5. **If existing signals cover the path, use the marker** in step 4
   instead of inventing a new metric. Name the existing signals.
6. **Run the verifier locally.**

   ```bash
   bash scripts/test-verify-telemetry-coverage.sh
   ESHU_TELEMETRY_COVERAGE_BASE=origin/main bash scripts/verify-telemetry-coverage.sh
   ```

7. **Watch the X3 CI gate** on the PR. The
   `Verify telemetry coverage gate` check must be green.
8. **If the metric should appear on the operator dashboard**, add it to
   `scripts/lib/operator-dashboard-metrics.sh` and either edit the panel
   in `scripts/lib/operator-dashboard-panels-{1,2}.sh` or re-run
   `scripts/generate-operator-dashboard.sh` to update the committed
   artifact.

## What The X2 Verifier Catches And Does Not Catch

The verifier is a static-analysis script. Read the "Limitations Of The
X2 Gate" section in `docs/internal/telemetry-discipline-precedent.md`
before relying on it for a non-trivial surface. The short version:

- **Catches**: a metric name in X1 not registered; an `eshu_dp_*` in
  `instruments.go` not in X1; a *new* `*.go` file under a stage-owner
  directory that has no doc row with a real signal.
- **Does not catch**: a new pipeline stage added inside an existing Go
  file (the new-stage check is `git diff --diff-filter=A`); a
  pre-existing gap from before the verifier was added; a label
  cardinality explosion; a metric that lives outside
  `go/internal/telemetry/`.

A maintainer reviewing a PR that touches reducers, collectors, parsers,
or queues must still read the diff and confirm the X1 doc and the code
agree. The verifier is the load-bearing piece for the most common
drift class, not a substitute for the human audit.

## Verification

After any change in this skill's scope, run the relevant local checks
plus the X3 and X4 workflows' local equivalents.

```bash
bash scripts/test-verify-telemetry-coverage.sh
ESHU_TELEMETRY_COVERAGE_BASE=origin/main bash scripts/verify-telemetry-coverage.sh
bash scripts/test-generate-operator-dashboard.sh
scripts/generate-operator-dashboard.sh  # then git diff to confirm no drift
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
  mkdocs build --strict --clean --config-file docs/mkdocs.yml
git diff --check
```

## Cross-References

- `docs/internal/telemetry-discipline-precedent.md` — the maintainer
  narrative and the "Limitations Of The X2 Gate" section. This is the
  durable home for the historical incident links; refer there rather
  than to ephemeral issue numbers in this skill.
- `docs/public/observability/telemetry-coverage.md` — the X1 contract doc
- `docs/public/reference/telemetry/index.md` — the public operator
  reference; the per-endpoint metrics section lives at lines 223-254;
  the historical drift note lives at lines 140-156
- `go/internal/telemetry/instruments.go` — metric source of truth
- `go/internal/telemetry/contract.go` and `contract_*.go` — dimensions,
  span names, log keys

Issue numbers in this skill would rot as issues close. The precedent doc
and the historical note in the public reference are stable; the issue
links live in the precedent doc, where they are maintained alongside the
narrative.

## Failure Modes

| Failure | What to do |
| --- | --- |
| New metric emitted but the X3 CI gate fails | The X1 doc was not updated. Add a row to `docs/public/observability/telemetry-coverage.md` or use the `No-Observability-Change:` marker. |
| Operator reports a missing signal; X2 verifier says everything is fine | The verifier does not catch in-file changes. Read the relevant reducer/collector/parser file and confirm an emission site was added. If yes, the X1 doc was not updated; fix and re-run. |
| New dashboard panel does not render | Re-run `scripts/generate-operator-dashboard.sh`; the artifact may be out of date. Confirm `git diff` shows no change after running (idempotency). |
| `mkdocs build --strict` rejects a new file referenced from a doc | The file is not in the nav. Either add a `nav:` entry in `docs/mkdocs.yml` or use prose (not a markdown link) to reference it. The X4 dashboard is referenced by prose for this reason. |
| `git diff --check` flags whitespace | The repo enforces trailing-whitespace hygiene. Run the editor's "trim trailing whitespace" pass on the touched files before committing. |
