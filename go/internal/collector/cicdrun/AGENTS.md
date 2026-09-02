# AGENTS.md — internal/collector/cicdrun guidance

## Read First

1. `README.md` — package purpose, exported surface, runtime boundary, and
   invariants.
2. `github_actions_fixture.go` — GitHub Actions fixture normalization.
3. `gitlab_ci_fixture.go` — GitLab CI/CD fixture normalization (issue #5427);
   reuses the SAME `ci.*` fact kinds and reducer join-key shape as
   `github_actions_fixture.go` — read both together when touching either.
4. `github_actions_deployments.go` — GitHub Deployments API event
   normalization (#5425 STEP 3; `ci.deployment_event`). A separate
   fact-kind family from `github_actions_fixture.go`'s run-scoped kinds: a
   deployment carries no `run_id`, so it does not go through the shared
   `sharedPayload`/`warningEnvelope` run-keyed helpers — read
   `deploymentEventEnvelope` and `GitHubActionsDeploymentWarningEnvelope`
   before touching either.
5. `envelope.go` — fact identity and envelope construction.
6. `docs/public/reference/collector-reducer-readiness.md` — source-truth boundary
   and implementation gates.

## Invariants

- Keep hosted provider polling in `ghactionsruntime`; do not move HTTP clients,
  workflow claims, credential loading, or runtime status into the parent
  normalizer package.
- Do not add graph writes, reducer imports, or query imports here.
- Preserve provider-native IDs and `run_attempt` in fact identity.
- Emit warnings for partial provider metadata instead of silently publishing
  complete-looking facts.
- Strip token-bearing URLs before payload or source-reference emission.
- Do not infer deployment truth from CI success, job names, shell text, or
  environment names.
- `ci.deployment_event`'s stable key is `{provider, scope_id, repository,
  deployment_id, status_id}` -- NEVER `state` or `updated_at`. Adding either
  to the key would make a re-poll of the same status mint a NEW fact instead
  of upserting, defeating the "pending -> in_progress -> success are three
  durable facts" contract the reducer's `selectDeploymentEvent` deterministic
  ranking depends on.

## Common Changes

- Add a provider by creating provider-specific fixture parsing and envelope
  tests in this package.
- Add or change live API collection only in `ghactionsruntime` or another
  runtime subpackage with credentials, request budgets, redaction proof,
  health/readiness, metrics, and status.
- If payload shape changes, check `go/internal/reducer/cicdrun/ci_cd_run_correlation.go`
  (issue #6061 moved this domain out of the flat reducer root into the
  `cicdrun` family subpackage) so reducer anchors stay aligned.
