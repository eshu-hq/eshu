# AGENTS.md - internal/collector/awscloud/checkpoint guidance

## Read First

1. `README.md` - checkpoint contract and commit-boundary warning.
2. `types.go` - scope, key, checkpoint, and store interface.
3. `../awsruntime/source.go` - workflow claim and generation construction.
4. `../../../../storage/postgres/aws_pagination_checkpoint.go` - durable
   storage implementation.
5. `../services/ecr/awssdk/client.go` - first service adapter using the seam.

## Invariants

- Scope every checkpoint by collector instance, account, region, service,
  generation, and fencing token.
- Treat `ResourceParent` and `PageToken` as high-cardinality values. Never put
  them in metric labels.
- Save retry-safe page tokens before page reads unless a future committer hook
  proves the next page token is durable after fact commit.
- Expire stale checkpoints when generation changes.
- Reject stale fencing tokens in storage instead of serializing AWS claim work.

## Common Changes

- Add a new service operation by creating a `Key` with a stable parent and
  operation name in the service adapter.
- Add storage behavior in `storage/postgres` without importing a concrete
  service scanner.
- Update this package's README when the commit boundary changes.

## What Not To Change Without An ADR

- Do not make checkpoint state part of AWS fact payloads.
- Do not advance checkpoints beyond uncommitted facts.
- Do not use page tokens, ARNs, repository names, or hosted-zone IDs as metric
  labels.
