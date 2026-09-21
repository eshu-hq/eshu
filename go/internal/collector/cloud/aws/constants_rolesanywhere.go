// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceRolesAnywhere identifies the regional AWS IAM Roles Anywhere
	// metadata-only scan slice. The scanner reads trust-anchor, profile, and
	// certificate-revocation-list (CRL) control-plane metadata through the
	// rolesanywhere management APIs (ListTrustAnchors, ListProfiles, ListCrls,
	// ListTagsForResource). It never reads or persists certificate private
	// material, PEM certificate bundles, CRL bodies, session policy documents, or
	// vended session credentials, and never mutates Roles Anywhere state.
	ServiceRolesAnywhere = "rolesanywhere"
)

const (
	// ResourceTypeRolesAnywhereTrustAnchor identifies an AWS IAM Roles Anywhere
	// trust anchor metadata resource. The scanner emits identity, enabled state,
	// and the trust-anchor source type (AWS_ACM_PCA, CERTIFICATE_BUNDLE, or
	// SELF_SIGNED_REPOSITORY) only. The PEM x509 certificate bundle and ACM PCA
	// root certificate material stay outside the contract.
	ResourceTypeRolesAnywhereTrustAnchor = "aws_rolesanywhere_trust_anchor"
	// ResourceTypeRolesAnywhereProfile identifies an AWS IAM Roles Anywhere
	// profile metadata resource. The scanner emits identity, enabled state,
	// session duration, the assumable IAM role and managed-policy ARNs, and
	// boolean flags only. The inline session policy document and certificate
	// attribute-mapping rules stay outside the contract.
	ResourceTypeRolesAnywhereProfile = "aws_rolesanywhere_profile"
	// ResourceTypeRolesAnywhereCRL identifies an AWS IAM Roles Anywhere imported
	// certificate revocation list (CRL) metadata resource. The scanner emits
	// identity, enabled state, and the associated trust-anchor ARN only. The
	// CRL body bytes are never read or persisted.
	ResourceTypeRolesAnywhereCRL = "aws_rolesanywhere_crl"
)

const (
	// RelationshipRolesAnywhereProfileAssumesRole records an AWS IAM Roles
	// Anywhere profile's reported dependency on an IAM role it can vend session
	// credentials for. The target is keyed by the role ARN, which matches how the
	// IAM scanner publishes its role resource_id, so the edge joins the IAM role
	// node instead of dangling.
	RelationshipRolesAnywhereProfileAssumesRole = "rolesanywhere_profile_assumes_role"
	// RelationshipRolesAnywhereTrustAnchorUsesACMPCA records an AWS IAM Roles
	// Anywhere trust anchor's reported dependency on an AWS Private CA (ACM PCA)
	// certificate authority. It is emitted only for trust anchors whose source is
	// AWS_ACM_PCA. The target is keyed by the CA ARN, which matches how the
	// acmpca scanner publishes its certificate-authority resource_id.
	RelationshipRolesAnywhereTrustAnchorUsesACMPCA = "rolesanywhere_trust_anchor_uses_acmpca"
	// RelationshipRolesAnywhereCRLValidatesTrustAnchor records that an imported
	// certificate revocation list (CRL) provides revocation for a trust anchor.
	// The target is keyed by the trust-anchor ARN, which is the resource_id the
	// trust-anchor node publishes, so the edge joins an internal scanner node.
	RelationshipRolesAnywhereCRLValidatesTrustAnchor = "rolesanywhere_crl_validates_trust_anchor"
)
