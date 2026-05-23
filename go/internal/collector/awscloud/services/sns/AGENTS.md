# AGENTS.md - services/sns

Read `README.md`, `doc.go`, `types.go`, `scanner.go`, and `awssdk/README.md`
before editing this service.

## Mandatory Rules

- Keep SNS AWS access behind `Client`; the scanner package must not import the
  AWS SDK.
- Emit reported topic metadata, tags, and ARN-shaped subscription relationship
  evidence only.
- Do not publish messages, read message payloads, persist topic policy JSON,
  delivery-policy JSON, data-protection-policy JSON, or raw email/SMS/HTTP(S)
  endpoints.
- Do not infer workload, environment, repository, ownership, or deployable-unit
  truth from topic names, tags, or subscriptions.
- Keep topic ARNs, topic names, tags, subscription ARNs, endpoints, raw AWS
  errors, and page tokens out of metric labels.
