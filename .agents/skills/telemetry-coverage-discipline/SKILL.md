---
name: telemetry-coverage-discipline
description: Maintain Eshu telemetry contracts and dashboards when adding signals or pipeline stages, or investigating missing metrics.
---

# Telemetry coverage discipline

Keep operator signals consistent across these artifacts. Paths are relative to
the repository root.

| Artifact | Contract |
| --- | --- |
| Coverage doc | `docs/public/observability/telemetry-coverage.md`: each stage names a real signal or a justified marker. |
| Verifier | `scripts/verify-telemetry-coverage.sh` and its test mirror compare docs, registrations, and added stage-owner files. |
| CI gate | The telemetry entry in `.github/workflows/static-contract-gates.yml` enforces the verifier. |
| Dashboard | `docs/public/observability/dashboards/eshu-operator-overview.json` and its generator expose operator-facing signals. |

Metrics are registered in `go/internal/telemetry/instruments.go`. Dimensions,
span names, and log keys live in `contract.go` and `contract_*.go` there. Keep
names and instrument types intentional, labels closed and bounded, and emission
at the owning dispatcher. Update the public operator reference at
`docs/public/reference/telemetry/index.md` when its documented surface changes.

When existing signals diagnose a new path, use this positive assertion in its
coverage row and name at least one registered metric:

```text
No-Observability-Change: <existing eshu_dp_* signals>
```

A blank, TODO, or invented metric is invalid. For a new metric or stage, read
[the contributor workflow](references/new-metric.md).

## Evidence limits

A passing static verifier does not prove emission at runtime. It catches
registered/documented metric drift and added Go files without a coverage row.
It misses stages added inside existing files, old gaps, label cardinality
explosions, and metrics outside the telemetry package. Inspect the changed
emission path when those limitations matter. For missing signals or uncertain
coverage, consult the “Limitations Of The X2 Gate” section in
[the precedent guide](../../../docs/internal/telemetry-discipline-precedent.md).
The guide owns historical incident links; do not duplicate stale inventories.

## Verification by affected surface

- Coverage, registrations, or stages: run
  `ESHU_TELEMETRY_COVERAGE_BASE=origin/main bash scripts/verify-telemetry-coverage.sh`
  (use the actual reviewed base when different). Run the verifier test mirror
  when its logic or fixtures change.
- Dashboard, generator, or metric registry feeding panels: run
  `bash scripts/test-generate-operator-dashboard.sh`, regenerate with
  `scripts/generate-operator-dashboard.sh`, and check subsequent runs for no
  drift. Add template variables to `OPERATOR_DASHBOARD_METRIC_VARS` when needed.
- Go emission changes: run focused production-path tests and relevant Go gates.
- Documentation changes: run the repository docs build and `git diff --check`.

These are focused development checks. Required CI, hooks, docs, and promotion
gates still apply; do not rerun unrelated artifact suites merely because this
skill was loaded. Finish the scoped implementation and local proof before
publishing under the session's existing authorization.
