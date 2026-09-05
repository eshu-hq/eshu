# Adding a metric or stage

Paths in backticks are repository-relative.

## Workflow When Adding A New Metric

1. **Decide the contract first.** Name, instrument type, labels (closed
   set, bounded cardinality), unit suffix, category. All before writing
   Go code.
2. **Register in `go/internal/telemetry/instruments.go`.** Add a typed
   field on the `Instruments` struct and a registration call inside
   `NewInstruments` using the appropriate `meter.<Type>(...)`
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
   ESHU_TELEMETRY_COVERAGE_BASE=origin/main bash scripts/verify-telemetry-coverage.sh
   ```

   Use the reviewed base if it differs from `origin/main`. Run
   `bash scripts/test-verify-telemetry-coverage.sh` when verifier logic or
   fixtures change; required CI checks still apply.

7. **If the metric should appear on the operator dashboard**, add it to
   `scripts/lib/operator-dashboard-metrics.sh` (registering the metric
   variable and, if it is a template token, adding its name to the
   `OPERATOR_DASHBOARD_METRIC_VARS` allowlist), update the panel body in
   `scripts/lib/operator-dashboard-panels-{1,2}.json.tmpl` as needed, and
   re-run `scripts/generate-operator-dashboard.sh` to update the
   committed artifact.

Complete dashboard updates and local checks before publication. When the task
includes a PR, the required telemetry CI gate must pass before merge.
