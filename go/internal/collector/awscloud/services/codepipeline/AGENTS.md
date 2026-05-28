# AGENTS.md - internal/collector/awscloud/services/codepipeline guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned CodePipeline domain types.
3. `scanner.go` - pipeline/execution/webhook/action-type fact emission.
4. `relationships.go` - pipeline, stage, action, and webhook relationship
   derivation.
5. `observations.go` - resource observation builders and ARN derivation.
6. `../../README.md` - shared AWS cloud observation and envelope contract.
7. `docs/public/services/collector-aws-cloud.md` - AWS collector service
   coverage and runtime requirements.

## Invariants

- Keep CodePipeline API access behind `Client`; do not import the AWS SDK into
  this package.
- Never persist an action configuration value. The scanner-owned `Action` has
  no value field; it carries configuration KEY names and allowlisted non-secret
  target identifiers only. The `action.Configuration` map holds GitHub OAuth
  tokens, webhook secrets, and provider credentials.
- Never persist the webhook authentication secret token or a GitHub OAuthToken.
- Route source-revision summaries through redaction in the SDK adapter; the
  scanner fails closed when the redaction key is zero.
- Every relationship sets a non-empty `target_type` matching the target
  scanner's resource_id. Synthesized ARNs derive the partition from the
  boundary, never a hardcoded `arn:aws:` prefix.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, or deployable-unit truth from CodePipeline names or tags.
- Keep ARNs, tags, and revision references out of metric labels.

## Common Changes

- Add a new CodePipeline metadata field by extending the scanner-owned type,
  writing a focused scanner or adapter test first, then mapping it through
  `awscloud` envelope builders.
- Add a new build/deploy/invoke target by extending the provider classifier and
  the non-secret identifier-key allowlist in the `awssdk` adapter, plus the
  target switch in `relationships.go`. Never add a key that can carry a secret.
- Extend SDK pagination and mapping in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not read action configuration values, job details, or the job-worker
  plane (PollForJobs and friends return secret configuration values).
- Do not persist webhook secret tokens or GitHub OAuth tokens.
- Do not resolve CodePipeline names, tags, or target links into workload
  ownership here; correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
