# AGENTS.md - services/ssm/awssdk

Read `README.md`, `doc.go`, `client.go`, `mapper.go`, and `../README.md`
before editing this adapter.

## Mandatory Rules

- Allowed calls are `DescribeParameters` and `ListTagsForResource`.
- Use resource type `Parameter`, wrap every page and point read in
  `recordAPICall`, and keep operation labels aligned with AWS SDK names.
- Do not expose `GetParameter`, `GetParameters`, `GetParametersByPath`,
  `GetParameterHistory`, decryption, raw policy JSON, mutation, credential,
  STS, graph, or reducer behavior here.
- Reduce raw descriptions and allowed patterns to presence flags only.
- Keep parameter names, paths, ARNs, tags, KMS IDs, page tokens, and raw AWS
  errors out of metric labels.
