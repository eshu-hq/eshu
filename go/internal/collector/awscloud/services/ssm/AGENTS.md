# AGENTS.md - services/ssm

Read `README.md`, `doc.go`, `types.go`, `scanner.go`, `relationships.go`, and
`awssdk/README.md` before editing this service.

## Mandatory Rules

- Keep SSM AWS access behind `Client`; the scanner package must not import the
  AWS SDK.
- Emit reported Parameter Store metadata, tags, and direct KMS evidence only.
- Do not read parameter values, history values, raw descriptions, raw allowed
  patterns, raw policy JSON, decrypted content, or mutation results.
- Do not infer workload, environment, repository, ownership, or deployable-unit
  truth from parameter names, paths, tags, accounts, or aliases.
- Keep parameter names, paths, ARNs, tags, KMS IDs, raw AWS errors, and page
  tokens out of metric labels.
