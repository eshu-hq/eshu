package kms

import "context"

// Client is the KMS metadata read surface consumed by Scanner. The interface
// intentionally exposes List/Describe-class methods only; cryptographic
// operations (Encrypt, Decrypt, GenerateDataKey, Sign, Verify, ReEncrypt,
// GenerateMac, VerifyMac, GenerateDataKeyPair, GenerateDataKeyWithoutPlaintext)
// and lifecycle mutations (CreateKey, ScheduleKeyDeletion, CancelKeyDeletion,
// EnableKey, DisableKey, PutKeyPolicy, CreateGrant, RevokeGrant, RetireGrant,
// ReplicateKey, ImportKeyMaterial, DeleteImportedKeyMaterial) are not part of
// the contract.
type Client interface {
	// ListKeys returns one snapshot per visible KMS key. Implementations
	// resolve identity, usage, origin, manager, key state, rotation status,
	// policy revision metadata, aliases, and grants without persisting key
	// policy Statement bodies or grant encryption contexts.
	ListKeys(context.Context) ([]Key, error)
}

// Key is the scanner-owned representation of one KMS key. It carries
// metadata only; key material, plaintext data, and key policy Statement
// bodies are intentionally outside the contract.
type Key struct {
	// ID is the AWS-assigned KeyId (a UUID-shaped string).
	ID string
	// ARN is the canonical KMS key ARN.
	ARN string
	// Description is the operator-supplied key description.
	Description string
	// KeyManager is "AWS" or "CUSTOMER" as reported by DescribeKey.
	KeyManager string
	// KeyUsage names the allowed cryptographic operation kind
	// (ENCRYPT_DECRYPT, SIGN_VERIFY, GENERATE_VERIFY_MAC). It is metadata
	// describing what the key is for, not an invocation of those operations.
	KeyUsage string
	// KeySpec names the algorithm family (SYMMETRIC_DEFAULT, RSA_*, ECC_*,
	// HMAC_*, SM2).
	KeySpec string
	// KeyState reports the key lifecycle state (Enabled, Disabled,
	// PendingDeletion, PendingReplicaDeletion, Unavailable, etc.).
	KeyState string
	// Origin reports key material origin (AWS_KMS, EXTERNAL,
	// AWS_CLOUDHSM, EXTERNAL_KEY_STORE).
	Origin string
	// CreationDate is the reported CreationDate as an RFC 3339 string.
	CreationDate string
	// DeletionDate is the scheduled DeletionDate as an RFC 3339 string when
	// the key is pending deletion; empty otherwise.
	DeletionDate string
	// Enabled reports whether the key is currently usable, mirroring
	// DescribeKey's Enabled flag without invoking any cryptographic
	// operation.
	Enabled bool
	// MultiRegion is true when the key is a multi-region key.
	MultiRegion bool
	// MultiRegionKeyType is "PRIMARY" or "REPLICA" when MultiRegion is true.
	MultiRegionKeyType string
	// PrimaryKeyARN is the primary key's ARN when this key is a multi-region
	// replica; empty otherwise.
	PrimaryKeyARN string
	// CustomerMasterKeySpec mirrors KeySpec for older fixtures that still
	// surface CustomerMasterKeySpec; empty when the AWS API does not report
	// it.
	CustomerMasterKeySpec string
	// EncryptionAlgorithms is the bounded list of encryption algorithm names
	// the key supports.
	EncryptionAlgorithms []string
	// SigningAlgorithms is the bounded list of signing algorithm names the
	// key supports.
	SigningAlgorithms []string
	// MACAlgorithms is the bounded list of MAC algorithm names the key
	// supports.
	MACAlgorithms []string
	// KeyAgreementAlgorithms is the bounded list of key-agreement algorithm
	// names the key supports.
	KeyAgreementAlgorithms []string
	// RotationEnabled reports whether automatic key rotation is enabled, as
	// reported by GetKeyRotationStatus. It is omitted for keys that do not
	// support rotation.
	RotationEnabled bool
	// RotationStatusKnown is true when GetKeyRotationStatus returned a
	// definitive answer; false when the key type does not support rotation
	// status reads.
	RotationStatusKnown bool
	// PolicyRevisionNames lists policy names attached to the key (the
	// stable "default" identifier and any other policy revision name AWS
	// reports). Policy Statement bodies are never persisted.
	PolicyRevisionNames []string
	// Tags is the raw AWS tag set on the key.
	Tags map[string]string
	// Aliases is the bounded list of aliases that point at this key.
	Aliases []Alias
	// Grants is the bounded list of grants attached to this key. Each grant
	// carries identity and the bounded operation list; encryption contexts
	// are excluded.
	Grants []Grant
}

// Alias is the scanner-owned representation of one KMS alias.
type Alias struct {
	// Name is the alias name (for example "alias/my-app-key"). KMS aliases
	// are stable names; they carry no key material.
	Name string
	// ARN is the canonical alias ARN.
	ARN string
	// TargetKeyID is the AWS-assigned KeyId the alias points at.
	TargetKeyID string
	// LastUpdated is the alias last-updated timestamp as an RFC 3339 string.
	LastUpdated string
}

// Grant is the scanner-owned representation of one KMS grant. It records
// identity and authorization scope as reported by ListGrants. Encryption
// contexts and constraint expressions are excluded by contract because they
// can carry tenant or workload metadata the collector must not persist.
type Grant struct {
	// ID is the grant identifier reported by KMS.
	ID string
	// Name is the optional grant name; empty when not set.
	Name string
	// CreationDate is the reported CreationDate as an RFC 3339 string.
	CreationDate string
	// GranteePrincipal is the ARN-shaped principal authorized to use the
	// key. It is emitted as-is for relationship evidence; trust evaluation
	// is a downstream reducer concern.
	GranteePrincipal string
	// RetiringPrincipal is the ARN-shaped principal authorized to retire the
	// grant; empty when not set.
	RetiringPrincipal string
	// IssuingAccount is the AWS account that issued the grant.
	IssuingAccount string
	// Operations is the bounded list of operation names the grant permits
	// (for example Encrypt, Decrypt, GenerateDataKey, DescribeKey). These
	// names are descriptive metadata; the scanner does not invoke them.
	Operations []string
}
