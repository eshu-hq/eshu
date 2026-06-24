// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

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
