# AGENTS.md - services/cloudwatchlogs

Read `README.md`, `doc.go`, `types.go`, `scanner.go`, `relationships.go`, and
`awssdk/README.md` before editing this service.

## Mandatory Rules

- Keep CloudWatch Logs AWS access behind `Client`; the scanner package must not
  import the AWS SDK.
- Emit reported control-plane metadata only: log group attributes, tags, and
  direct KMS dependency evidence.
- Do not read log events, log stream payloads, Insights query results, export
  payloads, resource policies, subscription payloads, or mutation APIs.
- Do not infer workload, environment, repository, ownership, or deployable-unit
  truth from names, tags, accounts, or aliases.
- Add relationships only when CloudWatch Logs directly reports both sides.
- Keep log group names, ARNs, tags, KMS IDs, raw AWS errors, and page tokens out
  of metric labels.
