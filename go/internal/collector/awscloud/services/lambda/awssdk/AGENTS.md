# AGENTS.md - services/lambda/awssdk

Read `README.md`, `doc.go`, `client.go`, `mapper.go`, and `../README.md`
before editing this adapter.

## Mandatory Rules

- Allowed calls are `ListFunctions`, `GetFunction`, `ListAliases`, and
  `ListEventSourceMappings`.
- Wrap every page and point read in `recordAPICall`.
- Drop presigned package download URLs from `GetFunction`; scanner callers must
  receive stable metadata only.
- Do not add invocation, code download, resource-policy persistence, mutation,
  credential, STS, graph, or reducer behavior here.
- Keep function names, ARNs, tags, image URIs, environment values, code URLs,
  page tokens, and raw AWS errors out of metric labels.
