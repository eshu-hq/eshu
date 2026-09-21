# #6695 observability/prometheus leaf move evidence

## Moved (behavior-preserving, `git mv` + package/qualifier edits only)

- `go/internal/collector/prometheusmimir/` →
  `go/internal/collector/observability/prometheus/` (14 files, tempo-leaf
  precedent: `package prometheusmimir` → `package prometheus`).
- Importers repointed (import path + qualifier only):
  `go/cmd/collector-prometheus-mimir/service.go` and `config.go`,
  `go/internal/replay/inputtape/acceptance_test.go` and
  `fault_collectors_test.go`, plus two prose package-name mentions in the
  inputtape README/doc.
- Preserved byte-identically (contracts, not paths): `CollectorKind =
  "prometheus_mimir"`, span names (`prometheus_mimir.observe/fetch`),
  metric names (`eshu_dp_prometheus_mimir_*`), the `"prometheusmimir"`
  tape name in the acceptance test, and the `prometheus-mimir-claim-*`
  ID prefixes.
- Parent `observability/README.md` now documents both children (tempo +
  prometheus); leaf trio paths updated.
- Registry-adjacent path-only updates: `specs/surface-inventory.v1.yaml`
  owner/proof_gates/fixture_refs, the Prometheus/Mimir row path in
  `docs/public/observability/telemetry-coverage.md`, and the two
  `collector-live-smokes.md` test paths. Historical run records
  (4786 matrix, 4791 evidence) intentionally untouched.
- No dirgate row covers this subtree (8 non-test files, under the 40 cap).

## No-Regression Evidence:

Baseline: the pre-move file contents at origin/main 3fbf13ea6 (git
records the move at high rename similarity; the only code edits are the
package clause, importer paths/qualifiers, and a gofumpt import-order
fix). After: `go test -count=1
./internal/collector/observability/... ./cmd/collector-prometheus-mimir/`
green (prometheus, tempo, cmd all ok), `go test -list` discovers all 12
leaf tests, and the affected inputtape suites pass. Backend/version:
unit-level proof only (recording fakes); the live smoke
(`TestLivePrometheusMimirObservedMetricEvidence`) requires
`ESHU_PROMETHEUS_MIMIR_BASE_URL` and stays a CI/live gate. Input shape:
unchanged collector configs and fact envelopes. Why safe: the move is
invisible at runtime (same package behavior, same contracts, same
telemetry names); the only production change is the import path.

## No-Observability-Change:

No spans, metrics, structured logs, or status surfaces are added,
removed, renamed, or re-labeled: the touched files contain no telemetry
definition changes, and the telemetry-coverage row keeps its exact span
and metric names (path cell only).

## Proof (this worktree, before push)

- `go build` + `go vet` clean on the observability tree, the cmd, and
  inputtape.
- `go test` green: observability/prometheus, observability/tempo,
  cmd/collector-prometheus-mimir, affected inputtape suites.
- `gofumpt -l` clean on all touched Go files; every touched file < 500 lines.
