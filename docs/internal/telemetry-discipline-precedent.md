# Telemetry Discipline Precedent

This precedent ties the recurring **telemetry inventory drift** class to its
prevention. It is the maintainer and contributor guide that explains why
`docs/public/observability/telemetry-coverage.md` (X1) and
`scripts/verify-telemetry-coverage.sh` (X2) exist, and how Epic X wires them
into CI (X3) and the operator dashboard (X4). Read this when adding a new
metric, when touching `go/internal/telemetry/instruments.go`, or when
investigating a 3 AM incident where an operator expected a signal and the
graph was silent.

## Failure Class

**Telemetry inventory drift** is any of:

- a metric defined in code (`go/internal/telemetry/instruments.go`) but never
  documented in the public contract (`docs/public/observability/telemetry-coverage.md`
  or `docs/public/reference/telemetry/index.md`),
- a metric referenced in the public contract but not actually registered in
  the instrument surface, or
- a new pipeline stage that emits a counter the contract never named, leaving
  the operator with a graph they cannot see.

The shape is always the same: a name on one side, nothing on the other. The
operator discovers the gap when the incident already has them on the call.

## Historical Incidents

| When | Where | What broke |
| --- | --- | --- |
| Pre-2026-06-23 | `docs/public/reference/telemetry/index.md:140-156` (historical note) | `eshu_dp_shared_acceptance_rows` and `eshu_dp_worker_pool_active` were defined-but-never-registered for an extended period. |
| 2026-06-23 | [#3633](https://github.com/eshu-hq/eshu/issues/3633) (closed) | Generation-liveness counters (`eshu_dp_generation_liveness_*`) were defined in code but missing from the telemetry README and the public docs index. The X1 doc references this issue as the root-cause class. |
| 2026-06-24 (open) | [#3680](https://github.com/eshu-hq/eshu/issues/3680) | Per-collector telemetry work in flight; the adopted discipline is the X1-X4 contract. |

The two earlier incidents are the same class with different surfaces. They were
human-caught because a maintainer noticed and filed an issue. The discipline
below replaces the human with a CI gate.

## Prevention Mechanism (Epic X)

| Stage | Artifact | Role |
| --- | --- | --- |
| X1 | `docs/public/observability/telemetry-coverage.md` ([#3689](https://github.com/eshu-hq/eshu/issues/3689), [PR #3715](https://github.com/eshu-hq/eshu/pull/3715)) | The single source of truth that maps every reducer / projector / collector / parser stage to a metric, span, log key, or `No-Observability-Change:` marker. |
| X2 | `scripts/verify-telemetry-coverage.sh` + test mirror ([#3690](https://github.com/eshu-hq/eshu/issues/3690), [PR #3718](https://github.com/eshu-hq/eshu/pull/3718)) | Static-analysis verifier that diffs the X1 doc against `go/internal/telemetry/instruments.go` and against new files added since the base ref. Fails on any drift in either direction. |
| X3 | `.github/workflows/verify-telemetry-coverage.yml` ([#3691](https://github.com/eshu-hq/eshu/issues/3691), [PR #3720](https://github.com/eshu-hq/eshu/pull/3720)) | The CI gate that runs X2 on every pull request and push to `main`. |
| X4 | `docs/public/observability/dashboards/eshu-operator-overview.json` + generator ([#3692](https://github.com/eshu-hq/eshu/issues/3692), [PR #3722](https://github.com/eshu-hq/eshu/pull/3722)) | The operator-visible artifact. The headline single-stat row surfaces `eshu_dp_active_generations{age_bucket="stuck"}` and `eshu_dp_generation_liveness_failures_total` as the 3 AM alarm signal. |

Together: a maintainer reads the X1 doc to learn the contract, runs the X2
script to validate their PR, sees X3 enforce it in CI, and confirms the
operator surface in X4. The link from "what broke" to "what prevents it next
time" is now a single file index.

## Diff Semantics

The X2 verifier fails when any of the following is true after a PR:

1. The X1 doc references a metric name that is not registered in
   `go/internal/telemetry/instruments.go`. (Symptom: the doc over-promises.)
2. `go/internal/telemetry/instruments.go` registers an `eshu_dp_*` metric
   that is not mentioned anywhere in the X1 doc. (Symptom: defined-but-never-
   registered, the #3633 class.)
3. A new `*.go` file under `go/internal/collector/`, `go/internal/reducer/`,
   `go/internal/projector/`, `go/internal/correlation/`, `go/internal/content/shape/`,
   or `go/cmd/collector-*/` has been added since the base ref, AND the X1 doc
   does not have a row that names the file's dispatcher column, AND that row's
   metric column carries either an `eshu_dp_*` name or a `No-Observability-Change:`
   marker. (Symptom: a new pipeline stage slipped in without telemetry.)

The intent of the `No-Observability-Change:` marker is the escape hatch. If
existing signals already diagnose the new path, the new row's metric column
must read `No-Observability-Change: <names the existing signals>`. This is a
**positive assertion**, not a TODO. A blank or "TODO" metric column fails the
gate.

## Contributor Runbook: How To Add A New Metric

Use this runbook when adding a new `eshu_dp_*` metric or a new pipeline stage.

1. **Decide the contract first.** Before writing Go code, decide:
   - the metric name (`eshu_dp_<surface>_<noun>_<unit_total_seconds_etc>`),
   - the instrument type (`Int64Counter`, `Int64Histogram`,
     `Int64ObservableGauge`, etc.),
   - the labels (closed set, bounded cardinality),
   - the unit suffix (`_total`, `_seconds`, `_bytes`),
   - the category (reducer runtime, collector dispatch, queue domain, etc.).
2. **Register the instrument in `go/internal/telemetry/instruments.go`.**
   - Add a typed field on the `Instruments` struct (see existing
     `ActiveGenerations`, `QueueDepth`, `WorkerPoolActive` for shape).
   - Add a registration call inside `InitInstruments` using the
     `meter.<Type>("<name>", ...)` constructor.
3. **Emit the metric from the dispatcher.** The first arg of the constructor
   is the metric name; the call site is the dispatcher. Pick the chokepoint
   (the function or goroutine that owns the seam), not the leaves.
4. **Add a row to `docs/public/observability/telemetry-coverage.md`.** Use the
   existing table shape. The `file:line` column must point at the dispatcher,
   not the contract. The `required metric name(s)` column must list the new
   metric name.
5. **If an existing signal already diagnoses the path, use the marker.** The
   metric column reads `No-Observability-Change: <names the existing signal(s)>`.
   Do not leave a blank or "TODO" cell; the X2 verifier rejects it.
6. **Run the verifier locally.**

   ```bash
   bash scripts/test-verify-telemetry-coverage.sh
   ESHU_TELEMETRY_COVERAGE_BASE=origin/main bash scripts/verify-telemetry-coverage.sh
   ```

7. **Watch the CI gate.** PR pushes trigger the
   `.github/workflows/verify-telemetry-coverage.yml` workflow. A failure means
   the doc, the code, or both drifted. The drift report is uploaded as the
   `telemetry-coverage-drift-report` artifact.
8. **If the new metric should appear on the operator overview dashboard**, add
   the metric name to `scripts/lib/operator-dashboard-metrics.sh` and either
   edit the panel in `scripts/lib/operator-dashboard-panels-{1,2}.sh` or
   re-run `scripts/generate-operator-dashboard.sh` to update the committed
   artifact. The dashboard generator has its own test mirror
   (`scripts/test-generate-operator-dashboard.sh`) and CI workflow
   (`.github/workflows/generate-operator-dashboard.yml`).

## Verifying The Discipline End To End

A maintainer or new contributor can confirm the discipline works in five
minutes.

1. **Read the contract.** `docs/public/observability/telemetry-coverage.md`
   shows every reducer / projector / collector / parser stage mapped to a
   metric or `No-Observability-Change:` marker. Read it as a maintainer
   would: scan the row you care about and confirm the metric name and
   `file:line` dispatcher are still accurate. The X2 verifier does not
   catch every drift (see Limitations below); the human audit is part of
   the discipline.
2. **Run the verifier.** `bash scripts/test-verify-telemetry-coverage.sh`
   runs the test mirror (8 cases) and `scripts/verify-telemetry-coverage.sh`
   (with `ESHU_TELEMETRY_COVERAGE_BASE=origin/main`) runs the gate. Both pass
   on `main`; the X2 PR proved it.
3. **Check CI.** Open any merged PR from Epic X. The
   `verify-telemetry-coverage / Verify telemetry coverage gate` check is
   green. The drift report artifact is empty (the gate would have failed
   with a per-stage diff otherwise).
4. **View the operator surface.** Import the dashboard at
   `docs/public/observability/dashboards/eshu-operator-overview.json` into a
   stock Grafana instance with a Prometheus source. The "Is Eshu Healthy?"
   row shows the alarm signal.
5. **Read the policy.** This file is the link from the historical incidents
   to the prevention. If you change the contract, change this file and the
   X1 doc together.

## Limitations Of The X2 Gate

The verifier is a static-analysis script; it catches a specific class of
drift, not all drift. Maintainers and contributors must understand what
the gate does and does not see.

The verifier catches:

- A metric name referenced in the X1 doc that is not registered in
  `go/internal/telemetry/instruments.go`.
- An `eshu_dp_*` metric registered in `go/internal/telemetry/instruments.go`
  that is not mentioned anywhere in the X1 doc.
- A *new* `*.go` file added since the base ref under a stage-owner
  directory (`go/internal/collector/`, `go/internal/reducer/`,
  `go/internal/projector/`, `go/internal/correlation/`,
  `go/internal/content/shape/`, or `go/cmd/collector-*/`) where the X1
  doc has no row that names the file's dispatcher with a real signal
  (an `eshu_dp_*` metric or a `No-Observability-Change:` marker).

The verifier does not catch:

- A new pipeline stage added *inside* an existing Go file. The new-stage
  check is `git diff --diff-filter=A`, which is file-level: an existing
  file that gains a new function or a new metric emission will not
  trigger the new-stage check, and a stale X1 row will not be flagged.
  Reviewers must catch these by reading the diff and confirming the X1
  row was updated.
- A pre-existing gap from before the verifier was added. The verifier
  compares the *current* X1 doc against the *current*
  `go/internal/telemetry/instruments.go`. It does not surface drift that
  was already on `main` when the X2 PR landed. A periodic human audit of
  the X1 doc is still required to catch this class.
- A label cardinality explosion, a missing dimension, a wrong unit suffix,
  or any semantic change to a metric that does not change its name. The
  verifier compares names; everything else is out of scope.
- A telemetry contract that lives outside `go/internal/telemetry/` (for
  example, an instrument registered from a third-party SDK with a
  different naming convention). Such surfaces are not part of Eshu's
  first-party contract and are out of scope for this discipline.

The X2 verifier is the load-bearing piece for the most common drift class
(metric name drift on a PR), but it is not a substitute for the human
audit. A maintainer reviewing a PR that touches reducers, collectors,
parsers, or queues must still read the diff and confirm the X1 doc and
the code agree.

## Cross-References

- `docs/internal/agent-guide.md:120-146` — the five evidence markers policy
  that this discipline complements. Every PR that touches runtime, queue,
  collector, or graph writes needs at least one of `Performance Evidence:`,
  `Benchmark Evidence:`, `No-Regression Evidence:`, `Observability Evidence:`,
  or `No-Observability-Change:` in a tracked repo file.
- `docs/public/reference/telemetry/index.md` — the public contract
  documentation that surfaces the same metric names to operators.
- `docs/public/reference/telemetry/index.md:223-254` — the per-endpoint
  metrics section, the implementation reference for API request counters.
- `go/internal/telemetry/instruments.go` — the metric source of truth.
- `go/internal/telemetry/contract.go` and `contract_*.go` — dimensions, span
  names, and log keys.

## In-Flight Adoption

- [#3680](https://github.com/eshu-hq/eshu/issues/3680) (open, 2026-06-24) —
  per-collector telemetry work is the first major in-flight change that
  adopts the X1-X4 discipline. New collectors land with a row in the X1 doc,
  a registered instrument, and a `No-Observability-Change:` marker where
  appropriate.
