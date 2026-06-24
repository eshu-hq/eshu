// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package verifiedpermissions

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// Client snapshots metadata-only Amazon Verified Permissions policy store,
// policy, and identity source observations for one AWS claim. Implementations
// read control-plane metadata through the verifiedpermissions management APIs
// and never read or persist Cedar policy statement bodies, schema bodies,
// policy template bodies, or authorization-request payloads.
type Client interface {
	// Snapshot returns every Verified Permissions policy store visible to the
	// configured AWS credentials, each carrying the policies and identity
	// sources that live under it.
	Snapshot(ctx context.Context) (Snapshot, error)
}

// Snapshot captures Verified Permissions policy store metadata plus non-fatal
// scan warnings.
type Snapshot struct {
	// PolicyStores is the metadata-only set of Verified Permissions policy
	// stores, each carrying its policies and identity sources.
	PolicyStores []PolicyStore
	// Warnings carries non-fatal partial-scan observations such as sustained
	// throttling that omitted a metadata component.
	Warnings []awscloud.WarningObservation
}

// PolicyStore is the scanner-owned Verified Permissions policy store model. It
// carries control-plane metadata only and intentionally excludes the Cedar
// schema body and any policy statement body.
type PolicyStore struct {
	// ARN is the Amazon Resource Name that uniquely identifies the policy store.
	ARN string
	// ID is the unique policy store identifier (for example
	// PSEXAMPLEabcdefg111111).
	ID string
	// Description is the optional descriptive text for the policy store.
	Description string
	// ValidationMode is the policy store validation mode (OFF or STRICT). The
	// validation schema body is never read.
	ValidationMode string
	// DeletionProtection reports whether the policy store is protected from
	// deletion (ENABLED or DISABLED).
	DeletionProtection string
	// EncryptionState reports the policy store encryption state when
	// GetPolicyStore returns it. The encryption key material is never read.
	EncryptionState string
	// CedarVersion is the Cedar language version configured for the policy
	// store (for example CEDAR_VERSION_4_0).
	CedarVersion string
	// CreatedDate is when the policy store was created.
	CreatedDate time.Time
	// LastUpdatedDate is when the policy store was last updated.
	LastUpdatedDate time.Time
	// Tags carries the policy store resource tags.
	Tags map[string]string
	// Policies are the metadata-only policies that live under this policy store.
	Policies []Policy
	// IdentitySources are the metadata-only identity sources that live under
	// this policy store.
	IdentitySources []IdentitySource
}

// Policy is the scanner-owned Verified Permissions policy model. It carries
// policy identity and classification metadata only and intentionally excludes
// the Cedar policy statement body, principal/resource entity payloads, and any
// data-plane authorization input.
type Policy struct {
	// ID is the policy identifier.
	ID string
	// PolicyStoreID is the identifier of the parent policy store.
	PolicyStoreID string
	// PolicyType is the policy type (STATIC or TEMPLATE_LINKED).
	PolicyType string
	// Effect is the policy decision effect (Permit or Forbid) when AWS reports
	// it for a static policy.
	Effect string
	// CreatedDate is when the policy was created.
	CreatedDate time.Time
	// LastUpdatedDate is when the policy was last updated.
	LastUpdatedDate time.Time
}

// IdentitySource is the scanner-owned Verified Permissions identity source
// model. It carries the identity source identity, its provider kind, and the
// non-secret provider reference (Cognito user pool ARN or OIDC issuer URL)
// only. Application client secrets and token payloads are never read.
type IdentitySource struct {
	// ID is the identity source identifier.
	ID string
	// PolicyStoreID is the identifier of the parent policy store.
	PolicyStoreID string
	// PrincipalEntityType is the Cedar entity type of principals the provider
	// returns.
	PrincipalEntityType string
	// ProviderKind is the configured identity provider kind (cognito or oidc).
	ProviderKind string
	// CognitoUserPoolARN is the Amazon Cognito user pool ARN when the provider
	// is a Cognito user pool. It is empty for an OIDC provider.
	CognitoUserPoolARN string
	// OpenIDIssuer is the OIDC issuer URL when the provider is an OIDC identity
	// source. It is empty for a Cognito provider.
	OpenIDIssuer string
	// ClientIDCount is the count of application client ids associated with the
	// provider. The client id values themselves are not persisted because they
	// reference application credentials.
	ClientIDCount int
	// CreatedDate is when the identity source was created.
	CreatedDate time.Time
	// LastUpdatedDate is when the identity source was last updated.
	LastUpdatedDate time.Time
}
