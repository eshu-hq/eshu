# AGENTS.md - internal/collector/awscloud/services/codecommit/awssdk guidance

## Read First

1. `README.md` - adapter purpose, exported surface, and invariants.
2. `client.go` - the `apiClient` read interface and SDK mapping.
3. `exclusion_test.go` - the metadata-only reflection guard.
4. `../README.md` - the scanner contract this adapter serves.

## Invariants

- Metadata only. The `apiClient` interface must expose only `ListRepositories`,
  `BatchGetRepositories`, `GetRepositoryTriggers`, and `ListTagsForResource`.
  Never add a commit, ref, blob, file-content (`GetFile`/`GetBlob`/`GetFolder`),
  pull-request, comment, or mutation method. The exclusion reflection guard
  fails the build if any forbidden method is added; keep it green.
- Chunk `BatchGetRepositories` to the AWS 25-name limit (`batchRepositoryLimit`).
- Map every SDK ARN through directly; never synthesize an ARN here.
- Keep repository ARNs, clone URLs, tags, KMS key ids, and raw AWS error
  payloads out of metric labels; only the bounded telemetry attributes
  (service, account, region, operation, result) are allowed.
- Translate AWS records into scanner-owned `codecommit` types; do not leak SDK
  types past this package's boundary.

## Common Changes

- Add a metadata field by mapping it in `mapRepository`/`mapTriggers` from the
  existing read responses, with a `client_test.go` assertion.
- Add a new bounded read only when it returns repository metadata (never
  content), update the `apiClient` interface and the exclusion-test allowlist
  together, and add telemetry via `recordAPICall`.

## What Not To Change Without An ADR

- Do not widen the read surface to commit, ref, blob, file-content,
  pull-request, or comment reads.
- Do not add mutation calls of any kind.
- Do not load credentials or call STS here; the runtime provides `aws.Config`.
