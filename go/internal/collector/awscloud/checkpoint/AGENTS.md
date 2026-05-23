# AGENTS.md - internal/collector/awscloud/checkpoint

Read `README.md`, `doc.go`, `types.go`, `../awsruntime/source.go`,
`../../../../storage/postgres/aws_pagination_checkpoint.go`, and the service
adapter using the checkpoint seam before editing this package.

## Mandatory Rules

- Scope every checkpoint by collector instance, account, region, service,
  generation, and fencing token.
- Save the page token that is safe to retry before reading the page unless the
  commit boundary changes and proves the next token is durable after fact
  commit.
- Expire stale checkpoints when generation changes.
- Reject stale fencing tokens in storage; do not serialize AWS claim work to
  avoid checkpoint races.
- Keep checkpoint state out of AWS fact payloads.
- Do not advance checkpoints beyond uncommitted facts.
- Keep `ResourceParent`, `PageToken`, ARNs, repository names, hosted-zone IDs,
  and raw AWS errors out of metric labels.
