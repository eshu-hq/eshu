# AGENTS.md - internal/collector/awscloud/services/codedeploy guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned CodeDeploy domain types.
3. `scanner.go` - application/group/config/deployment fact emission.
4. `relationships.go` - deployment-group relationship derivation.
5. `observations.go` - resource observation builders and ARN derivation.
6. `../../README.md` - shared AWS cloud observation and envelope contract.
7. `docs/public/services/collector-aws-cloud.md` - AWS collector service
   coverage and runtime requirements.

## Invariants

- Keep CodeDeploy API access behind `Client`; do not import the AWS SDK into
  this package.
- Never read or persist appspec.yml lifecycle-hook bodies. `RevisionSummary`
  holds only revision type plus S3/GitHub source references.
- Never persist raw on-premises instance tag values. The SDK adapter redacts
  them; `Scanner.Scan` fails closed when the redaction key is zero.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, or deployable-unit truth from CodeDeploy names or tags.
- Preserve stable resource identities across repeated observations in the same
  AWS generation; the ARN derivation in `observations.go` is the source of
  truth for identity.
- Keep ARNs, tags, and revision references out of metric labels.

## Common Changes

- Add a new CodeDeploy metadata field by extending the scanner-owned type,
  writing a focused scanner or adapter test first, then mapping it through
  `awscloud` envelope builders.
- Add new relationship evidence only when the CodeDeploy API reports both sides
  directly and the target names a concrete resource (tag filters do not).
- Extend SDK pagination and mapping in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not read deployment revisions, appspec bodies, or instance data-plane
  state.
- Do not resolve CodeDeploy names, tags, or target links into workload
  ownership here; correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
