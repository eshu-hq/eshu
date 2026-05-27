package awssdk

import (
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	kmstypes "github.com/aws/aws-sdk-go-v2/service/kms/types"

	kmsservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/kms"
)

// mapKey converts the AWS SDK KMS describe-and-list call results into the
// scanner-owned Key. Encryption-context grant constraints are intentionally
// dropped; only identity, usage, origin, manager, key state, rotation
// status, policy revision names, aliases, and grant identity flow forward.
func mapKey(
	keyID string,
	metadata *kmstypes.KeyMetadata,
	aliases []kmsservice.Alias,
	grants []kmsservice.Grant,
	tags map[string]string,
	policyNames []string,
	rotation rotationStatus,
) kmsservice.Key {
	if metadata == nil {
		metadata = &kmstypes.KeyMetadata{}
	}
	return kmsservice.Key{
		ID:                     firstNonEmpty(strings.TrimSpace(aws.ToString(metadata.KeyId)), keyID),
		ARN:                    strings.TrimSpace(aws.ToString(metadata.Arn)),
		Description:            strings.TrimSpace(aws.ToString(metadata.Description)),
		KeyManager:             string(metadata.KeyManager),
		KeyUsage:               string(metadata.KeyUsage),
		KeySpec:                string(metadata.KeySpec),
		KeyState:               string(metadata.KeyState),
		Origin:                 string(metadata.Origin),
		CreationDate:           formatTime(metadata.CreationDate),
		DeletionDate:           formatTime(metadata.DeletionDate),
		Enabled:                metadata.Enabled,
		MultiRegion:            aws.ToBool(metadata.MultiRegion),
		MultiRegionKeyType:     multiRegionKeyType(metadata.MultiRegionConfiguration),
		PrimaryKeyARN:          primaryKeyARN(metadata.MultiRegionConfiguration),
		// CustomerMasterKeySpec is deprecated upstream in favor of
		// KeySpec; we already populate KeySpec above and leave the legacy
		// alias empty rather than reach for the deprecated AWS field.
		CustomerMasterKeySpec:  "",
		EncryptionAlgorithms:   encryptionAlgorithms(metadata.EncryptionAlgorithms),
		SigningAlgorithms:      signingAlgorithms(metadata.SigningAlgorithms),
		MACAlgorithms:          macAlgorithms(metadata.MacAlgorithms),
		KeyAgreementAlgorithms: keyAgreementAlgorithms(metadata.KeyAgreementAlgorithms),
		RotationEnabled:        rotation.enabled,
		RotationStatusKnown:    rotation.known,
		PolicyRevisionNames:    cloneStrings(policyNames),
		Tags:                   cloneStringMap(tags),
		Aliases:                cloneAliases(aliases),
		Grants:                 grants,
	}
}

// mapGrant converts one KMS GrantListEntry into a scanner-owned Grant.
// entry.Constraints (encryption context pairs and SourceArn) is intentionally
// dropped because encryption contexts can carry tenant or workload metadata
// the scanner contract forbids persisting.
func mapGrant(entry kmstypes.GrantListEntry) kmsservice.Grant {
	return kmsservice.Grant{
		ID:                strings.TrimSpace(aws.ToString(entry.GrantId)),
		Name:              strings.TrimSpace(aws.ToString(entry.Name)),
		CreationDate:      formatTime(entry.CreationDate),
		GranteePrincipal:  firstNonEmpty(strings.TrimSpace(aws.ToString(entry.GranteePrincipal)), strings.TrimSpace(aws.ToString(entry.GranteeServicePrincipal))),
		RetiringPrincipal: firstNonEmpty(strings.TrimSpace(aws.ToString(entry.RetiringPrincipal)), strings.TrimSpace(aws.ToString(entry.RetiringServicePrincipal))),
		IssuingAccount:    strings.TrimSpace(aws.ToString(entry.IssuingAccount)),
		Operations:        grantOperations(entry.Operations),
	}
}

func grantOperations(operations []kmstypes.GrantOperation) []string {
	if len(operations) == 0 {
		return nil
	}
	output := make([]string, 0, len(operations))
	for _, operation := range operations {
		if trimmed := strings.TrimSpace(string(operation)); trimmed != "" {
			output = append(output, trimmed)
		}
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func encryptionAlgorithms(values []kmstypes.EncryptionAlgorithmSpec) []string {
	if len(values) == 0 {
		return nil
	}
	output := make([]string, 0, len(values))
	for _, value := range values {
		output = append(output, string(value))
	}
	return output
}

func signingAlgorithms(values []kmstypes.SigningAlgorithmSpec) []string {
	if len(values) == 0 {
		return nil
	}
	output := make([]string, 0, len(values))
	for _, value := range values {
		output = append(output, string(value))
	}
	return output
}

func macAlgorithms(values []kmstypes.MacAlgorithmSpec) []string {
	if len(values) == 0 {
		return nil
	}
	output := make([]string, 0, len(values))
	for _, value := range values {
		output = append(output, string(value))
	}
	return output
}

func keyAgreementAlgorithms(values []kmstypes.KeyAgreementAlgorithmSpec) []string {
	if len(values) == 0 {
		return nil
	}
	output := make([]string, 0, len(values))
	for _, value := range values {
		output = append(output, string(value))
	}
	return output
}

func multiRegionKeyType(config *kmstypes.MultiRegionConfiguration) string {
	if config == nil {
		return ""
	}
	return string(config.MultiRegionKeyType)
}

func primaryKeyARN(config *kmstypes.MultiRegionConfiguration) string {
	if config == nil || config.PrimaryKey == nil {
		return ""
	}
	return strings.TrimSpace(aws.ToString(config.PrimaryKey.Arn))
}

// rotationCheckSupported reports whether GetKeyRotationStatus is meaningful
// for this key. AWS returns UnsupportedOperationException for asymmetric,
// HMAC, AWS-managed, and pending-deletion keys. We avoid the API call in
// those cases to keep noise out of the throttle/error counters.
func rotationCheckSupported(metadata *kmstypes.KeyMetadata) bool {
	if metadata == nil {
		return false
	}
	if string(metadata.KeyManager) != string(kmstypes.KeyManagerTypeCustomer) {
		return false
	}
	if metadata.KeyUsage != kmstypes.KeyUsageTypeEncryptDecrypt {
		return false
	}
	if metadata.KeySpec != kmstypes.KeySpecSymmetricDefault {
		return false
	}
	switch metadata.KeyState {
	case kmstypes.KeyStatePendingDeletion,
		kmstypes.KeyStatePendingReplicaDeletion,
		kmstypes.KeyStateUnavailable,
		kmstypes.KeyStateCreating:
		return false
	}
	return true
}

type rotationStatus struct {
	known   bool
	enabled bool
	err     error
}

func formatTime(value *time.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func cloneStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, 0, len(input))
	for _, value := range input {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			output = append(output, trimmed)
		}
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		output[trimmed] = value
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func cloneAliases(input []kmsservice.Alias) []kmsservice.Alias {
	if len(input) == 0 {
		return nil
	}
	output := make([]kmsservice.Alias, len(input))
	copy(output, input)
	return output
}
