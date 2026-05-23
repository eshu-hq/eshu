# AGENTS.md - services/dynamodb/awssdk

Read `README.md`, `doc.go`, `client.go`, `mapper.go`, and `../README.md`
before editing this adapter.

## Mandatory Rules

- Allowed calls are `ListTables`, `DescribeTable`, `ListTagsOfResource`,
  `DescribeTimeToLive`, and `DescribeContinuousBackups`.
- Wrap every page and point read in `recordAPICall`; keep pagination and
  throttling observable.
- Do not add item, stream-record, export payload, backup payload, resource
  policy, PartiQL, mutation, credential, STS, graph, or reducer behavior here.
- Add retry/throttle or optional-error handling only from AWS or Smithy
  evidence and cover it with adapter tests.
- Keep table names, ARNs, tags, KMS IDs, stream ARNs, page tokens, and raw AWS
  errors out of metric labels.
