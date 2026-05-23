# AGENTS.md - services/s3/awssdk

Read `README.md`, `doc.go`, `client.go`, `mapper.go`, and `../README.md`
before editing this adapter.

## Mandatory Rules

- Allowed calls are `ListBuckets`, `GetBucketTagging`, `GetBucketVersioning`,
  `GetBucketEncryption`, `GetPublicAccessBlock`, `GetBucketPolicyStatus`,
  `GetBucketOwnershipControls`, `GetBucketWebsite`, and `GetBucketLogging`.
- Set `MaxBuckets` before relying on `ContinuationToken`; wrap every page and
  point read in `recordAPICall`.
- Do not call object, object-version, inventory, policy JSON, ACL grant,
  lifecycle, replication, notification, analytics, metrics, mutation,
  credential, STS, graph, or reducer APIs.
- Add optional-not-configured or throttle handling only from AWS or Smithy
  evidence and cover it with adapter tests.
- Keep bucket names, ARNs, tags, prefixes, KMS IDs, object keys, page tokens,
  and raw AWS errors out of metric labels.
