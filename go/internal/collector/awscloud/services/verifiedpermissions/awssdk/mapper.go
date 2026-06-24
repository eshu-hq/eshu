// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsvp "github.com/aws/aws-sdk-go-v2/service/verifiedpermissions"
	awsvptypes "github.com/aws/aws-sdk-go-v2/service/verifiedpermissions/types"

	vpservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/verifiedpermissions"
)

// mapPolicyStoreItem maps a ListPolicyStores item into the scanner-owned policy
// store metadata. The richer GetPolicyStore detail is layered on later by
// applyPolicyStoreDetail.
func mapPolicyStoreItem(item awsvptypes.PolicyStoreItem) vpservice.PolicyStore {
	return vpservice.PolicyStore{
		ARN:             strings.TrimSpace(aws.ToString(item.Arn)),
		ID:              strings.TrimSpace(aws.ToString(item.PolicyStoreId)),
		Description:     strings.TrimSpace(aws.ToString(item.Description)),
		CreatedDate:     aws.ToTime(item.CreatedDate),
		LastUpdatedDate: aws.ToTime(item.LastUpdatedDate),
	}
}

// applyPolicyStoreDetail layers GetPolicyStore metadata (validation mode,
// deletion protection, encryption kind, Cedar version, tags) onto a store. It
// records only a non-secret encryption-state label; the customer-managed KMS
// key ARN and user-defined encryption context are never persisted.
func applyPolicyStoreDetail(store *vpservice.PolicyStore, detail *awsvp.GetPolicyStoreOutput) {
	if detail == nil {
		return
	}
	if arn := strings.TrimSpace(aws.ToString(detail.Arn)); arn != "" {
		store.ARN = arn
	}
	if id := strings.TrimSpace(aws.ToString(detail.PolicyStoreId)); id != "" {
		store.ID = id
	}
	if description := strings.TrimSpace(aws.ToString(detail.Description)); description != "" {
		store.Description = description
	}
	if detail.ValidationSettings != nil {
		store.ValidationMode = strings.TrimSpace(string(detail.ValidationSettings.Mode))
	}
	store.DeletionProtection = strings.TrimSpace(string(detail.DeletionProtection))
	store.CedarVersion = strings.TrimSpace(string(detail.CedarVersion))
	store.EncryptionState = encryptionStateLabel(detail.EncryptionState)
	if created := aws.ToTime(detail.CreatedDate); !created.IsZero() {
		store.CreatedDate = created
	}
	if updated := aws.ToTime(detail.LastUpdatedDate); !updated.IsZero() {
		store.LastUpdatedDate = updated
	}
	store.Tags = cloneTags(detail.Tags)
}

// encryptionStateLabel returns a non-secret encryption-kind label (DEFAULT or
// KMS) for a policy store. It never returns the customer-managed key ARN or the
// user-defined encryption context, which could carry sensitive material.
func encryptionStateLabel(state awsvptypes.EncryptionState) string {
	switch state.(type) {
	case *awsvptypes.EncryptionStateMemberDefault:
		return "DEFAULT"
	case *awsvptypes.EncryptionStateMemberKmsEncryptionState:
		return "KMS"
	default:
		return ""
	}
}

// mapPolicyItem maps a ListPolicies item into scanner-owned policy metadata. It
// keeps the policy id, type, effect, and parent store id only; the Cedar policy
// statement body and principal/resource entity payloads are never read here
// because ListPolicies does not return them and GetPolicy is excluded from the
// adapter surface.
func mapPolicyItem(item awsvptypes.PolicyItem) vpservice.Policy {
	return vpservice.Policy{
		ID:              strings.TrimSpace(aws.ToString(item.PolicyId)),
		PolicyStoreID:   strings.TrimSpace(aws.ToString(item.PolicyStoreId)),
		PolicyType:      strings.TrimSpace(string(item.PolicyType)),
		Effect:          strings.TrimSpace(string(item.Effect)),
		CreatedDate:     aws.ToTime(item.CreatedDate),
		LastUpdatedDate: aws.ToTime(item.LastUpdatedDate),
	}
}

// mapIdentitySourceItem maps a ListIdentitySources item into scanner-owned
// identity source metadata. It records the provider kind and the non-secret
// provider reference (Cognito user pool ARN or OIDC issuer URL) plus an
// application client id count, never the client id values or any token payload.
func mapIdentitySourceItem(item awsvptypes.IdentitySourceItem) vpservice.IdentitySource {
	source := vpservice.IdentitySource{
		ID:                  strings.TrimSpace(aws.ToString(item.IdentitySourceId)),
		PolicyStoreID:       strings.TrimSpace(aws.ToString(item.PolicyStoreId)),
		PrincipalEntityType: strings.TrimSpace(aws.ToString(item.PrincipalEntityType)),
		CreatedDate:         aws.ToTime(item.CreatedDate),
		LastUpdatedDate:     aws.ToTime(item.LastUpdatedDate),
	}
	applyConfiguration(&source, item.Configuration)
	if source.ProviderKind == "" {
		// item.Details is deprecated in favor of item.Configuration, but AWS still
		// returns it for identity sources created before the configuration union
		// existed. Reading it preserves the Cognito user pool edge for those legacy
		// sources; dropping it would silently miss that join. The fallback only runs
		// when the current Configuration union was empty.
		applyDeprecatedDetails(&source, item.Details) //nolint:staticcheck // deprecated field still populated for legacy identity sources; metadata-only read.
	}
	return source
}

// applyConfiguration reads the identity source provider configuration union and
// records the provider kind plus the non-secret provider reference. Application
// client id values are never persisted; only their count is recorded.
func applyConfiguration(source *vpservice.IdentitySource, configuration awsvptypes.ConfigurationItem) {
	switch config := configuration.(type) {
	case *awsvptypes.ConfigurationItemMemberCognitoUserPoolConfiguration:
		source.ProviderKind = "cognito"
		source.CognitoUserPoolARN = strings.TrimSpace(aws.ToString(config.Value.UserPoolArn))
		source.ClientIDCount = len(config.Value.ClientIds)
	case *awsvptypes.ConfigurationItemMemberOpenIdConnectConfiguration:
		source.ProviderKind = "oidc"
		source.OpenIDIssuer = strings.TrimSpace(aws.ToString(config.Value.Issuer))
	}
}

// applyDeprecatedDetails populates the Cognito user pool reference from the
// deprecated IdentitySourceItemDetails struct for older accounts that still
// report identity sources without the configuration union. It records only the
// user pool ARN and an application client id count, never the client id values.
func applyDeprecatedDetails(source *vpservice.IdentitySource, details *awsvptypes.IdentitySourceItemDetails) {
	if details == nil {
		return
	}
	if arn := strings.TrimSpace(aws.ToString(details.UserPoolArn)); arn != "" { //nolint:staticcheck // deprecated field still populated for legacy identity sources; metadata-only read.
		source.ProviderKind = "cognito"
		source.CognitoUserPoolARN = arn
		source.ClientIDCount = len(details.ClientIds) //nolint:staticcheck // deprecated field still populated for legacy identity sources; metadata-only read.
	}
}

// cloneTags returns a trimmed-key copy of the policy store tags, or nil when
// empty.
func cloneTags(input map[string]string) map[string]string {
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
