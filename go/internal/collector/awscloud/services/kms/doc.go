// Package kms maps AWS Key Management Service metadata into AWS cloud
// collector facts.
//
// The package owns scanner-level fact selection for customer master keys,
// AWS-managed keys when AWS makes them listable, aliases, and grants. It
// emits reported evidence only: key identity, usage, origin, manager, key
// state, rotation status, policy revision metadata, alias-to-key edges, and
// grant identity with the bounded operation list. It does not call
// cryptographic operations (Encrypt, Decrypt, GenerateDataKey, Sign, Verify,
// ReEncrypt, GenerateMac, VerifyMac, GenerateDataKeyPair,
// GenerateDataKeyWithoutPlaintext), does not call lifecycle mutation APIs
// (CreateKey, ScheduleKeyDeletion, CancelKeyDeletion, EnableKey, DisableKey,
// PutKeyPolicy, CreateGrant, RevokeGrant, RetireGrant, ReplicateKey,
// ImportKeyMaterial, DeleteImportedKeyMaterial), never persists key policy
// Statement bodies (only the bounded set of policy revision names AWS
// reports), never persists grant encryption contexts, and never persists key
// material.
package kms
