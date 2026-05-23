# AGENTS.md - services/s3

Read `README.md`, `doc.go`, `types.go`, `scanner.go`, `relationships.go`, and
`awssdk/README.md` before editing this service.

## Mandatory Rules

- Keep S3 AWS access behind `Client`; the scanner package must not import the
  AWS SDK.
- Keep S3 claims regional. Do not turn `aws-global` into an unfiltered bucket
  scan.
- Emit reported bucket control-plane metadata and logging-target relationship
  evidence only.
- Do not read objects, list object keys or versions, read bucket policy JSON,
  ACL grants, lifecycle rules, replication rules, notifications, inventory,
  analytics, metrics, or mutation results.
- Do not infer workload, environment, repository, ownership, or deployable-unit
  truth from bucket names, tags, website status, or logging targets.
- Keep bucket names, ARNs, tags, prefixes, KMS IDs, object keys, raw AWS
  errors, and page tokens out of metric labels.
