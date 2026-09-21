// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rolesanywhere

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// Client snapshots metadata-only AWS IAM Roles Anywhere observations for one AWS
// claim. Implementations read control-plane metadata through the rolesanywhere
// management APIs and never read certificate private material, PEM certificate
// bundles, CRL bodies, session policy documents, or vended session credentials.
type Client interface {
	// Snapshot returns every Roles Anywhere trust anchor, profile, and imported
	// certificate revocation list visible to the configured AWS credentials.
	Snapshot(ctx context.Context) (Snapshot, error)
}

// Snapshot captures Roles Anywhere control-plane metadata plus non-fatal scan
// warnings. All three collections carry control-plane metadata only.
type Snapshot struct {
	// TrustAnchors is the metadata-only set of Roles Anywhere trust anchors.
	TrustAnchors []TrustAnchor
	// Profiles is the metadata-only set of Roles Anywhere profiles.
	Profiles []Profile
	// CRLs is the metadata-only set of imported certificate revocation lists.
	CRLs []CRL
	// Warnings carries non-fatal partial-scan observations such as sustained
	// throttling that omitted a metadata component.
	Warnings []awscloud.WarningObservation
}

// TrustAnchor is the scanner-owned Roles Anywhere trust anchor model. It carries
// control-plane metadata only and intentionally excludes the PEM x509
// certificate bundle and any certificate private material.
type TrustAnchor struct {
	// ARN is the Amazon Resource Name that uniquely identifies the trust anchor.
	ARN string
	// TrustAnchorID is the unique identifier of the trust anchor.
	TrustAnchorID string
	// Name is the trust anchor name.
	Name string
	// Enabled reports whether the trust anchor is enabled.
	Enabled bool
	// SourceType is the trust anchor source type AWS reports (AWS_ACM_PCA,
	// CERTIFICATE_BUNDLE, or SELF_SIGNED_REPOSITORY). It is configuration
	// metadata, not certificate content.
	SourceType string
	// ACMPCAArn is the ARN of the AWS Private CA (ACM PCA) certificate authority
	// backing an AWS_ACM_PCA trust anchor, when reported. It is empty for
	// certificate-bundle and self-signed trust anchors. The scanner never copies
	// the PEM certificate data carried alongside it.
	ACMPCAArn string
	// CreatedAt is when the trust anchor was created.
	CreatedAt time.Time
	// UpdatedAt is when the trust anchor was last updated.
	UpdatedAt time.Time
	// Tags carries the trust anchor resource tags.
	Tags map[string]string
}

// Profile is the scanner-owned Roles Anywhere profile model. It carries
// control-plane metadata only and intentionally excludes the inline session
// policy document and the certificate attribute-mapping rules.
type Profile struct {
	// ARN is the Amazon Resource Name that uniquely identifies the profile.
	ARN string
	// ProfileID is the unique identifier of the profile.
	ProfileID string
	// Name is the profile name.
	Name string
	// Enabled reports whether the profile is enabled.
	Enabled bool
	// DurationSeconds is how long sessions vended using this profile are valid
	// for, when reported.
	DurationSeconds int32
	// AcceptRoleSessionName reports whether a custom role session name is accepted
	// in a temporary credential request.
	AcceptRoleSessionName bool
	// RequireInstanceProperties reports whether instance properties are required
	// in temporary credential requests using this profile.
	RequireInstanceProperties bool
	// HasSessionPolicy reports whether an inline session policy is configured. The
	// policy document body itself is never persisted.
	HasSessionPolicy bool
	// AttributeMappingCount is the number of certificate attribute mappings
	// configured. The mapping rule contents are never persisted.
	AttributeMappingCount int
	// RoleARNs are the IAM role ARNs this profile can assume in a temporary
	// credential request.
	RoleARNs []string
	// ManagedPolicyARNs are the managed policy ARNs applied to the vended session
	// credentials.
	ManagedPolicyARNs []string
	// CreatedAt is when the profile was created.
	CreatedAt time.Time
	// UpdatedAt is when the profile was last updated.
	UpdatedAt time.Time
	// Tags carries the profile resource tags.
	Tags map[string]string
}

// CRL is the scanner-owned Roles Anywhere certificate revocation list model. It
// carries control-plane metadata only and intentionally excludes the CRL body
// bytes.
type CRL struct {
	// ARN is the Amazon Resource Name that uniquely identifies the CRL.
	ARN string
	// CRLID is the unique identifier of the CRL.
	CRLID string
	// Name is the CRL name.
	Name string
	// Enabled reports whether the CRL is enabled.
	Enabled bool
	// TrustAnchorARN is the ARN of the trust anchor this CRL provides revocation
	// for, when reported.
	TrustAnchorARN string
	// CreatedAt is when the CRL was created.
	CreatedAt time.Time
	// UpdatedAt is when the CRL was last updated.
	UpdatedAt time.Time
	// Tags carries the CRL resource tags.
	Tags map[string]string
}
