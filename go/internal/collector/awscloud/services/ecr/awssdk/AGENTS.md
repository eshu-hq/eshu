# AGENTS.md - services/ecr/awssdk

Read `README.md`, `doc.go`, `client.go`, `checkpoint.go`, `mapper.go`, and
`../README.md` before editing this adapter.

## Mandatory Rules

- Allowed calls are repository and image pagination, `GetLifecyclePolicy`, and
  `ListTagsForResource`.
- Keep durable image pagination checkpoints retry-safe; save only the token that
  can be safely retried for the current claim boundary.
- Wrap every page and point read in `recordAPICall`.
- Do not add image layer downloads, auth token exposure, repository mutation,
  image mutation, credential, STS, graph, or reducer behavior here.
- Keep repository names, ARNs, image digests, tags, policy JSON, checkpoint
  parents, page tokens, and raw AWS errors out of metric labels.
