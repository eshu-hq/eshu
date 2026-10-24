# Internal → Public Shim Map (`#8854`, analysis artifact)

This is a **design map, not an implementation**. It records how each
in-tree seam becomes a public SDK exchange at extraction, so the `#6710`
template repository and the `#4047` parity gate share one
interpretation. Nothing here changes runtime behavior.

## 1. Claim path: `ClaimedService` / `NextClaimed` → SDK `Result` / `Claim`

There is no unclaimed poll path for the live binary: `ClaimedSource.
NextClaimed` (`ghactionsruntime/source.go:165`,
`gitlabciruntime/source.go:139`) is the only live emission entry. It
binds a `workflow.WorkItem` to one configured target by `scope_id`
(`validateClaim` in each source rejects any other
`scope.CollectorCICDRun` mismatch), and reuses `item.GenerationID` so
retries are idempotent. `providerRoutedSource.NextClaimed`
(`provider_source.go:45`) resolves the claim's `scope_id` to its
registered provider source first and delegates; an unregistered scope
fails closed (`"has no registered provider source"`).

| In-tree outcome | SDK `ResultState` (`sdk/go/collector/types.go:32-40`) | Host action |
| --- | --- | --- |
| envelopes committed | `complete` | validate facts, complete claim |
| rate-limited provider response, partial page committed | `partial` + `Status{warning}` + `ci.warning` facts | commit reachable evidence |
| truncation cap cut (`max_runs` ceiling, artifact caps) | `partial` + `Status{warning}` + truncated `ci.warning` | commit capped evidence |
| transient transport / retryable provider status | `retryable` | release claim, bounded retry |
| terminal (auth-denied at claim scope, contract violation) | `terminal` | terminal-fail claim |
| cancellation | (no result) | stop, no commit |

Duplicate delivery stays safe: stable keys are deterministic
(`facts.StableID` in each fixture constructor) and committed
generations are idempotent at the core boundary.

| In-tree field | SDK `Claim` (`sdk/go/collector/types.go:88-101`) |
| --- | --- |
| `item.CollectorKind` (`scope.CollectorCICDRun`) | `collector_kind` |
| `item.CollectorInstanceID` vs `SourceConfig.CollectorInstanceID` | `instance_id` (mismatch rejects) |
| `item.ScopeID` | `scope` (scope id; provider implied by registration) |
| `item.GenerationID` / `item.GenerationID` reuse on retry | `generation_id` |
| fencing token (watermark row / control store) | `fencing_token` |
| `item.Attempt`, deadline, `WorkItemID` | `attempt`, `deadline`, `work_item_id` |
| per-target scan config | `config_handle` (opaque; resolved host-side) |

Stale-claim rule (core-owned): the workflow control store, not the
producer, decides lease expiry and supersession. The producer must treat
a claim it did not `.Claim()` as foreign and never write for it.

## 2. The `ci.*` kinds at `1.0.0` → SDK `FactDeclaration`

One declaration per kind, each `schema_versions: ["1.0.0"]`
(`go/internal/facts/ci_cd_run.go:45-69`):

```json
[
  {"kind": "ci.pipeline_definition", "schema_versions": ["1.0.0"]},
  {"kind": "ci.run", "schema_versions": ["1.0.0"]},
  {"kind": "ci.job", "schema_versions": ["1.0.0"]},
  {"kind": "ci.step", "schema_versions": ["1.0.0"]},
  {"kind": "ci.artifact", "schema_versions": ["1.0.0"]},
  {"kind": "ci.trigger_edge", "schema_versions": ["1.0.0"]},
  {"kind": "ci.environment_observation", "schema_versions": ["1.0.0"]},
  {"kind": "ci.deployment_event", "schema_versions": ["1.0.0"]},
  {"kind": "ci.warning", "schema_versions": ["1.0.0"]}
]
```

Nine kinds, not ten: `ci.workflow_image_evidence` is family-declared
but emitted by the git collector
(`go/internal/collector/repo/git/workflow/image/facts.go`), so the
extracted `cicd-run` producer must never declare or emit it. The
corresponding SDK `Fact` mapping is `stable_key` ← fixture stable key,
`source_ref.scope_id` ← target `scope_id`, `source_ref.generation_id`
← committed generation, `payload` ← `factschema.EncodeCICD*` contract
payload (`sdk/go/factschema/cicdrun/v1`), redactions ← URL/text
sanitizers (`stripSensitiveURL`, `redactSensitiveText`,
`sanitizeDeploymentURL`).

## 3. `targetJSON` operator shape (unchanged by extraction)

`targetJSON` (`go/cmd/collector-cicd-run/config.go:59-70`) keys:
`provider` (`github_actions` default when omitted, `gitlab_ci`),
`scope_id`, `repository` (owner/repo for GitHub, namespace/project for
GitLab), `token_env`, `allowed_repositories`, `api_base_url`,
`max_runs`, `max_jobs`, `max_artifacts` (GitHub-only; silently ignored
for `gitlab_ci`), `source_uri` (defaults to
`https://github.com/<repo>` or `https://gitlab.com/<project>`).
Secrets travel by env reference, never inline. Disabled or
non-claim-enabled instances fail closed; `heartbeat < lease` is
enforced.

## 4. Telemetry the external producer must preserve

| Signal | Name | Emitted at |
| --- | --- | --- |
| span | `cicd_run.observe` (`SpanCICDRunObserve`) | `source.go:178` (gha), `source.go:152` (gl) |
| span | `cicd_run.fetch` (`SpanCICDRunFetch`) | `source.go:180` (gha), `source.go:154` (gl) |
| histogram | `eshu_dp_ci_cd_run_fetch_duration_seconds` | shared instruments, both runtimes |
| counter | `eshu_dp_ci_cd_run_provider_requests_total` | shared instruments, both runtimes |
| counter | `eshu_dp_ci_cd_run_facts_emitted_total` | shared instruments, both runtimes |
| counter | `eshu_dp_ci_cd_run_rate_limited_total` | shared instruments, both runtimes |
| counter | `eshu_dp_ci_cd_run_partial_generations_total` | shared instruments, both runtimes |

Attributes stay `provider` (`github_actions` | `gitlab_ci`),
`status_class`, `fact_kind`. Instance IDs, repository names, target
URLs, token values, and raw API payloads must never appear in metric
labels. The host additionally records grant decisions per emission
(`#6709`).

No-Observability-Change: this PREP adds no signals; the existing
`eshu_dp_ci_cd_run_*` counters, fetch-duration histogram, and the two
spans already cover the claimed path. The external producer must
preserve them verbatim.

## 5. Known deltas (in-tree vs external, by design)

1. **Grant check**: in-tree needs none (first-party, same binary).
   External fails closed on missing/expired/revoked grant at install,
   activation, and every emission. See [PRODUCER_GRANT_DRAFT](PRODUCER_GRANT_DRAFT.md).
2. **Commit boundary**: in-tree calls `postgres.NewIngestionStore`
   directly (`service.go:112`). External returns facts; core commits.
3. **Claim control**: in-tree `ClaimedService` owns lease/heartbeat
   (`service.go:115-130`). External receives a bounded `Claim` and
   returns a `Result`; transitions stay `eshu-workflow`.
4. **Shared kinds**: the `ci.*` kinds are also the contract of the
   factschema SDK, not of sibling collectors — but the grant's producer
   identity + scope (`ci_cd_run` instance scope) is what separates this
   producer's authority from the git collector's
   `ci.workflow_image_evidence` authority, not the kind family name.
5. **Watermark**: in-tree GitHub watermarks live in
   `cicd_run_watermarks` (migration 078). The external producer keeps
   no durable watermark; gap detection across restarts stays core-side
   until the extracted release proves its own equivalent.
