# AGENTS.md - internal/collector/awscloud/services/kms guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - scanner-owned KMS domain types. The `Client` interface here
   defines the entire SDK surface the scanner is allowed to touch.
3. `scanner.go` - key, alias, and grant emission with the alias and grant
   relationship edges.
4. `../../README.md` - shared AWS cloud observation and envelope contract.
5. `docs/public/services/collector-aws-cloud.md` - AWS collector runtime
   requirements.
6. `docs/public/services/collector-aws-cloud-scanners.md` - scanner
   coverage and data boundary.

## Invariants

- Keep KMS API access behind `Client`; do not import the AWS SDK into
  this package.
- Never call any cryptographic operation: Encrypt, Decrypt,
  GenerateDataKey, GenerateDataKeyPair,
  GenerateDataKeyPairWithoutPlaintext, GenerateDataKeyWithoutPlaintext,
  Sign, Verify, ReEncrypt, GenerateMac, VerifyMac, DeriveSharedSecret,
  GetPublicKey, GenerateRandom.
- Never call any lifecycle mutation: CreateKey, ScheduleKeyDeletion,
  CancelKeyDeletion, EnableKey, DisableKey, EnableKeyRotation,
  DisableKeyRotation, PutKeyPolicy, CreateGrant, RevokeGrant,
  RetireGrant, ReplicateKey, ImportKeyMaterial,
  DeleteImportedKeyMaterial, UpdateKeyDescription, CreateAlias,
  UpdateAlias, DeleteAlias, TagResource, UntagResource,
  RotateKeyOnDemand, UpdatePrimaryRegion.
- Never persist key policy Statement bodies. Only the bounded list of
  policy revision names from ListKeyPolicies is emitted. The scanner
  does not call GetKeyPolicy.
- Never persist grant encryption contexts. The scanner-owned `Grant`
  type has no field for `EncryptionContextSubset`,
  `EncryptionContextEquals`, or other `GrantConstraints` payload.
- Never persist key material.
- Emit reported evidence only. Do not infer workload ownership,
  environment, repository, or deployable-unit truth from key
  descriptions, tags, aliases, or grantee principals.
- Preserve stable key, alias, and grant identities across repeated
  observations in the same AWS generation.
- Keep key ids, ARNs, aliases, grantee principals, and tags out of
  metric labels.

## Common Changes

- Add a new safe KMS metadata field by extending the scanner-owned
  type, writing a focused scanner or adapter test first, then mapping it
  through `awscloud` envelope builders.
- Add new relationship evidence only when KMS directly reports both
  sides as identity (not as policy Statement principals; principals
  inferred from policy text are out of scope).
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not add any cryptographic or lifecycle mutation API to `Client`.
  The package test refuses to compile if such a method name appears.
- Do not call GetKeyPolicy. Policy Statement bodies expose the org's
  IAM model and must stay outside the collector contract.
- Do not persist grant encryption contexts. They can carry tenant or
  workload metadata.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
