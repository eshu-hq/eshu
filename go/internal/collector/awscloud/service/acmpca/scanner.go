// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package acmpca

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// certificateAuthorityTypeSubordinate is the CA tier whose certificate is
// issued by a parent CA. Only SUBORDINATE CAs emit an issued-by-parent edge.
const certificateAuthorityTypeSubordinate = "SUBORDINATE"

// Scanner emits AWS Certificate Manager Private CA (acm-pca) metadata facts for
// one claimed account and region. It never issues or exports certificates and
// never persists the certificate chain body, CSR body, or private key material.
type Scanner struct {
	Client Client
}

// Scan observes ACM Private CA certificate authorities through the configured
// client and emits metadata-only resource facts plus ARN-driven relationship
// evidence. The certificate authority resource_id is the CA ARN so App Mesh
// virtual-node client TLS trust edges resolve against it.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("acmpca scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceACMPCA:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceACMPCA
	default:
		return nil, fmt.Errorf("acmpca scanner received service_kind %q", boundary.ServiceKind)
	}

	authorities, err := s.Client.ListCertificateAuthorities(ctx)
	if err != nil {
		return nil, fmt.Errorf("list ACM Private CA certificate authorities: %w", err)
	}
	var envelopes []facts.Envelope
	for _, authority := range authorities {
		// The CA ARN is load-bearing: it is the resource_id App Mesh client TLS
		// trust edges resolve against, and the scanner never synthesizes one. A
		// malformed upstream entry with a blank ARN is skipped rather than
		// failing the whole scan, so one bad record cannot drop every CA fact.
		if strings.TrimSpace(authority.ARN) == "" {
			continue
		}
		resource, err := awscloud.NewResourceEnvelope(certificateAuthorityObservation(boundary, authority))
		if err != nil {
			return nil, fmt.Errorf("build acmpca certificate authority fact: %w", err)
		}
		envelopes = append(envelopes, resource)
		for _, observation := range certificateAuthorityRelationships(boundary, authority) {
			envelope, err := awscloud.NewRelationshipEnvelope(observation)
			if err != nil {
				return nil, fmt.Errorf("build acmpca relationship fact: %w", err)
			}
			envelopes = append(envelopes, envelope)
		}
	}
	return envelopes, nil
}

func certificateAuthorityObservation(boundary awscloud.Boundary, authority CertificateAuthority) awscloud.ResourceObservation {
	caARN := strings.TrimSpace(authority.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          caARN,
		ResourceID:   caARN,
		ResourceType: awscloud.ResourceTypeACMPCACertificateAuthority,
		Name:         firstNonEmpty(authority.SubjectCommonName, caARN),
		State:        strings.TrimSpace(authority.Status),
		Tags:         cloneStringMap(authority.Tags),
		Attributes: map[string]any{
			"owner_account":                 strings.TrimSpace(authority.OwnerAccount),
			"type":                          strings.TrimSpace(authority.Type),
			"status":                        strings.TrimSpace(authority.Status),
			"serial":                        strings.TrimSpace(authority.Serial),
			"failure_reason":                strings.TrimSpace(authority.FailureReason),
			"usage_mode":                    strings.TrimSpace(authority.UsageMode),
			"key_storage_security_standard": strings.TrimSpace(authority.KeyStorageSecurityStandard),
			"key_algorithm":                 strings.TrimSpace(authority.KeyAlgorithm),
			"signing_algorithm":             strings.TrimSpace(authority.SigningAlgorithm),
			"subject_common_name":           strings.TrimSpace(authority.SubjectCommonName),
			"created_at":                    timeOrNil(authority.CreatedAt),
			"last_state_change_at":          timeOrNil(authority.LastStateChangeAt),
			"not_before":                    timeOrNil(authority.NotBefore),
			"not_after":                     timeOrNil(authority.NotAfter),
			"crl_enabled":                   authority.CRLEnabled,
			"crl_s3_bucket_name":            strings.TrimSpace(authority.CRLS3BucketName),
			"ocsp_enabled":                  authority.OCSPEnabled,
		},
		CorrelationAnchors: []string{caARN, strings.TrimSpace(authority.Serial)},
		SourceRecordID:     caARN,
	}
}

// certificateAuthorityRelationships emits the ARN-driven relationship evidence
// AWS reports for one certificate authority. Each edge is conditional: the
// scanner emits a relationship only when AWS reports a concrete join key and
// never synthesizes an ARN. The partition is never hardcoded because every
// target identity comes straight from a reported value.
func certificateAuthorityRelationships(boundary awscloud.Boundary, authority CertificateAuthority) []awscloud.RelationshipObservation {
	caARN := strings.TrimSpace(authority.ARN)
	if caARN == "" {
		return nil
	}
	var relationships []awscloud.RelationshipObservation
	if rel, ok := kmsKeyRelationship(boundary, caARN, authority); ok {
		relationships = append(relationships, rel)
	}
	if rel, ok := parentRelationship(boundary, caARN, authority); ok {
		relationships = append(relationships, rel)
	}
	if rel, ok := crlBucketRelationship(boundary, caARN, authority); ok {
		relationships = append(relationships, rel)
	}
	return relationships
}

// kmsKeyRelationship emits a CA-to-KMS-key edge only when AWS reports an
// ARN-shaped KMS key for the CA. The target keys on the KMS key ARN, which the
// KMS scanner carries as a key correlation anchor, with target_type
// aws_kms_key. target_arn is set because the value is ARN-shaped.
func kmsKeyRelationship(boundary awscloud.Boundary, caARN string, authority CertificateAuthority) (awscloud.RelationshipObservation, bool) {
	kmsKeyARN := strings.TrimSpace(authority.KMSKeyARN)
	if !looksLikeARN(kmsKeyARN) {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipACMPCACertificateAuthorityUsesKMSKey,
		SourceResourceID: caARN,
		SourceARN:        caARN,
		TargetResourceID: kmsKeyARN,
		TargetARN:        kmsKeyARN,
		TargetType:       awscloud.ResourceTypeKMSKey,
		SourceRecordID:   caARN + "->" + kmsKeyARN,
	}, true
}

// parentRelationship emits a subordinate-to-parent edge only for a SUBORDINATE
// CA that reports an ARN-shaped parent CA ARN. The target keys on the parent CA
// ARN, which is the parent CA's resource_id, with target_type
// aws_acmpca_certificate_authority. A ROOT CA is self-signed and never emits
// this edge.
func parentRelationship(boundary awscloud.Boundary, caARN string, authority CertificateAuthority) (awscloud.RelationshipObservation, bool) {
	if !strings.EqualFold(strings.TrimSpace(authority.Type), certificateAuthorityTypeSubordinate) {
		return awscloud.RelationshipObservation{}, false
	}
	parentARN := strings.TrimSpace(authority.ParentCAARN)
	if !looksLikeARN(parentARN) {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipACMPCASubordinateCertificateAuthorityIssuedByParent,
		SourceResourceID: caARN,
		SourceARN:        caARN,
		TargetResourceID: parentARN,
		TargetARN:        parentARN,
		TargetType:       awscloud.ResourceTypeACMPCACertificateAuthority,
		SourceRecordID:   caARN + "->" + parentARN,
	}, true
}

// crlBucketRelationship emits a CA-to-S3-bucket edge only when the CA publishes
// its CRL to a named bucket. The target keys on the bucket name, which the S3
// scanner carries as a bucket correlation anchor, with target_type
// aws_s3_bucket. The bucket name is not an ARN, so target_arn stays empty.
func crlBucketRelationship(boundary awscloud.Boundary, caARN string, authority CertificateAuthority) (awscloud.RelationshipObservation, bool) {
	bucket := strings.TrimSpace(authority.CRLS3BucketName)
	if bucket == "" {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipACMPCACertificateAuthorityPublishesCRLToBucket,
		SourceResourceID: caARN,
		SourceARN:        caARN,
		TargetResourceID: bucket,
		TargetType:       awscloud.ResourceTypeS3Bucket,
		SourceRecordID:   caARN + "->s3:" + bucket,
	}, true
}

// looksLikeARN keeps the relationship contract honest: the scanner emits an
// ARN-keyed relationship only when the reported value is actually an ARN, so a
// blank or alias-shaped value never produces a dangling target_arn.
func looksLikeARN(value string) bool {
	return strings.HasPrefix(value, "arn:")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func cloneStringMap(input map[string]string) map[string]string {
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

func timeOrNil(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}
