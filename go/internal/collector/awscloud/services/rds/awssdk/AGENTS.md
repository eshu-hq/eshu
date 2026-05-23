# AGENTS.md - services/rds/awssdk

Read `README.md`, `doc.go`, `client.go`, `mapper.go`, and `../README.md`
before editing this adapter.

## Mandatory Rules

- Allowed calls are `DescribeDBInstances`, `DescribeDBClusters`,
  `DescribeDBSubnetGroups`, and `ListTagsForResource`.
- Follow markers and wrap every page or point read in `recordAPICall`.
- Do not add database data reads, log reads, snapshot payload reads, secret
  reads, parameter value reads, policy persistence, mutation, credential, STS,
  graph, or reducer behavior here.
- Keep DB identifiers, endpoints, ARNs, tags, KMS IDs, page tokens, and raw AWS
  errors out of metric labels.
