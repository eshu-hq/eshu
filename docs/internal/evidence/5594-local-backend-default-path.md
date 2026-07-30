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

## Cassette-substitution `sed` fix and corrected corpus-proof status (this pass)

With `results_field` fixed, a live run still failed
`?variant=local-backend-resolved` (`"drift_findings" has 0 results, want >=
1`). Diagnostics against that run showed zero `ingestion_scopes` rows for the
computed scope_id `state_snapshot:local:10ae50af...` -- the state-snapshot
scope never landed at all, though the fixture repository itself did.

Root cause: `stage_local_backend_cassette`'s `sed` call
(`scripts/lib/golden-corpus-local-backend.sh`) has never substituted anything
since the commit that introduced it. The `-e` arguments were double-quoted --
`"s|\$LOCAL_BACKEND_SCOPE_ID\$|${local_backend_scope_id}|g"` -- so bash
consumes the backslash before each `\$` during its own double-quote parsing,
and `sed` receives a bare, unescaped `$LOCAL_BACKEND_SCOPE_ID$` pattern. An
unescaped trailing `$` in a BRE is an end-of-line anchor, so the pattern only
matches "...SCOPE_ID$" immediately at end of line -- never true in the
cassette JSON, where the sentinel is always followed by a closing quote and
more content. `sed` exits 0 having substituted nothing. Verified directly by
re-running the exact pre-fix arguments against the real committed cassette:
all four sentinel occurrences (including one inside the scope's own `"note"`
prose) survived byte-for-byte. This also explains why the sibling
`?variant=unresolved` scope (a plain literal scope_id, never routed through
this substitution) landed and passed live in the same run.

Fixed by single-quoting the `\$`-escaped pattern segments and concatenating
the variable-bearing replacement as a separate double-quoted segment, so the
backslashes reach `sed` intact.

- No-Regression Evidence (#5594): `bash
  scripts/test-verify-golden-corpus-gate.sh` passes; `bash -n
  scripts/lib/golden-corpus-local-backend.sh` is clean. Verified by sourcing
  the actual patched `stage_local_backend_cassette` function (stubbing `pg`
  and `bin_dir`, not reimplementing the `sed` call by hand) against the real
  committed cassette: the produced runtime copy carries zero remaining
  sentinel occurrences and the correct substituted
  `scope_id`/`partition_key`/`payload.locator_hash` values. The live gate
  re-run that confirms this fix end to end is not runnable in this
  environment.
- No-Observability-Change (#5594): a shell orchestration fix in a
  gate-support script; no runtime code path, metric, span, or log changed.

**Corrected corpus-proof status.** Across all live rounds to date, the
`backend "local" {}` default-path fix's `"exact"`-outcome path has never
actually been proven end to end by a green
`?variant=local-backend-resolved` assertion -- the state-snapshot scope that
proof depends on had never reached Postgres under its intended identifier
until this fix. Only the `"unresolved"`-outcome path (which never depended on
this substitution) has live proof today. A fresh `bash
scripts/verify-golden-corpus-gate.sh` run against this fix is required before
the `"exact"`-outcome path can be called proven end to end; see
`CHANGELOG.md`'s "Third live golden-corpus-gate round trip" entry for the
full narrative.
