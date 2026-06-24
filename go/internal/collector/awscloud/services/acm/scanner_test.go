// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package acm

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsCertificateFactsMetadataOnlyAndInUseRelationships(t *testing.T) {
	certificateARN := "arn:aws:acm:us-east-1:123456789012:certificate/12345678-1234-1234-1234-123456789012"
	notBefore := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	notAfter := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	loadBalancerARN := "arn:aws:elasticloadbalancing:us-east-1:123456789012:loadbalancer/app/web/abc"
	cloudFrontARN := "arn:aws:cloudfront::123456789012:distribution/E123ABCDEF"
	apiGatewayARN := "arn:aws:apigateway:us-east-1::/domainnames/api.example.com"
	appSyncARN := "arn:aws:appsync:us-east-1:123456789012:apis/abcdef"
	appRunnerARN := "arn:aws:apprunner:us-east-1:123456789012:service/web/abc"

	client := fakeClient{certificates: []Certificate{{
		ARN:                     certificateARN,
		DomainName:              "example.com",
		SubjectAlternativeNames: []string{"example.com", "www.example.com"},
		Status:                  "ISSUED",
		Type:                    "AMAZON_ISSUED",
		Issuer:                  "Amazon",
		NotBefore:               notBefore,
		NotAfter:                notAfter,
		KeyAlgorithm:            "RSA_2048",
		SignatureAlgorithm:      "SHA256WITHRSA",
		InUseBy: []string{
			loadBalancerARN,
			cloudFrontARN,
			apiGatewayARN,
			appSyncARN,
			appRunnerARN,
		},
		Tags: map[string]string{"Environment": "prod"},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	certificate := resourceByType(t, envelopes, awscloud.ResourceTypeACMCertificate)
	attributes := attributesOf(t, certificate)
	if got, want := attributes["domain_name"], "example.com"; got != want {
		t.Fatalf("domain_name = %#v, want %q", got, want)
	}
	if got, want := attributes["status"], "ISSUED"; got != want {
		t.Fatalf("status = %#v, want %q", got, want)
	}
	if got, want := attributes["type"], "AMAZON_ISSUED"; got != want {
		t.Fatalf("type = %#v, want %q", got, want)
	}
	if got, want := attributes["issuer"], "Amazon"; got != want {
		t.Fatalf("issuer = %#v, want %q", got, want)
	}
	if got, want := attributes["key_algorithm"], "RSA_2048"; got != want {
		t.Fatalf("key_algorithm = %#v, want %q", got, want)
	}
	if got, want := attributes["signature_algorithm"], "SHA256WITHRSA"; got != want {
		t.Fatalf("signature_algorithm = %#v, want %q", got, want)
	}
	if got, want := attributes["not_before"], notBefore.UTC(); got != want {
		t.Fatalf("not_before = %#v, want %v", got, want)
	}
	if got, want := attributes["not_after"], notAfter.UTC(); got != want {
		t.Fatalf("not_after = %#v, want %v", got, want)
	}
	if got, want := attributes["subject_alternative_names"], []string{"example.com", "www.example.com"}; !equalStringSlices(got, want) {
		t.Fatalf("subject_alternative_names = %#v, want %#v", got, want)
	}
	if got, want := attributes["in_use_by"], []string{loadBalancerARN, cloudFrontARN, apiGatewayARN, appSyncARN, appRunnerARN}; !equalStringSlices(got, want) {
		t.Fatalf("in_use_by = %#v, want %#v", got, want)
	}
	if _, exists := attributes["certificate"]; exists {
		t.Fatalf("certificate body attribute persisted; ACM scanner must not store certificate PEM")
	}
	if _, exists := attributes["certificate_body"]; exists {
		t.Fatalf("certificate_body attribute persisted; ACM scanner must not store certificate PEM")
	}
	if _, exists := attributes["private_key"]; exists {
		t.Fatalf("private_key attribute persisted; ACM scanner must not store certificate private key material")
	}
	if got, want := certificate.Payload["arn"], certificateARN; got != want {
		t.Fatalf("resource ARN = %#v, want %q", got, want)
	}

	relationships := relationshipsByType(envelopes, awscloud.RelationshipACMCertificateUsedByResource)
	if got, want := len(relationships), 5; got != want {
		t.Fatalf("relationship count = %d, want %d", got, want)
	}
	wantTargets := map[string]string{
		loadBalancerARN: awscloud.ResourceTypeELBv2LoadBalancer,
		cloudFrontARN:   awscloud.ResourceTypeCloudFrontDistribution,
		apiGatewayARN:   awscloud.ResourceTypeAPIGatewayDomainName,
		appSyncARN:      "aws_appsync_api",
		appRunnerARN:    "aws_apprunner_service",
	}
	for _, relationship := range relationships {
		targetARN, _ := relationship.Payload["target_arn"].(string)
		targetType, _ := relationship.Payload["target_type"].(string)
		want, ok := wantTargets[targetARN]
		if !ok {
			t.Fatalf("unexpected target_arn %q in relationship %#v", targetARN, relationship.Payload)
		}
		if targetType != want {
			t.Fatalf("target_type for %s = %q, want %q", targetARN, targetType, want)
		}
	}
}

func TestScannerSkipsRelationshipsWhenInUseByIsEmpty(t *testing.T) {
	client := fakeClient{certificates: []Certificate{{
		ARN:        "arn:aws:acm:us-east-1:123456789012:certificate/abc",
		DomainName: "example.com",
		Status:     "PENDING_VALIDATION",
		Type:       "AMAZON_ISSUED",
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	for _, envelope := range envelopes {
		if envelope.FactKind == facts.AWSRelationshipFactKind {
			t.Fatalf("relationship emitted for certificate with no in-use-by ARNs: %#v", envelope.Payload)
		}
	}
}

func TestScannerSkipsRelationshipForUnshapedInUseByEntry(t *testing.T) {
	client := fakeClient{certificates: []Certificate{{
		ARN:        "arn:aws:acm:us-east-1:123456789012:certificate/abc",
		DomainName: "example.com",
		Status:     "ISSUED",
		InUseBy:    []string{"  ", "not-an-arn"},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	for _, envelope := range envelopes {
		if envelope.FactKind == facts.AWSRelationshipFactKind {
			t.Fatalf("relationship emitted for non-ARN in_use_by entry: %#v", envelope.Payload)
		}
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceECR

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

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceACM,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:acm:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 14, 14, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	certificates []Certificate
}

func (c fakeClient) ListCertificates(context.Context) ([]Certificate, error) {
	return c.certificates, nil
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

func equalStringSlices(got any, want []string) bool {
	values, ok := got.([]string)
	if !ok || len(values) != len(want) {
		return false
	}
	for i := range values {
		if values[i] != want[i] {
			return false
		}
	}
	return true
}
