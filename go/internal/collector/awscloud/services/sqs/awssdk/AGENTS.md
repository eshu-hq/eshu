# AGENTS.md - services/sqs/awssdk

Read `README.md`, `doc.go`, `client.go`, `mapper.go`, and `../README.md`
before editing this adapter.

## Mandatory Rules

- Allowed calls are `ListQueues`, `GetQueueAttributes` with an explicit safe
  metadata allowlist, and `ListQueueTags`.
- Do not request or persist the queue `Policy` attribute.
- Wrap every paginator page and point read in `recordAPICall`.
- Do not call `ReceiveMessage`, `DeleteMessage`, `PurgeQueue`, queue/message
  mutation, credential, STS, graph, or reducer APIs.
- Keep queue URLs, names, ARNs, tags, redrive policy values, page tokens, and
  raw AWS errors out of metric labels.
