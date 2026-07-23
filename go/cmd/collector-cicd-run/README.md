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
for workflow and fact stores, and `internal/telemetry` for spans and metrics.

## Telemetry

The runtime emits `ci_cd_run.observe` and `ci_cd_run.fetch` spans through the
source package. Metrics include
`eshu_dp_ci_cd_run_provider_requests_total`,
`eshu_dp_ci_cd_run_fetch_duration_seconds`,
`eshu_dp_ci_cd_run_rate_limited_total`,
`eshu_dp_ci_cd_run_facts_emitted_total`, and
`eshu_dp_ci_cd_run_partial_generations_total`. Labels stay bounded to provider,
status class, fact kind, and partial reason.

## Gotchas / invariants

- `ESHU_COLLECTOR_INSTANCES_JSON` must contain exactly one matching enabled
  claim-capable `ci_cd_run` instance unless
  `ESHU_CICD_RUN_COLLECTOR_INSTANCE_ID` selects it.
- Target credentials come from `token_env`; token values are never stored in
  collector JSON, facts, logs, metrics, or status.
- `allowed_repositories`, `max_runs`, `max_jobs`, and `max_artifacts` bound the
  provider read shape.
- Heartbeat interval must be shorter than the claim lease TTL.

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
