# AGENTS.md - services/iam

Read `README.md`, `doc.go`, `types.go`, `scanner.go`, `relationships.go`, and
`awssdk/README.md` before editing this service.

## Mandatory Rules

- Keep IAM AWS access behind `Client`; the scanner package must not import the
  AWS SDK.
- Keep IAM global and use the configured global region label.
- Emit reported role, policy, instance-profile, trust-policy, and attachment
  metadata only.
- Do not persist credential material, access keys, inline policy bodies beyond
  the documented safe projection, or mutation results.
- Do not infer workload, environment, repository, ownership, or deployable-unit
  truth from names, paths, tags, policies, accounts, or aliases.
- Keep IAM names, paths, ARNs, tags, trust policy text, raw AWS errors, and page
  tokens out of metric labels.
