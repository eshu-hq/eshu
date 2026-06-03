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
- The scanner MAY read the key policy (the `awssdk` adapter calls
  GetKeyPolicy, owner-approved reversal, PR4b of #1134) to emit the
  normalized, derived `aws_resource_policy_permission` fact: per
  statement, the effect, normalized action/resource patterns,
  condition key/operator NAMES, and derived grantee facts (principal account ids,
  principal ARNs, principal types, public, cross-account) — the
  resource-side analog of `aws_iam_permission`. The adapter parses the
  policy transiently and hands this package only the derived
  `Key.ResourcePolicyStatements`; this package never sees the raw policy
  document.
- Never persist key policy Statement bodies, statement Sids, or condition
  VALUES. The bounded list of policy revision names from ListKeyPolicies
  is still emitted as before; normalized/derived policy actions,
  resources, and condition key/operator NAMES are allowed via
  `aws_resource_policy_permission`, but raw statement bodies and condition
  values are not.
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
  inferred from policy text are out of scope as graph edges — the
  metadata-only `aws_resource_policy_permission` fact is the only sink for
  derived policy-statement principal facts, and it emits no graph edge).
- Extend `aws_resource_policy_permission` only with normalized/derived
  statement metadata already derived on `Key.ResourcePolicyStatements`.
  The derivation lives in the `awssdk` adapter
  (`deriveKeyPolicyResourcePermissionStatements`); never add raw statement
  bodies or condition values to the statement or the fact. This fact is
  the facts foundation for resource-policy-aware CAN_PERFORM (a later
  reducer follow-up); do not add a graph edge or reducer projection here.
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not add any cryptographic or lifecycle mutation API to `Client`.
  The package test refuses to compile if such a method name appears.
  GetKeyPolicy (a read) is allowed and pinned by the adapter test; the
  policy-mutating PutKeyPolicy remains forbidden.
- GetKeyPolicy MAY be called to derive the normalized, metadata-only
  `aws_resource_policy_permission` fact (owner-approved, PR4b of #1134).
  Do not persist the raw policy Statement bodies or condition values that
  it returns: only the derived effect, normalized actions/resources,
  condition key/operator NAMES, and grantee principal facts may leave the adapter.
- Do not persist grant encryption contexts. They can carry tenant or
  workload metadata.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
