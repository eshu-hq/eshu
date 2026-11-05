# collector-cicd-run

## Purpose

`collector-cicd-run` runs the hosted GitHub Actions CI/CD run collector. It
claims planned `ci_cd_run` workflow items, fetches bounded workflow-run, job,
and artifact metadata, and commits source facts for reducer-owned bridge proof.

## Ownership boundary

The command owns runtime configuration, credential environment resolution,
Postgres wiring, workflow claims, and the shared hosted admin surface. It does
not plan collector instances, infer deployment truth from CI success, write
graph truth, or resolve source-to-image bridge correlations.

## Exported surface

See `doc.go` for the package contract. The command exposes only process
behavior: `eshu-collector-cicd-run`, `/healthz`, `/readyz`, `/metrics`, and
`/admin/status`. The default `-mode=live` runs the claim-driven provider
collector; `-mode=cassette -cassette-file=<path>` replays recorded
ci.run/ci.artifact facts credential-free for the golden-corpus gate.

## Dependencies

The command imports `internal/collector` for claimed service execution,
`internal/collector/cicdrun/ghactionsruntime` for provider reads,
`internal/replay/cassette` for credential-free cassette replay,
`internal/runtime` for pprof and Postgres bootstrap, `internal/storage/postgres`
for workflow and fact stores, `internal/storage/postgres/cicd` for
(#5429) `CICDRunWatermarkStore`, and `internal/telemetry` for spans and
metrics.

## Telemetry

The runtime emits `ci_cd_run.observe` and `ci_cd_run.fetch` spans through the
source package. Metrics include
`eshu_dp_ci_cd_run_provider_requests_total`,
`eshu_dp_ci_cd_run_fetch_duration_seconds`,
`eshu_dp_ci_cd_run_rate_limited_total`,
`eshu_dp_ci_cd_run_facts_emitted_total`, and
`eshu_dp_ci_cd_run_partial_generations_total`. Labels stay bounded to provider,
status class, fact kind, and partial reason. Terminal App mint failures and
non-rate-limited direct-App provider 401/403/404 reads use
`status_class="auth_error"`.
Host-file failures use `auth_denied`; static-token provider failures retain
`error`. Throttled provider reads and App mints use `rate_limited`.

## Gotchas / invariants

- `ESHU_COLLECTOR_INSTANCES_JSON` must contain exactly one matching enabled
  claim-capable `ci_cd_run` instance unless
  `ESHU_CICD_RUN_COLLECTOR_INSTANCE_ID` selects it.
- GitHub Actions targets select one credential shape. `token_env` (the default)
  resolves a static token at startup. `host_minted` reads a host-managed file
  containing `token` and `scope_id` on each claim; startup checks the file,
  and a missing, malformed, scope-mismatched, or Actions-refused token fails
  terminally as `auth_denied`. A bare 403 without rate-limit headers on the
  runs fetch uses the same terminal class instead of burning the claim's
  attempt budget. The next claim re-resolves the file and recovers after the
  host refreshes it; persistent permission failures repeat as `auth_denied`.
  The host refreshes by atomic replace below the one-hour token TTL, and the
  collector keeps no frozen host bearer. A Deployments-only 403/404 keeps run
  facts with a permission warning. `github_app` resolves three private env
  references at startup and mints one installation token per claim for bounded
  Actions and Deployments reads. These shapes cannot mix. GitLab CI still uses
  `token_env` and ignores GitHub-only fields. Credential values never enter
  collector JSON, facts, logs, metrics, or status.
- `allowed_repositories`, `max_runs`, `max_jobs`, and `max_artifacts` bound the
  provider read shape.
- Heartbeat interval must be shorter than the claim lease TTL.
- `-mode=live` (`buildClaimedService`) wires `SourceConfig.Watermarks` to a
  `cicdstore.CICDRunWatermarkStore` over the same database as the workflow
  and fact stores, so cross-cycle run-collection gap detection (#5429)
  survives process restarts and is visible across collector replicas. The
  `-mode=cassette` credential-free replay path does not use
  `ghactionsruntime.ClaimedSource` at all (see `internal/replay/cassette`),
  so it carries no watermark and needs none.
- `providerRoutedSource` must forward `ObserveClaimedGenerationCommitted` and
  `ObserveClaimedGenerationRefused` by `scope_id`, because `ClaimedService` finds
  them by type assertion on its `Source`. From #5778 to #10063 it did not, so this
  binary never wrote `cicd_run_watermarks`, gap detection never fired, and each
  claim leaked one staged entry. The first deployment with #10063 starts those
  writes. To watch that rollout: `cicd_run_watermarks` should gain a row per GitHub
  target that has at least one run (an empty window saves nothing, so a target
  with no runs gets no row); `eshu_dp_workflow_claim_commit_observer_failures_total{collector_kind="ci_cd_run"}`
  should stay flat (a rise means the save failed or lost the guard, and the
  `claimed_generation_commit_observer_failed` span event carries the error); and
  `eshu_dp_ci_cd_run_partial_generations_total{reason="runs_backfill_gap"}` can
  start for the first time on repositories busier than `max_runs` per cycle, which
  is the signal working, not a regression. While pre-#10063 pods are still
  draining during the rollout they load new-pod rows but never advance them, so
  they can emit transient `runs_backfill_gap` for windows the new pods actually
  collected; trust that signal only after the rollout completes. To roll back to
  an image before #10063,
  also run `DELETE FROM cicd_run_watermarks`, and run it only after the last pod
  on the new image has exited: a draining new pod can still save a row, and that
  row would then stay frozen. The old image reads the rows but never advances
  them, so a frozen row would report a false gap on every truncated window.

## Related docs

- `docs/public/deployment/service-runtimes-collectors.md`
- `docs/public/reference/environment-collectors.md`
- `docs/public/reference/collector-reducer-readiness.md`
- `go/internal/collector/cicdrun/ghactionsruntime/README.md`

## Evidence

No-Regression Evidence: `go test ./cmd/collector-cicd-run
./internal/collector/cicdrun/ghactionsruntime ./internal/runtime -count=1`
covers config parsing, claim-driven provider collection, binary deployability,
and Helm chart rendering for the hosted runtime.

Observability Evidence: `go test ./internal/collector/cicdrun/ghactionsruntime
./internal/telemetry ./internal/storage/postgres -count=1` covers CI/CD run
provider metrics, spans, partial-generation metrics, rate-limit metrics, and
fact-backed central collector status evidence.

### Historical #9186 evidence

The following validation and impact notes were recorded for #9186 against
baseline `d1f56dc14b` using Go 1.26.6. They do not describe or validate the
App authentication, durable Actions queue, storage, or telemetry changes in
#9146; those changes require their own proof.

No-Regression Evidence (#9186): `go test ./cmd/collector-cicd-run
./internal/collector/cicdrun/... ./internal/workflow -count=1` was reported
green for that change with synthetic unit targets and no live provider calls. The token_env mapping statements
are verbatim and its fetch-error propagation is untouched, so existing
static-token activations behave byte-identically (pinned by
TestMapTargetTokenEnvByteIdentical). host_minted adds per-claim file
resolution (a local disk read, no provider volume change) and classifies a
refused host value as terminal `auth_denied`; the fetch, pagination, and
watermark hot path is otherwise untouched. These unit checks do not measure
provider latency or the end-to-end cost of #9146. No queue, lease, Cypher, or
storage path is touched in #9186, so there
are no terminal queue or row counts to compare.

Observability Evidence (#9186): no new metrics, spans, logs, or status
fields. The existing fetch dimensions carry one new bounded value:
resolution and refusal failures record under `status_class="auth_denied"`,
distinguishable from provider `"error"`, with the token value excluded from
every label (pinned by
TestClaimedSourceHostMintedRecordsAuthDeniedTelemetry and the lane startup
tests).

## Workflow Claim Store

`buildClaimedService` gets its claim store from `store.New`
(`go/internal/collector/claim/store`). With `ESHU_WORKFLOW_CLAIM_PLANE_URL`
unset or blank, the collector claims from the core Postgres workflow store
exactly as before. With a valid `http` or `https` base URL, it claims through
the `eshu-workflow` claim plane over `workflow.v1` and never falls back to
Postgres. An invalid URL stops startup with an error that names the variable.
Until the claim-plane commit fence lands (#8875 P2), an ingestion commit on a
claim-plane claim fails closed. The startup log
`collector.workflow_control_store.selected` and
`eshu_dp_workflow_control_store_selections_total` show which store the pod
selected.
