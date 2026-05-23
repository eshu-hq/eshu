# AGENTS.md - services/secretsmanager/awssdk

Read `README.md`, `doc.go`, `client.go`, `mapper.go`, and `../README.md`
before editing this adapter.

## Mandatory Rules

- Allowed call is `ListSecrets` for this slice.
- Wrap every page in `recordAPICall`; keep operation labels aligned with AWS SDK
  operation names.
- Do not add `GetSecretValue`, `BatchGetSecretValue`,
  `ListSecretVersionIds`, `GetResourcePolicy`, partner rotation metadata,
  mutation, credential, STS, graph, or reducer behavior here.
- Keep secret names, ARNs, tags, KMS IDs, Lambda ARNs, page tokens, and raw AWS
  errors out of metric labels.
