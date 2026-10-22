// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

import awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"

// Security-category constants consolidated from constants_iam.go (#6696).

const (
	// ServiceIAM identifies the global IAM service scan slice.
	ServiceIAM = "iam"
)

const (
	// ResourceTypeIAMRole identifies an IAM role.
	ResourceTypeIAMRole = awsv1.ResourceTypeIAMRole
	// ResourceTypeIAMUser identifies an IAM user.
	ResourceTypeIAMUser = awsv1.ResourceTypeIAMUser
	// ResourceTypeIAMGroup identifies an IAM group.
	ResourceTypeIAMGroup = awsv1.ResourceTypeIAMGroup
	// ResourceTypeIAMPolicy identifies an IAM policy.
	ResourceTypeIAMPolicy = awsv1.ResourceTypeIAMPolicy
	// ResourceTypeIAMInstanceProfile identifies an IAM instance profile.
	ResourceTypeIAMInstanceProfile = awsv1.ResourceTypeIAMInstanceProfile
	// ResourceTypeIAMPrincipal identifies a principal from an IAM trust policy.
	ResourceTypeIAMPrincipal = awsv1.ResourceTypeIAMPrincipal
)

const (
	// IAMPolicySourceInline marks a derived permission from an inline policy
	// embedded on a role, user, or group.
	IAMPolicySourceInline = "inline"
	// IAMPolicySourceAttachedManaged marks a derived permission from an attached
	// managed policy document (customer- or AWS-managed).
	IAMPolicySourceAttachedManaged = "attached_managed"
	// IAMPolicySourceTrust marks a derived permission from a role trust /
	// assume-role policy document.
	IAMPolicySourceTrust = "trust"
	// IAMPolicySourcePermissionBoundary marks a derived permission from a managed
	// policy attached as a permissions boundary. It is a ceiling, not an identity
	// grant source.
	IAMPolicySourcePermissionBoundary = "permission_boundary"
)

const (
	// RelationshipIAMRoleTrustsPrincipal records a role trust-policy principal.
	RelationshipIAMRoleTrustsPrincipal = "iam_role_trusts_principal"
	// RelationshipIAMRoleAttachedPolicy records a managed policy attachment.
	RelationshipIAMRoleAttachedPolicy = "iam_role_attached_policy"
	// RelationshipIAMRoleInInstanceProfile records a role/profile membership.
	RelationshipIAMRoleInInstanceProfile = "iam_role_in_instance_profile"
)

// Security-category constants consolidated from constants_kms.go (#6696).

const (
	// ServiceKMS identifies the regional AWS Key Management Service
	// metadata-only scan slice. The scanner never invokes cryptographic
	// operations (Encrypt, Decrypt, GenerateDataKey, Sign, Verify, ReEncrypt,
	// GenerateMac, VerifyMac, GenerateDataKeyPair,
	// GenerateDataKeyWithoutPlaintext) and never invokes key lifecycle
	// mutation APIs (CreateKey, ScheduleKeyDeletion, CancelKeyDeletion,
	// EnableKey, DisableKey, PutKeyPolicy, CreateGrant, RevokeGrant,
	// RetireGrant, ReplicateKey, ImportKeyMaterial, DeleteImportedKeyMaterial).
	ServiceKMS = "kms"
)

const (
	// ResourceTypeKMSKey identifies a KMS customer master key metadata
	// resource. The scanner emits identity, usage, origin, manager, key
	// state, rotation status, and policy revision metadata only; key policy
	// Statement bodies and key material are never persisted.
	ResourceTypeKMSKey = "aws_kms_key"
	// ResourceTypeKMSAlias identifies a KMS alias metadata resource. Aliases
	// are name-only pointers; no key material or policy text is carried.
	ResourceTypeKMSAlias = "aws_kms_alias"
	// ResourceTypeKMSGrant identifies a KMS grant metadata resource. The
	// scanner emits grant identity, grantee principal, retiring principal,
	// and the bounded list of allowed operations. Grant encryption contexts
	// are never persisted.
	ResourceTypeKMSGrant = "aws_kms_grant"
)

const (
	// RelationshipKMSAliasTargetsKey records a KMS alias pointing at its
	// target key.
	RelationshipKMSAliasTargetsKey = "kms_alias_targets_key"
	// RelationshipKMSGrantOnKey records a KMS grant attached to its target
	// key.
	RelationshipKMSGrantOnKey = "kms_grant_on_key"
	// RelationshipKMSGrantForGrantee records the grantee principal reported
	// by a KMS grant. The principal is either an IAM ARN or an AWS service
	// principal and is emitted with the IAM principal scheme (target identity
	// "<type>:<principal>", target_arn set only for ARN-shaped values); trust
	// evaluation belongs to reducers, not this scanner.
	RelationshipKMSGrantForGrantee = "kms_grant_for_grantee"
)

// Security-category constants consolidated from constants_acm.go (#6696).

const (
	// ServiceACM identifies the regional AWS Certificate Manager metadata-only
	// scan slice. The scanner covers public ACM-issued and imported public
	// certificates managed through ACM. ACM Private CA (acm-pca) is out of
	// scope for this service kind.
	ServiceACM = "acm"
)

const (
	// ResourceTypeACMCertificate identifies an AWS Certificate Manager
	// certificate metadata resource. The fact carries certificate identity
	// (ARN), subject identity (domain name and subject alternative names),
	// lifecycle metadata (status, type, issuer, validity, key/signature
	// algorithms), and ACM-reported in-use-by ARNs. The certificate body
	// PEM and private key material are never persisted.
	ResourceTypeACMCertificate = "aws_acm_certificate"
)

const (
	// RelationshipACMCertificateUsedByResource records ACM-reported in-use-by
	// evidence from a certificate to an ARN-addressable consuming resource
	// (ELB, CloudFront, API Gateway, AppSync, App Runner, or other ARN-shaped
	// AWS service target).
	RelationshipACMCertificateUsedByResource = "acm_certificate_used_by_resource"
)

// Security-category constants consolidated from constants_acmpca.go (#6696).

const (
	// ServiceACMPCA identifies the regional AWS Certificate Manager Private CA
	// (acm-pca) metadata-only scan slice. The scanner emits certificate
	// authority identity, type, status, key and signing algorithm, usage mode,
	// and revocation configuration metadata only. It never issues or exports
	// certificates and never reads the CA certificate chain body, the CSR body,
	// or private key material. ACM Private CA is a distinct service kind from
	// the public ACM scanner (ServiceACM).
	ServiceACMPCA = "acm-pca"
)

// ResourceTypeACMPCACertificateAuthority is defined in constants_appmesh.go
// because App Mesh introduced the forward-looking join key before this scanner
// existed. The ACM Private CA scanner publishes certificate authority
// resources with that resource_type and resource_id set to the CA ARN, which
// is the join key App Mesh virtual-node client TLS trust edges target.

const (
	// RelationshipACMPCACertificateAuthorityUsesKMSKey records that a
	// certificate authority is associated with a KMS key reported by AWS as an
	// ARN. The target keys on the KMS key ARN with target_type aws_kms_key. The
	// edge is emitted only when AWS reports an ARN-shaped key; the scanner never
	// synthesizes a key ARN.
	RelationshipACMPCACertificateAuthorityUsesKMSKey = "acmpca_certificate_authority_uses_kms_key"

	// RelationshipACMPCASubordinateCertificateAuthorityIssuedByParent records
	// that a SUBORDINATE certificate authority is issued by a parent (issuing)
	// certificate authority reported by AWS as an ARN. The target keys on the
	// parent CA ARN with target_type aws_acmpca_certificate_authority. The edge
	// is emitted only when AWS reports an ARN-shaped parent; the scanner never
	// synthesizes a parent ARN.
	RelationshipACMPCASubordinateCertificateAuthorityIssuedByParent = "acmpca_subordinate_certificate_authority_issued_by_parent"

	// RelationshipACMPCACertificateAuthorityPublishesCRLToBucket records that a
	// certificate authority publishes its certificate revocation list to an S3
	// bucket reported by AWS. The target keys on the bucket name (an S3
	// correlation anchor) with target_type aws_s3_bucket. The edge is emitted
	// only when CRL publishing is configured with a bucket name.
	RelationshipACMPCACertificateAuthorityPublishesCRLToBucket = "acmpca_certificate_authority_publishes_crl_to_bucket"
)

// Security-category constants consolidated from constants_signer.go (#6696).

const (
	// ServiceSigner identifies the regional AWS Signer (code-signing) metadata
	// scan slice. The scanner reads signing-profile and signing-platform
	// control-plane metadata through the Signer management APIs
	// (ListSigningProfiles, GetSigningProfile, ListSigningPlatforms); profile
	// tags come from the ListSigningProfiles response rather than a separate
	// tag read. It never reads signing jobs, signing material private keys,
	// signed-object payloads, or revocation records, and never starts,
	// cancels, or mutates a signing operation.
	ServiceSigner = "signer"
)

const (
	// ResourceTypeSignerSigningProfile identifies an AWS Signer signing-profile
	// metadata resource. The scanner emits identity, the bound signing platform,
	// the profile version, status, signature validity period, signing image
	// format, the names (never the values) of signing parameters, and the ACM
	// certificate ARN reference only. Signing jobs, private key material, and
	// signed-object payloads stay outside the contract.
	ResourceTypeSignerSigningProfile = "aws_signer_signing_profile"
	// ResourceTypeSignerSigningPlatform identifies an AWS Signer signing-platform
	// metadata resource. The scanner emits the platform id, display name,
	// category, signing target, partner, maximum signable size, and the
	// revocation-supported flag only. Algorithm option sets are recorded as
	// names so no signing-material detail is persisted.
	ResourceTypeSignerSigningPlatform = "aws_signer_signing_platform"
)

const (
	// RelationshipSignerProfileUsesACMCertificate records a signing profile's
	// reported ACM certificate dependency. AWS reports the certificate ARN in
	// the profile's SigningMaterial, which matches how the ACM scanner publishes
	// its certificate resource_id, so the edge joins the certificate node
	// exactly.
	RelationshipSignerProfileUsesACMCertificate = "signer_profile_uses_acm_certificate"
	// RelationshipSignerProfileUsesSigningPlatform records a signing profile's
	// binding to the signing platform it was created with. The target is keyed
	// by the bare platform id this scanner also publishes as a
	// signing-platform resource_id, so the internal edge joins the platform node
	// the scanner emits.
	RelationshipSignerProfileUsesSigningPlatform = "signer_profile_uses_signing_platform"
)

// Security-category constants consolidated from constants_ssoadmin.go (#6696).

const (
	// ServiceSSOAdmin identifies the AWS IAM Identity Center (sso-admin plus
	// identitystore) metadata scan slice. Identity Center is org-scoped and runs
	// against the management or a delegated-administrator account.
	ServiceSSOAdmin = "ssoadmin"
)

const (
	// ResourceTypeSSOAdminInstance identifies an IAM Identity Center instance.
	ResourceTypeSSOAdminInstance = "aws_identitycenter_instance"
	// ResourceTypeSSOAdminPermissionSet identifies an IAM Identity Center
	// permission set. Permission set inline policy bodies and customer-managed
	// policy bodies are never part of this resource.
	ResourceTypeSSOAdminPermissionSet = "aws_identitycenter_permission_set"
	// ResourceTypeSSOAdminAccountAssignment identifies an IAM Identity Center
	// account assignment binding a principal and permission set to an account.
	ResourceTypeSSOAdminAccountAssignment = "aws_identitycenter_account_assignment"
	// ResourceTypeSSOAdminApplication identifies an IAM Identity Center
	// application instance. Application access-scope attributes that can carry
	// sensitive group filters are never part of this resource.
	ResourceTypeSSOAdminApplication = "aws_identitycenter_application"
	// ResourceTypeSSOAdminTrustedTokenIssuer identifies an IAM Identity Center
	// trusted token issuer configuration.
	ResourceTypeSSOAdminTrustedTokenIssuer = "aws_identitycenter_trusted_token_issuer"
	// ResourceTypeSSOAdminPrincipal identifies an IAM Identity Center principal
	// (group or user) resolved from the connected identity store. The display
	// name is redacted before persistence.
	ResourceTypeSSOAdminPrincipal = "aws_identitycenter_principal"
)

const (
	// RelationshipSSOAdminPermissionSetInInstance records that a permission set
	// belongs to an Identity Center instance.
	RelationshipSSOAdminPermissionSetInInstance = "identitycenter_permission_set_in_instance"
	// RelationshipSSOAdminApplicationInInstance records that an application
	// instance belongs to an Identity Center instance.
	RelationshipSSOAdminApplicationInInstance = "identitycenter_application_in_instance"
	// RelationshipSSOAdminAssignmentUsesPermissionSet records that an account
	// assignment grants a permission set.
	RelationshipSSOAdminAssignmentUsesPermissionSet = "identitycenter_assignment_uses_permission_set"
	// RelationshipSSOAdminAssignmentTargetsAccount records that an account
	// assignment applies to a target AWS account.
	RelationshipSSOAdminAssignmentTargetsAccount = "identitycenter_assignment_targets_account"
	// RelationshipSSOAdminAssignmentGrantsPrincipal records that an account
	// assignment grants access to a group or user principal.
	RelationshipSSOAdminAssignmentGrantsPrincipal = "identitycenter_assignment_grants_principal"
	// RelationshipSSOAdminPermissionSetUsesManagedPolicy records an AWS managed
	// policy attached to a permission set by ARN reference.
	RelationshipSSOAdminPermissionSetUsesManagedPolicy = "identitycenter_permission_set_uses_managed_policy"
	// RelationshipSSOAdminPermissionSetUsesCustomerManagedPolicy records a
	// customer-managed policy reference attached to a permission set by name and
	// path only. The IAM policy body is never read or persisted here.
	RelationshipSSOAdminPermissionSetUsesCustomerManagedPolicy = "identitycenter_permission_set_uses_customer_managed_policy"
)
