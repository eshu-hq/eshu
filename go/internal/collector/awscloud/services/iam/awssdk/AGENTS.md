# AGENTS.md - services/iam/awssdk

Read `README.md`, `doc.go`, `client.go`, `mapper.go`, and `../README.md`
before editing this adapter.

## Mandatory Rules

- Allowed calls are IAM pagination and metadata reads for roles, policies, and
  instance profiles, including source trust policy decoding.
- Wrap every page and point read in `recordAPICall`.
- Do not add credential reads, access-key reads, user/group inventory, mutation
  APIs, STS, graph, or reducer behavior here.
- Keep IAM names, paths, ARNs, tags, policy text, page tokens, and raw AWS
  errors out of metric labels.
