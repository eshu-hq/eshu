# AGENTS.md - services/eventbridge

Read `README.md`, `doc.go`, `types.go`, `scanner.go`, `relationships.go`, and
`awssdk/README.md` before editing this service.

## Mandatory Rules

- Keep EventBridge AWS access behind `Client`; the scanner package must not
  import the AWS SDK.
- Emit reported event bus, rule, target, tag, and direct target relationship
  evidence only.
- Do not persist event bus policies, target payload fields, input templates,
  credentials, or event payloads.
- Do not call `PutEvents` or infer workload, environment, repository,
  ownership, or deployable-unit truth from rule names, targets, or tags.
- Keep bus names, rule names, target IDs, ARNs, tags, raw AWS errors, and page
  tokens out of metric labels.
