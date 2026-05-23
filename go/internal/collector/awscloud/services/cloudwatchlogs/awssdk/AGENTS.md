# AGENTS.md - services/cloudwatchlogs/awssdk

Read `README.md`, `doc.go`, `client.go`, `mapper.go`, and `../README.md`
before editing this adapter.

## Mandatory Rules

- Allowed calls are `DescribeLogGroups` and `ListTagsForResource`.
- Use `Limit=50`, follow `NextToken`, and wrap every AWS call in
  `recordAPICall`.
- Use the non-wildcard log group ARN for tags; trim a trailing `:*` only when
  `logGroupArn` is absent.
- Do not add log event, log stream payload, Insights, export, resource-policy,
  subscription, mutation, credential, STS, graph, or reducer behavior here.
- Keep log group names, ARNs, tags, KMS IDs, page tokens, and raw AWS errors out
  of metric labels.
