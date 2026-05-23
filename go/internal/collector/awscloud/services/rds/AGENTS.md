# AGENTS.md - services/rds

Read `README.md`, `doc.go`, `types.go`, `scanner.go`, `relationships.go`, and
`awssdk/README.md` before editing this service.

## Mandatory Rules

- Keep RDS AWS access behind `Client`; the scanner package must not import the
  AWS SDK.
- Emit reported DB instance, cluster, subnet group, tag, security-group,
  subnet, KMS, and role evidence only.
- Do not read database rows, snapshots, logs, secret values, parameter values,
  policy bodies, or mutation results.
- Do not infer workload, environment, repository, ownership, or deployable-unit
  truth from identifiers, endpoints, tags, accounts, or aliases.
- Keep DB identifiers, endpoints, ARNs, tags, KMS IDs, security group IDs, raw
  AWS errors, and page tokens out of metric labels.
