// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package acm

import (
	"context"
	"time"
)

// Client is the ACM read surface consumed by Scanner. Runtime adapters MUST
// translate AWS SDK responses into these scanner-owned metadata records.
//
// The interface deliberately excludes GetCertificate and ExportCertificate.
// GetCertificate returns the PEM body, and ExportCertificate returns private
// key material; both are out of scope for metadata-only inventory. Tests assert
// the absence of these methods on the SDK adapter so a future SDK change
// cannot smuggle them in.
type Client interface {
	// ListCertificates returns the ACM metadata visible to the configured
	// credentials. Implementations MUST handle pagination and the
	// DescribeCertificate / ListTagsForCertificate per-certificate refinement
	// inside the adapter so the scanner sees a flat, metadata-only view.
	ListCertificates(context.Context) ([]Certificate, error)
}

// Certificate is the scanner-owned representation of one ACM certificate.
// It contains metadata only; certificate body PEM and any private key material
// are intentionally outside this contract.
type Certificate struct {
	// ARN is the certificate ARN reported by ACM.
	ARN string
	// DomainName is the primary common-name domain reported by ACM.
	DomainName string
	// SubjectAlternativeNames carries the SAN list reported by ACM.
	SubjectAlternativeNames []string
	// Status reports the ACM certificate lifecycle status. ACM returns these
	// as uppercase enum values: ISSUED, PENDING_VALIDATION, EXPIRED,
	// VALIDATION_TIMED_OUT, REVOKED, FAILED, INACTIVE. The scanner records
	// the value verbatim from AWS.
	Status string
	// Type reports the ACM-managed type (AMAZON_ISSUED, IMPORTED,
	// PRIVATE). PRIVATE here means the certificate was issued by ACM PCA but
	// is still surfaced via the public ACM API; the scanner records identity
	// metadata only and never calls ACM PCA APIs.
	Type string
	// Issuer reports the issuing certificate authority name reported by ACM.
	Issuer string
	// NotBefore is the ACM-reported start of certificate validity.
	NotBefore time.Time
	// NotAfter is the ACM-reported end of certificate validity.
	NotAfter time.Time
	// KeyAlgorithm reports the certificate key algorithm (RSA_2048, EC_prime256v1,
	// etc.) as reported by ACM.
	KeyAlgorithm string
	// SignatureAlgorithm reports the signing algorithm name as reported by ACM.
	SignatureAlgorithm string
	// InUseBy carries the ARN list ACM reports as currently consuming this
	// certificate. Each entry drives one certificate-to-using-resource
	// relationship fact.
	InUseBy []string
	// Tags carries ACM resource tags as raw evidence. Do not infer
	// environment, owner, workload, or deployable-unit truth from tags here.
	Tags map[string]string
}
