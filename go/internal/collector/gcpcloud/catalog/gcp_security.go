// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

// Security-family (lane K) constants consolidated from 15 security (+ API Gateway) extractor files (#6696).
// API Gateway constants consolidated from extractor_api_gateway.go (#6696).
const (
	// AssetTypeAPIGatewayGateway is the Cloud Asset Inventory asset type for a GCP
	// API Gateway Gateway.
	AssetTypeAPIGatewayGateway = "apigateway.googleapis.com/Gateway"
	// AssetTypeAPIGatewayAPIConfig is the Cloud Asset Inventory asset type for a
	// GCP API Gateway ApiConfig. ApiConfig is itself a separately CAI-inventoried
	// asset type (verified against the live Cloud Asset Inventory supported-asset-
	// types reference), so a Gateway's apiConfig reference resolves to a real edge
	// endpoint rather than staying an attribute-only value.
	AssetTypeAPIGatewayAPIConfig = "apigateway.googleapis.com/ApiConfig"
	// RelationshipTypeAPIGatewayUsesAPIConfig is the bounded provider relationship
	// type for a Gateway's required apiConfig reference, carried on a
	// gcp_cloud_relationship fact. The reducer materializes the edge only when both
	// endpoints resolve exactly.
	RelationshipTypeAPIGatewayUsesAPIConfig = "api_gateway_uses_api_config"
	// ApiGatewayAPIConfigFullResourceNamePrefix is the exact CAI full-resource-name
	// service prefix for an apigateway.googleapis.com/ApiConfig, verified against
	// the live Cloud Asset Inventory resource-name-format reference:
	// "//apigateway.googleapis.com/projects/{project}/locations/{location}/apis/{api}/configs/{apiConfig}".
	// An already-absolute apiConfig value must carry this exact prefix to be
	// trusted as-is; any other absolute value names a different, untrusted
	// service and must not mint an edge or anchor.
	ApiGatewayAPIConfigFullResourceNamePrefix = "//apigateway.googleapis.com/"
	// ApiGatewayAPIConfigRelativeMarker is the fixed "/apis/" path segment in the
	// documented relative ApiConfig resource-name shape
	// ("projects/{project}/locations/global/apis/{api}/configs/{apiConfig}", per
	// the API Gateway v1 projects.locations.apis.configs REST reference). A
	// relative apiConfig value must contain this marker with a non-empty prefix
	// and suffix to be recognized as that documented shape.
	ApiGatewayAPIConfigRelativeMarker = "/apis/"
)

const (
	// ApiGatewayLocationsMarker is the fixed "/locations/" path segment in a relative ApiConfig resource name. It is the API Gateway extractor's function-local marker, consolidated here so the family owns every constant it reads.
	ApiGatewayLocationsMarker = "/locations/"
)

// API Key constants consolidated from extractor_api_key.go (#6696).
const (
	// ApiKeyAssetType is the Cloud Asset Inventory asset type for a GCP API Key.
	ApiKeyAssetType = "apikeys.googleapis.com/Key"
)

// Certificate Manager Certificate constants consolidated from extractor_certificate_manager_certificate.go (#6696).
// AssetTypeCertificateManagerDNSAuthorization and
// AssetTypeCertificateManagerCertificateIssuanceConfig are the CAI asset types
// for the two Certificate Manager resources this Certificate extractor
// resolves outbound edges toward. Neither resource has its own typed-depth
// extractor yet, so these constants are declared in this leaf — the first extractor
// to reference them — for a future DNSAuthorization or
// CertificateIssuanceConfig extractor to reuse, mirroring how
// the forwarding-rule family in this leaf declares its proxy-kind asset type constants
// for reuse by their own eventual typed-depth extractors.
// AssetTypeCertificateManagerCertificate lives in this leaf (consolidated from extractor_target_https_proxy.go); certificateManagerFullName is the helper in extractor_target_https_proxy.go (that extractor resolves a
// TargetHttpsProxy's serving-certificate reference into this same asset type)
// and reused here for the Certificate resource's own typed depth.
const (
	AssetTypeCertificateManagerDNSAuthorization          = "certificatemanager.googleapis.com/DnsAuthorization"
	AssetTypeCertificateManagerCertificateIssuanceConfig = "certificatemanager.googleapis.com/CertificateIssuanceConfig"
)

const (
	// AssetTypeCertificateManagerCertificateMapEntry is the CAI asset type for a
	// Certificate Manager CertificateMapEntry, one of the two resource kinds a
	// Certificate's own `usedBy[]` field can name (see certManagerCertificateData
	// and resolveCertManagerCertificateUsedBy). Like
	// AssetTypeCertificateManagerDNSAuthorization above, no typed-depth extractor
	// for this asset type exists yet, so this constant is declared in this leaf for a
	// future CertificateMapEntry extractor to reuse.
	AssetTypeCertificateManagerCertificateMapEntry = "certificatemanager.googleapis.com/CertificateMapEntry"
)

// Certificate Manager Certificate `type` classification values. Certificate
// Manager's Certificate resource reports exactly one of `managed`,
// `selfManaged`, or `managedIdentity` populated (never a `type` field the way
// Compute's classic SslCertificate does), so the extractor derives this
// classification from which block is present rather than reading a field.
const (
	CertManagerCertificateTypeManaged         = "MANAGED"
	CertManagerCertificateTypeSelfManaged     = "SELF_MANAGED"
	CertManagerCertificateTypeManagedIdentity = "MANAGED_IDENTITY"
)

// Bounded provider relationship types for Certificate Manager Certificate
// edges. Each is a stable string carried on a gcp_cloud_relationship fact;
// the reducer materializes an edge only when both endpoints resolve exactly.
const (
	RelationshipTypeCertManagerCertificateUsesDNSAuthorization      = "certificate_manager_certificate_uses_dns_authorization"
	RelationshipTypeCertManagerCertificateUsesIssuanceConfig        = "certificate_manager_certificate_uses_issuance_config"
	RelationshipTypeCertManagerCertificateUsedByCertificateMapEntry = "certificate_manager_certificate_used_by_certificate_map_entry"
	RelationshipTypeCertManagerCertificateUsedByTargetHTTPSProxy    = "certificate_manager_certificate_used_by_target_https_proxy"
)

// IAM Role constants consolidated from extractor_iam_role.go (#6696).
const (
	// IamRoleAssetType is the Cloud Asset Inventory asset type for a GCP custom IAM
	// Role. It is declared in this leaf so the extractor registration stays self-contained
	// in this file.
	IamRoleAssetType = "iam.googleapis.com/Role"
)

// Identity Platform Config constants consolidated from extractor_identity_platform_config.go (#6696).
const (
	// IdentityConfigAssetType is the Cloud Asset Inventory asset type for a GCP
	// Identity Platform (Identity Toolkit) project Config.
	IdentityConfigAssetType = "identitytoolkit.googleapis.com/Config"
)

// KMS Crypto Key constants consolidated from extractor_kms_crypto_key.go (#6696).
const (
	// AssetTypeKMSKeyRing is the Cloud Asset Inventory asset type for a Cloud KMS
	// KeyRing, the CryptoKey's typed parent. assetTypeKMSCryptoKey and cloudKMSResourceNamePrefix are shared cross-group constants (see extractor/shared.go, aliased in gcpcloud/compat_extractor.go).
	AssetTypeKMSKeyRing = "cloudkms.googleapis.com/KeyRing"
	// RelationshipTypeCryptoKeyInKeyRing is the bounded provider relationship type
	// for the edge from a CryptoKey to its containing KeyRing. Cloud KMS does not
	// report the KeyRing as a separate field on the CryptoKey's resource.data; it
	// is derived from the CryptoKey's own resource-name path.
	RelationshipTypeCryptoKeyInKeyRing = "kms_crypto_key_in_key_ring"
	// KmsCryptoKeyPurposeEncryptDecrypt is the Cloud KMS CryptoKey purpose value
	// for symmetric/asymmetric encrypt-decrypt keys, the only purpose that
	// supports an automatic rotation schedule. A signing or MAC key purpose (for
	// example ASYMMETRIC_SIGN or MAC) does not rotate, so rotationPeriod and
	// nextRotationTime are gated on this purpose to avoid fabricating a rotation
	// schedule for a purpose that does not have one.
	KmsCryptoKeyPurposeEncryptDecrypt = "ENCRYPT_DECRYPT"
)

const (
	// KmsKeyRingLocationsSegment is the fixed "/locations/" path segment in a relative KeyRing resource name. It is the KeyRing extractor's function-local segment, consolidated here so the family owns every constant it reads.
	KmsKeyRingLocationsSegment = "/locations/"
	// KmsKeyRingKeyRingsSegment is the fixed "/keyRings/" path segment in a relative KeyRing resource name. It is the KeyRing extractor's function-local segment, consolidated here so the family owns every constant it reads.
	KmsKeyRingKeyRingsSegment = "/keyRings/"
)

// Organization Policy constants consolidated from extractor_org_policy.go (#6696).
const (
	// OrgPolicyAssetType is the Cloud Asset Inventory asset type for a GCP
	// Organization Policy (constraint binding). It is declared in this leaf so the
	// extractor registration stays self-contained in this file.
	OrgPolicyAssetType = "orgpolicy.googleapis.com/Policy"
)

// AssetTypeCloudResourceManagerOrganization and
// AssetTypeCloudResourceManagerFolder key the resource-hierarchy nodes an
// Organization Policy can target. This leaf reuses AssetTypeCloudResourceManagerProject,
// declared in this package's platform family (gcp_platform.go, consolidated
// from extractor_firebase_project.go by the IJ slice), never redeclared.
const (
	AssetTypeCloudResourceManagerOrganization = "cloudresourcemanager.googleapis.com/Organization"
	AssetTypeCloudResourceManagerFolder       = "cloudresourcemanager.googleapis.com/Folder"
)

const (
	// RelationshipTypeOrgPolicyAppliesToResource is the bounded provider
	// relationship type for the edge from an Organization Policy to the
	// organization, folder, or project resource it is bound to.
	RelationshipTypeOrgPolicyAppliesToResource = "org_policy_applies_to_resource"
	// OrgPolicyFullResourcePoliciesMarker separates an Organization Policy's
	// resource-hierarchy parent segment from its trailing `/policies/<constraint>`
	// suffix in a CAI full resource name.
	OrgPolicyFullResourcePoliciesMarker = "/policies/"
	// OrgPolicyFullResourceNamePrefix is the exact CAI service prefix every
	// Organization Policy full resource name must carry. The target-derivation
	// fails closed unless the name starts with this prefix, so a relative or
	// wrong-service asset name never mints a fabricated resource-hierarchy edge.
	OrgPolicyFullResourceNamePrefix = "//orgpolicy.googleapis.com/"
)

// reCAPTCHA Enterprise Key constants consolidated from extractor_recaptcha_key.go (#6696).
const (
	// RecaptchaKeyAssetType is the Cloud Asset Inventory asset type for a GCP
	// reCAPTCHA Enterprise Key.
	RecaptchaKeyAssetType = "recaptchaenterprise.googleapis.com/Key"
)

// Secret Manager Secret constants consolidated from extractor_secret_manager_secret.go (#6696).
// Bounded provider relationship types for Secret Manager Secret edges. Each is a
// stable, bounded string carried on a gcp_cloud_relationship fact; the reducer
// materializes an edge only when both endpoints resolve exactly.
const (
	RelationshipTypeSecretEncryptedByKMSKey = "secret_encrypted_by_kms_key"
	RelationshipTypeSecretNotifiesTopic     = "secret_notifies_topic"
)

// Secret Manager Secret Version constants consolidated from extractor_secret_version.go (#6696).
// Asset type for a Secret Manager Secret Version and for the KMS CryptoKeyVersion
// a CMEK-encrypted version references. The parent Secret endpoint reuses
// secretManagerSecretAssetType; the KMS resource-name prefix reuses
// cloudKMSResourceNamePrefix, both declared elsewhere in this package.
const (
	SecretVersionAssetType       = "secretmanager.googleapis.com/SecretVersion"
	AssetTypeKMSCryptoKeyVersion = "cloudkms.googleapis.com/CryptoKeyVersion"
)

// Bounded provider relationship types for Secret Version edges.
const (
	RelationshipTypeSecretVersionOfSecret                 = "secret_version_of_secret"
	RelationshipTypeSecretVersionEncryptedByKMSKeyVersion = "secret_version_encrypted_by_kms_key_version"
)

const (
	// SecretVersionVersionsMarker separates a Secret full resource name from its
	// per-version suffix (`.../secrets/<secret>/versions/<id>`).
	SecretVersionVersionsMarker = "/versions/"
)

// IAM Service Account Key constants consolidated from extractor_service_account_key.go (#6696).
const (
	// ServiceAccountKeyAssetType is the Cloud Asset Inventory asset type for a GCP
	// IAM ServiceAccount key. It is declared in this leaf so the extractor registration
	// stays self-contained in this file. The parent ServiceAccount endpoint reuses
	// serviceAccountAssetType already declared for the ServiceAccount extractor.
	ServiceAccountKeyAssetType = "iam.googleapis.com/ServiceAccountKey"
	// RelationshipTypeServiceAccountKeyOf is the bounded provider relationship type
	// for the edge from a key to its parent ServiceAccount. The reducer materializes
	// the edge only when both endpoints resolve exactly.
	RelationshipTypeServiceAccountKeyOf = "service_account_key_of"
	// ServiceAccountKeyKeysMarker separates a ServiceAccount full resource name from
	// its per-key suffix in a ServiceAccountKey full resource name
	// (`.../serviceAccounts/<email>/keys/<id>`).
	ServiceAccountKeyKeysMarker = "/keys/"
)

// Workload Identity Pool constants consolidated from extractor_workload_identity_pool.go (#6696).
const (
	// WorkloadIdentityPoolAssetType is the Cloud Asset Inventory asset type for a GCP
	// IAM Workload Identity Pool. It is declared in this leaf so both the pool extractor and
	// the provider extractor (which builds the provider -> pool edge) share one
	// constant.
	WorkloadIdentityPoolAssetType = "iam.googleapis.com/WorkloadIdentityPool"
)

// Workload Identity Pool Provider constants consolidated from extractor_workload_identity_pool_provider.go (#6696).
const (
	// WorkloadIdentityPoolProviderAssetType is the Cloud Asset Inventory asset type
	// for a GCP IAM Workload Identity Pool Provider — the child of a
	// WorkloadIdentityPool that configures the external AWS/OIDC/SAML trust.
	WorkloadIdentityPoolProviderAssetType = "iam.googleapis.com/WorkloadIdentityPoolProvider"
	// RelationshipTypeWIFProviderOfPool is the bounded provider relationship type for
	// the edge from a provider to its parent Workload Identity Pool. The reducer
	// materializes the edge only when both endpoints resolve exactly.
	RelationshipTypeWIFProviderOfPool = "workload_identity_provider_of_pool"
	// WifProvidersMarker separates a WorkloadIdentityPool full resource name from its
	// per-provider suffix (`.../workloadIdentityPools/<pool>/providers/<id>`).
	WifProvidersMarker = "/providers/"
	// AwsDefaultAttributeMappingKeyCount is the number of attribute-mapping keys IAM
	// applies by default to an AWS Workload Identity Pool Provider that declares no
	// explicit attributeMapping (`google.subject` and `attribute.aws_role`). Reporting
	// it keeps the effective trust-mapping posture accurate rather than emitting an
	// "unknown/empty" mapping for the common default-AWS case.
	AwsDefaultAttributeMappingKeyCount = 2
)
