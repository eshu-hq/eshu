# AGENTS.md - services/kms/awssdk guidance

## Read First

1. `README.md` - SDK adapter contract and forbidden API list.
2. `client.go` - pagination, point reads, and the `apiClient` interface.
3. `mappers.go` - SDK-to-scanner-owned mapping. The `mapGrant` function
   intentionally drops `GrantConstraints` so encryption contexts never
   reach the scanner type.
4. `telemetry.go` - shared AWS API call instruments and error
   classification.
5. `../README.md` - KMS scanner contract.

## Invariants

- The `apiClient` interface is the entire AWS SDK surface this package
  consumes. Do not add any method whose name matches a cryptographic
  operation (Encrypt, Decrypt, GenerateDataKey*, Sign, Verify, ReEncrypt,
  GenerateMac, VerifyMac, DeriveSharedSecret, GetPublicKey,
  GenerateRandom) or any key lifecycle mutation (CreateKey,
  ScheduleKeyDeletion, CancelKeyDeletion, EnableKey, DisableKey,
  EnableKeyRotation, DisableKeyRotation, PutKeyPolicy, CreateGrant,
  RevokeGrant, RetireGrant, ReplicateKey, ImportKeyMaterial,
  DeleteImportedKeyMaterial, UpdateKeyDescription, CreateAlias,
  UpdateAlias, DeleteAlias, TagResource, UntagResource,
  RotateKeyOnDemand, UpdatePrimaryRegion).
- Do not call `GetKeyPolicy`. Policy Statement bodies expose the org's
  IAM model and are out of contract.
- Do not propagate `GrantConstraints.EncryptionContext*` pairs.
- Treat `UnsupportedOperationException` from `GetKeyRotationStatus` as
  "rotation status unknown", not as a scan failure.
- Treat `AccessDeniedException` from `ListResourceTags` as "no tags
  reported" rather than failing the scan.
- Keep raw AWS error payloads out of metric labels.

## Common Changes

- Add new safe paginated metadata reads by extending the `apiClient`
  interface with a List/Describe-class method, mirroring the existing
  pagination shape, and writing a fake-client test first.
- Update the `mapKey`/`mapGrant` translation only when the scanner-owned
  type changes.

## What Not To Change Without An ADR

- Do not add any method whose name matches a forbidden operation.
- Do not add additional KMS API surfaces (cryptographic, lifecycle, or
  GetKeyPolicy) without an active design record.
- Do not add S3 or external storage reads from this package.
