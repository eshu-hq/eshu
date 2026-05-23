# AGENTS.md - services/sqs

Read `README.md`, `doc.go`, `types.go`, `scanner.go`, and `awssdk/README.md`
before editing this service.

## Mandatory Rules

- Keep SQS AWS access behind `Client`; the scanner package must not import the
  AWS SDK.
- Emit reported queue metadata, tags, and dead-letter queue relationship
  evidence only.
- Do not read messages, persist message bodies, persist queue policy JSON, or
  mutate queues or messages.
- Do not infer workload, environment, repository, ownership, or deployable-unit
  truth from queue names, tags, or DLQ links.
- Keep queue URLs, names, ARNs, tags, redrive policy values, raw AWS errors, and
  page tokens out of metric labels.
