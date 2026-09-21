// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package acmpca

import (
	"context"
	"time"
)

// Client is the ACM Private CA read surface consumed by Scanner. Runtime
// adapters MUST translate AWS SDK responses into these scanner-owned metadata
// records.
//
// The interface deliberately excludes every operation that returns sensitive
// bodies or mutates state: IssueCertificate, GetCertificate,
// GetCertificateAuthorityCsr (CSR body), GetCertificateAuthorityCertificate
// (certificate chain body), and the Create/Delete/Update/Restore/Import
// lifecycle APIs. Tests assert the absence of these methods on the SDK adapter
// so a future SDK change cannot smuggle them in.
type Client interface {
	// ListCertificateAuthorities returns the ACM Private CA metadata visible to
	// the configured credentials. Implementations MUST handle pagination and the
	// DescribeCertificateAuthority / ListTags per-CA refinement inside the
	// adapter so the scanner sees a flat, metadata-only view.
	ListCertificateAuthorities(context.Context) ([]CertificateAuthority, error)
}

// CertificateAuthority is the scanner-owned representation of one ACM Private
// CA certificate authority. It contains metadata only; the CA certificate
// chain body, CSR, and private key material are intentionally outside this
// contract.
type CertificateAuthority struct {
	// ARN is the certificate authority ARN reported by ACM Private CA. It is the
	// resource_id the scanner publishes, matching the join key App Mesh
	// virtual-node client TLS trust edges target.
	ARN string
	// OwnerAccount is the AWS account ID that owns the certificate authority as
	// reported by AWS. It may differ from the scanning account for shared CAs.
	OwnerAccount string
	// Type reports the CA tier reported by AWS: ROOT or SUBORDINATE. The scanner
	// records the value verbatim.
	Type string
	// Status reports the CA lifecycle status reported by AWS (CREATING,
	// PENDING_CERTIFICATE, ACTIVE, DELETED, DISABLED, EXPIRED, FAILED). The
	// scanner records the value verbatim.
	Status string
	// Serial is the CA serial number reported by AWS, when present.
	Serial string
	// FailureReason reports why CA creation failed, when AWS reports one.
	FailureReason string
	// UsageMode reports whether the CA issues GENERAL_PURPOSE or SHORT_LIVED
	// certificates as reported by AWS.
	UsageMode string
	// KeyStorageSecurityStandard reports the key management compliance standard
	// reported by AWS (for example FIPS_140_2_LEVEL_3_OR_HIGHER).
	KeyStorageSecurityStandard string
	// KeyAlgorithm reports the CA key-pair algorithm from the CA configuration
	// (for example RSA_2048, EC_prime256v1) as reported by AWS.
	KeyAlgorithm string
	// SigningAlgorithm reports the algorithm the CA uses to sign certificate
	// requests as reported by AWS. It is distinct from the algorithm used to
	// sign issued certificates.
	SigningAlgorithm string
	// SubjectCommonName reports the X.500 common name of the CA subject as
	// reported by AWS. Distinguished-name detail beyond the common name is not
	// carried; the subject is identity metadata, not a secret.
	SubjectCommonName string
	// CreatedAt is the AWS-reported CA creation time.
	CreatedAt time.Time
	// LastStateChangeAt is the AWS-reported time of the most recent CA state
	// change.
	LastStateChangeAt time.Time
	// NotBefore is the AWS-reported start of CA certificate validity.
	NotBefore time.Time
	// NotAfter is the AWS-reported end of CA certificate validity.
	NotAfter time.Time
	// CRLEnabled reports whether the CA maintains a certificate revocation list.
	CRLEnabled bool
	// CRLS3BucketName reports the S3 bucket name that holds the CA's CRL, when a
	// CRL is configured. It is reported metadata, not a secret, and drives a
	// CA-to-S3-bucket relationship.
	CRLS3BucketName string
	// OCSPEnabled reports whether the CA exposes OCSP revocation responses.
	OCSPEnabled bool
	// KMSKeyARN reports an ARN-shaped KMS key the CA record associates with the
	// CA, when AWS reports one. The metadata-only ListCertificateAuthorities /
	// DescribeCertificateAuthority surface does not currently report a KMS key,
	// so this stays empty for those responses; the field exists so the
	// CA-to-KMS-key edge keys on a real reported ARN and never on a synthesized
	// value.
	KMSKeyARN string
	// ParentCAARN reports the parent (issuing) certificate authority ARN for a
	// SUBORDINATE CA, when AWS reports one. The metadata-only surface does not
	// report the parent ARN (resolving it would require reading the certificate
	// chain body, which is out of scope), so this stays empty for those
	// responses; the field exists so the subordinate-to-parent edge keys on a
	// real reported ARN and never on a synthesized value.
	ParentCAARN string
	// Tags carries CA resource tags as raw evidence. Do not infer environment,
	// owner, workload, or deployable-unit truth from tags here.
	Tags map[string]string
}
