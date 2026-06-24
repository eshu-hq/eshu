// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

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
