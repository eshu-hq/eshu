# AGENTS.md - services/secretsmanager

Read `README.md`, `doc.go`, `types.go`, `scanner.go`, `relationships.go`, and
`awssdk/README.md` before editing this service.

## Mandatory Rules

- Keep Secrets Manager AWS access behind `Client`; the scanner package must not
  import the AWS SDK.
- Emit reported secret metadata, tag evidence, KMS evidence, and rotation
  Lambda relationship evidence only.
- Do not read secret values, version payloads, resource policy JSON, external
  rotation partner metadata, or mutation results.
- Do not infer workload, environment, repository, ownership, or deployable-unit
  truth from secret names, tags, accounts, or aliases.
- Keep secret names, ARNs, tags, KMS IDs, Lambda ARNs, raw AWS errors, and page
  tokens out of metric labels.
