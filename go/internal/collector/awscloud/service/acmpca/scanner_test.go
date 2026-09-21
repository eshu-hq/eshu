// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package acmpca

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsCertificateAuthorityMetadataKeyedByARN(t *testing.T) {
	caARN := "arn:aws:acm-pca:us-east-1:123456789012:certificate-authority/abc"
	createdAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	notAfter := time.Date(2031, 1, 1, 0, 0, 0, 0, time.UTC)
	client := fakeClient{authorities: []CertificateAuthority{{
		ARN:                        caARN,
		OwnerAccount:               "123456789012",
		Type:                       "ROOT",
		Status:                     "ACTIVE",
		Serial:                     "01",
		UsageMode:                  "GENERAL_PURPOSE",
		KeyStorageSecurityStandard: "FIPS_140_2_LEVEL_3_OR_HIGHER",
		KeyAlgorithm:               "RSA_2048",
		SigningAlgorithm:           "SHA256WITHRSA",
		SubjectCommonName:          "Eshu Root CA",
		CreatedAt:                  createdAt,
		NotAfter:                   notAfter,
		CRLEnabled:                 false,
		OCSPEnabled:                true,
		Tags:                       map[string]string{"Environment": "prod"},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	ca := resourceByType(t, envelopes, awscloud.ResourceTypeACMPCACertificateAuthority)
	// The resource_id contract: the CA ARN. App Mesh virtual-node client TLS
	// trust edges target aws_acmpca_certificate_authority keyed by this ARN.
	if got, want := ca.Payload["resource_id"], caARN; got != want {
		t.Fatalf("resource_id = %#v, want %q (must match App Mesh CA trust edge target)", got, want)
	}
	if got, want := ca.Payload["arn"], caARN; got != want {
		t.Fatalf("arn = %#v, want %q", got, want)
	}
	if got, want := ca.Payload["state"], "ACTIVE"; got != want {
		t.Fatalf("state = %#v, want %q", got, want)
	}
	attributes := attributesOf(t, ca)
	if got, want := attributes["type"], "ROOT"; got != want {
		t.Fatalf("type = %#v, want %q", got, want)
	}
	if got, want := attributes["status"], "ACTIVE"; got != want {
		t.Fatalf("status = %#v, want %q", got, want)
	}
	if got, want := attributes["usage_mode"], "GENERAL_PURPOSE"; got != want {
		t.Fatalf("usage_mode = %#v, want %q", got, want)
	}
	if got, want := attributes["key_algorithm"], "RSA_2048"; got != want {
		t.Fatalf("key_algorithm = %#v, want %q", got, want)
	}
	if got, want := attributes["signing_algorithm"], "SHA256WITHRSA"; got != want {
		t.Fatalf("signing_algorithm = %#v, want %q", got, want)
	}
	if got, want := attributes["key_storage_security_standard"], "FIPS_140_2_LEVEL_3_OR_HIGHER"; got != want {
		t.Fatalf("key_storage_security_standard = %#v, want %q", got, want)
	}
	if got, want := attributes["subject_common_name"], "Eshu Root CA"; got != want {
		t.Fatalf("subject_common_name = %#v, want %q", got, want)
	}
	if got, want := attributes["owner_account"], "123456789012"; got != want {
		t.Fatalf("owner_account = %#v, want %q", got, want)
	}
	if got, want := attributes["not_after"], notAfter.UTC(); got != want {
		t.Fatalf("not_after = %#v, want %v", got, want)
	}
	if got, want := attributes["ocsp_enabled"], true; got != want {
		t.Fatalf("ocsp_enabled = %#v, want %v", got, want)
	}
	// Sensitive bodies must never be persisted.
	for _, forbidden := range []string{"certificate", "certificate_chain", "csr", "private_key"} {
		if _, exists := attributes[forbidden]; exists {
			t.Fatalf("attribute %q persisted; ACM Private CA scanner must not store sensitive bodies", forbidden)
		}
	}
	// No reported KMS key or parent ARN means no relationships at all.
	for _, envelope := range envelopes {
		if envelope.FactKind == facts.AWSRelationshipFactKind {
			t.Fatalf("relationship emitted with no reported KMS/parent/CRL ARN: %#v", envelope.Payload)
		}
	}
}

func TestScannerEmitsKMSKeyRelationshipWhenARNReported(t *testing.T) {
	caARN := "arn:aws:acm-pca:us-east-1:123456789012:certificate-authority/abc"
	kmsKeyARN := "arn:aws:kms:us-east-1:123456789012:key/12345678-1234-1234-1234-123456789012"
	client := fakeClient{authorities: []CertificateAuthority{{
		ARN:       caARN,
		Type:      "ROOT",
		Status:    "ACTIVE",
		KMSKeyARN: kmsKeyARN,
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	relationships := relationshipsByType(envelopes, awscloud.RelationshipACMPCACertificateAuthorityUsesKMSKey)
	if got, want := len(relationships), 1; got != want {
		t.Fatalf("KMS relationship count = %d, want %d", got, want)
	}
	rel := relationships[0]
	if got, want := rel.Payload["source_resource_id"], caARN; got != want {
		t.Fatalf("source_resource_id = %#v, want %q", got, want)
	}
	if got, want := rel.Payload["target_resource_id"], kmsKeyARN; got != want {
		t.Fatalf("target_resource_id = %#v, want %q (must match KMS key ARN correlation anchor)", got, want)
	}
	if got, want := rel.Payload["target_arn"], kmsKeyARN; got != want {
		t.Fatalf("target_arn = %#v, want %q", got, want)
	}
	if got, want := rel.Payload["target_type"], awscloud.ResourceTypeKMSKey; got != want {
		t.Fatalf("target_type = %#v, want %q", got, want)
	}
}

func TestScannerSkipsKMSKeyRelationshipForNonARNValue(t *testing.T) {
	client := fakeClient{authorities: []CertificateAuthority{{
		ARN:       "arn:aws:acm-pca:us-east-1:123456789012:certificate-authority/abc",
		Type:      "ROOT",
		Status:    "ACTIVE",
		KMSKeyARN: "alias/not-an-arn",
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if rels := relationshipsByType(envelopes, awscloud.RelationshipACMPCACertificateAuthorityUsesKMSKey); len(rels) != 0 {
		t.Fatalf("KMS relationship emitted for non-ARN key value: %#v", rels)
	}
}

func TestScannerEmitsSubordinateToParentRelationshipWhenParentReported(t *testing.T) {
	caARN := "arn:aws:acm-pca:us-east-1:123456789012:certificate-authority/sub"
	parentARN := "arn:aws:acm-pca:us-east-1:123456789012:certificate-authority/root"
	client := fakeClient{authorities: []CertificateAuthority{{
		ARN:         caARN,
		Type:        "SUBORDINATE",
		Status:      "ACTIVE",
		ParentCAARN: parentARN,
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	relationships := relationshipsByType(envelopes, awscloud.RelationshipACMPCASubordinateCertificateAuthorityIssuedByParent)
	if got, want := len(relationships), 1; got != want {
		t.Fatalf("parent relationship count = %d, want %d", got, want)
	}
	rel := relationships[0]
	if got, want := rel.Payload["source_resource_id"], caARN; got != want {
		t.Fatalf("source_resource_id = %#v, want %q", got, want)
	}
	if got, want := rel.Payload["target_resource_id"], parentARN; got != want {
		t.Fatalf("target_resource_id = %#v, want %q (must match parent CA resource_id = CA ARN)", got, want)
	}
	if got, want := rel.Payload["target_type"], awscloud.ResourceTypeACMPCACertificateAuthority; got != want {
		t.Fatalf("target_type = %#v, want %q", got, want)
	}
}

func TestScannerSkipsParentRelationshipForRootCA(t *testing.T) {
	parentARN := "arn:aws:acm-pca:us-east-1:123456789012:certificate-authority/root"
	client := fakeClient{authorities: []CertificateAuthority{{
		ARN:  "arn:aws:acm-pca:us-east-1:123456789012:certificate-authority/abc",
		Type: "ROOT",
		// A ROOT CA is self-signed; a reported parent ARN must not produce an
		// issued-by edge even if present.
		ParentCAARN: parentARN,
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if rels := relationshipsByType(envelopes, awscloud.RelationshipACMPCASubordinateCertificateAuthorityIssuedByParent); len(rels) != 0 {
		t.Fatalf("parent relationship emitted for ROOT CA: %#v", rels)
	}
}

func TestScannerEmitsCRLBucketRelationshipWhenConfigured(t *testing.T) {
	caARN := "arn:aws:acm-pca:us-east-1:123456789012:certificate-authority/abc"
	client := fakeClient{authorities: []CertificateAuthority{{
		ARN:             caARN,
		Type:            "ROOT",
		Status:          "ACTIVE",
		CRLEnabled:      true,
		CRLS3BucketName: "eshu-crl-bucket",
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	relationships := relationshipsByType(envelopes, awscloud.RelationshipACMPCACertificateAuthorityPublishesCRLToBucket)
	if got, want := len(relationships), 1; got != want {
		t.Fatalf("CRL relationship count = %d, want %d", got, want)
	}
	rel := relationships[0]
	if got, want := rel.Payload["target_resource_id"], "eshu-crl-bucket"; got != want {
		t.Fatalf("target_resource_id = %#v, want %q (must match S3 bucket name correlation anchor)", got, want)
	}
	if got, want := rel.Payload["target_type"], awscloud.ResourceTypeS3Bucket; got != want {
		t.Fatalf("target_type = %#v, want %q", got, want)
	}
	// The CRL bucket name is not an ARN, so target_arn stays empty.
	if got, want := rel.Payload["target_arn"], ""; got != want {
		t.Fatalf("target_arn = %#v, want empty (bucket name is not an ARN)", got)
	}
}

func TestScannerSkipsAuthorityWithBlankARN(t *testing.T) {
	caARN := "arn:aws:acm-pca:us-east-1:123456789012:certificate-authority/abc"
	client := fakeClient{authorities: []CertificateAuthority{
		// A malformed upstream entry with no ARN must be skipped, not fail the
		// whole scan: the CA ARN is load-bearing and cannot be synthesized.
		{ARN: "   ", Type: "ROOT", Status: "ACTIVE"},
		{ARN: caARN, Type: "ROOT", Status: "ACTIVE"},
	}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil (blank-ARN authority must be skipped, not fail the scan)", err)
	}
	resources := 0
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		resources++
		if got := envelope.Payload["resource_id"]; got != caARN {
			t.Fatalf("resource_id = %#v, want %q (only the valid CA must materialize)", got, caARN)
		}
	}
	if resources != 1 {
		t.Fatalf("resource fact count = %d, want 1 (blank-ARN authority must not materialize)", resources)
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceACM

	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func TestScannerRequiresClient(t *testing.T) {
	_, err := Scanner{}.Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want client-required rejection")
	}
}

func TestScannerDefaultsServiceKindWhenEmpty(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = ""
	caARN := "arn:aws:acm-pca:us-east-1:123456789012:certificate-authority/abc"
	client := fakeClient{authorities: []CertificateAuthority{{ARN: caARN, Type: "ROOT", Status: "ACTIVE"}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	ca := resourceByType(t, envelopes, awscloud.ResourceTypeACMPCACertificateAuthority)
	if got, want := ca.Payload["service_kind"], awscloud.ServiceACMPCA; got != want {
		t.Fatalf("service_kind = %#v, want %q", got, want)
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceACMPCA,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:acm-pca:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 14, 14, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	authorities []CertificateAuthority
}

func (c fakeClient) ListCertificateAuthorities(context.Context) ([]CertificateAuthority, error) {
	return c.authorities, nil
}

func resourceByType(t *testing.T, envelopes []facts.Envelope, resourceType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if got, _ := envelope.Payload["resource_type"].(string); got == resourceType {
			return envelope
		}
	}
	t.Fatalf("missing resource_type %q in %#v", resourceType, envelopes)
	return facts.Envelope{}
}

func relationshipsByType(envelopes []facts.Envelope, relationshipType string) []facts.Envelope {
	var result []facts.Envelope
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			result = append(result, envelope)
		}
	}
	return result
}

func attributesOf(t *testing.T, envelope facts.Envelope) map[string]any {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	return attributes
}
