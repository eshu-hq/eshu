# AGENTS.md - services/ecs/awssdk

Read `README.md`, `doc.go`, `client.go`, `mapper.go`, and `../README.md`
before editing this adapter.

## Mandatory Rules

- Allowed calls are ECS list/describe calls for clusters, services, task
  definitions, and tasks.
- Wrap every page, batch, and point read in `recordAPICall`.
- Do not add `ExecuteCommand`, task stop/start, service mutation, secret-value
  reads, credential, STS, graph, or reducer behavior here.
- Preserve SDK response mapping as scanner-owned metadata; scanner code owns
  redaction before persistence.
- Keep cluster names, service names, task ARNs, task definition ARNs, tags,
  image URIs, environment values, page tokens, and raw AWS errors out of metric
  labels.
