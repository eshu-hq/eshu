# Evidence: #5594 bare `backend "local" {}` default-path drift ownership

This note carries the performance/no-regression and observability evidence
markers `scripts/verify-performance-evidence.sh` requires for the hot-path
files this PR touches. The full narrative (problem, fix shape, hostile-review
findings) lives in `CHANGELOG.md`'s "Bare `backend \"local\" {}` drift
ownership resolution" section; this file exists only because the gate's
`is_evidence_file()` allowlist does not include `CHANGELOG.md` or
`docs/public/observability/telemetry-coverage.md`, so the markers below are
duplicated here rather than moved, to keep both docs' own prose intact.

Touched hot-path files this covers:

- `go/internal/collector/terraformstate/backend_config.go`
- `go/internal/collector/terraformstate/backend_config_local.go`
- `go/internal/collector/terraformstate/discovery_types.go`
- `go/internal/reducer/terraform_config_state_drift.go`
- `go/internal/reducer/terraform_config_state_drift_unresolved_owner.go`
- `go/internal/reducer/terraform_config_state_drift_writer.go`
- `go/internal/storage/postgres/terraform_config_state_drift_findings.go`
- `go/internal/storage/postgres/tfstate_backend_canonical.go`
- `go/internal/storage/postgres/tfstate_backend_filter.go`
- `go/internal/storage/postgres/tfstate_backend_interpolation.go`
- `go/internal/telemetry/instruments.go`

## Backend-local default-path candidate derivation and the repo-scoped join

`backend_config.go`/`backend_config_local.go` add one additional
`BackendLocal` candidate derivation per Terraform backend block (no new
per-fact-record work; same O(1)-per-block shape as the existing `s3` path).
`tfstate_backend_canonical.go`'s `repo_local_paths` CTE adds a
`(scope_id, generation_id, repo_id)`-scoped, `DISTINCT ON` + latest-`observed_at`
join, mirroring the pre-existing `active_repositories` convention used
elsewhere in the same query, so it does not introduce a new join shape or
scan pattern.

- No-Regression Evidence (#5594): `cd go && go test ./internal/parser/
  ./internal/collector/ ./internal/collector/terraformstate/...
  ./internal/storage/postgres/... ./internal/relationships/tfstatebackend/...
  ./internal/reducer/ -count=1` is green.
  `TestPostgresTerraformBackendQueryResolvesBareLocalBackendDefaultPath`,
  `TestEvaluateBackendConfigDefaultsBareLocalBackendPath`, and
  `TestDefaultEngineParsePathHCLLocalBackendBareBlockOmitsPathAttribute` fail
  before this change (zero candidates / no `state_path` field) and pass after.
  `TestPostgresTerraformBackendQueryRepoLocalPathJoinIsRepoScopedAndLatest`
  (`go/internal/storage/postgres/tfstate_backend_canonical_repo_local_path_integration_test.go`)
  was run against a real, freshly schema-migrated Postgres 16 container:
  fails against the pre-fix join (0 rows where 1 was expected), passes
  against the fix. `EXPLAIN (ANALYZE, BUFFERS)` on a 200-repo seeded corpus
  showed no measurable regression: 2.890 ms (pre-fix) vs. 2.873 ms (fixed),
  both returning the correct 200 rows with no duplication.
- Observability Evidence (#5594): `DiscoveryCandidate.LocatorDefaulted`
  threads through `TerraformBackendRow`/`CommitAnchor` unchanged; a new
  info-level `"drift candidate resolved via defaulted locator"` log
  (`locator_defaulted=true`) fires only for the new defaulted-resolution path,
  leaving the pre-existing explicit-path and `s3` flows' log volume unchanged.
  `TestDriftHandlerLogsDefaultedLocatorResolution` and
  `TestDriftHandlerDoesNotLogDefaultedLocatorForExplicitResolution` cover both
  cases. The rejection path reuses the reducer's existing `"drift candidate
  rejected"` structured log (`failure_class`/`rejection.reason`); no new
  stage, query, worker, or metric there.

## `unresolved` outcome durable write and read-surface

`terraform_config_state_drift_unresolved_owner.go` and
`terraform_config_state_drift_writer.go` add a third write mode,
`UnresolvedOwner`, persisting exactly one durable `"unresolved"` finding per
state-snapshot scope, at the same per-scope-per-generation cadence the
pre-existing `"ambiguous"` write already has (same upsert-by-stable-fact-id
idempotency, same non-fatal write-failure handling).
`terraform_config_state_drift_findings.go` and `terraform_config_state_drift.go`
extend the existing read query/handler to surface `"unresolved"` alongside
`exact`/`ambiguous`, with no new query shape or scan pattern.

- No-Regression Evidence (#5594): `cd go && go test ./internal/reducer/...
  ./internal/query/... ./internal/storage/postgres/... ./internal/mcp/...
  ./internal/telemetry/... ./internal/correlation/... -count=1` is green.
  `TestPostgresTerraformConfigStateDriftWriterPersistsUnresolvedOwnerFinding`,
  `TestDriftHandlerNoOwnerWritesDurableUnresolvedFinding`, and
  `TestHandleTerraformConfigStateDriftFindingsDistinguishesUnresolvedFromNoDrift`
  each fail before the corresponding production change (missing field / zero
  writes / identical empty page for both cases) and pass after.
  `TestDriftHandlerAmbiguousOwnerStillWritesAmbiguousNotUnresolved` and
  `TestTerraformConfigStateDriftFindingStoreFiltersByOutcome` (pre-existing)
  both stayed green throughout, proving the pre-existing `"ambiguous"` path is
  untouched.
- Observability Evidence (#5594): `eshu_dp_drift_unresolved_owner_write_failed_total`
  (`go/internal/telemetry/instruments.go`, registered alongside the
  pre-existing `DriftAmbiguousOwnerWriteFailed`), with its X1 contract row in
  `docs/public/observability/telemetry-coverage.md`. Verified with `bash
  scripts/test-verify-telemetry-coverage.sh` (15/15) and
  `ESHU_TELEMETRY_COVERAGE_BASE=origin/main bash
  scripts/verify-telemetry-coverage.sh` (clean, no drift).

## Golden-corpus fixture wiring (`tfstate_backend_filter.go`, `tfstate_backend_interpolation.go`)

These two files are on the touched-hot-path list because they sit in the same
package as `tfstate_backend_canonical.go`'s join change; their own logic
(backend filter predicates, interpolation-expression evaluation) is unchanged
by this PR. No-Regression Evidence (#5594): covered by the same
`go test ./internal/storage/postgres/... -count=1` run cited above, which
exercises this package's existing test suite unchanged and green.

## P0 golden-corpus snapshot fix (this pass)

Separately from the production-code evidence above: the two new B-12 query
shapes this PR added
(`POST /api/v0/terraform/config-state-drift/findings?variant=local-backend-resolved`
and `...?variant=unresolved`) were committed without `results_field`, which
`EvaluateQueryShape` (`go/internal/goldengate/evaluate.go`, since #5862) treats
as an unconditional hard error for any shape that sets `minimum_results` --
confirmed by `git log -p` showing neither entry ever carried `results_field`.
Fixed by adding `"results_field": "drift_findings"` to both, matching the
pre-existing S3-scope sibling shape immediately above them in
`testdata/golden/e2e-20repo-snapshot.json`.

- No-Regression Evidence (#5594): `cd go && go test ./cmd/golden-corpus-gate/...
  -count=1` was red (`--- FAIL: TestQueryClientChecksHTTPShapes`, both entries
  named with `results_field is required when minimum_results ... is set`)
  before this fix and green after. Confirmed the assertion is not vacuous:
  evaluating each fixed shape against a synthetic response whose
  `drift_findings` array is empty (every other required field present) fails
  with `"drift_findings" has 0 results, want >= 1`, not the missing-field
  error, proving `minimum_results` actually binds once `results_field` is set.
- No-Observability-Change (#5594): a JSON fixture edit; no runtime code path,
  metric, span, or log changed.
