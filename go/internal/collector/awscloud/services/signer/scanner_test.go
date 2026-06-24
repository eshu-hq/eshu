// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package signer

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	testProfileARN        = "arn:aws:signer:us-east-1:123456789012:/signing-profiles/lambda_release"
	testProfileVersionARN = "arn:aws:signer:us-east-1:123456789012:/signing-profiles/lambda_release/AbCdEf123456"
	testCertificateARN    = "arn:aws:acm:us-east-1:123456789012:certificate/12345678-1234-1234-1234-123456789012"
	testPlatformID        = "AWSLambda-SHA384-ECDSA"
)

func TestScannerEmitsSignerMetadataAndRelationships(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		Platforms: []SigningPlatform{{
			PlatformID:          testPlatformID,
			DisplayName:         "AWS Lambda",
			Category:            "AWSIoT",
			Target:              "Lambda",
			MaxSizeInMB:         250,
			RevocationSupported: true,
		}},
		Profiles: []SigningProfile{{
			ARN:                    testProfileARN,
			ProfileVersionARN:      testProfileVersionARN,
			Name:                   "lambda_release",
			ProfileVersion:         "AbCdEf123456",
			PlatformID:             testPlatformID,
			PlatformDisplayName:    "AWS Lambda",
			Status:                 "Active",
			SignatureValidityType:  "DAYS",
			SignatureValidityValue: 135,
			SigningImageFormat:     "JSONEmbedded",
			SigningParameterNames:  []string{"release-channel"},
			CertificateARN:         testCertificateARN,
			Tags:                   map[string]string{"Environment": "prod"},
		}},
	}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	// Signing-platform resource node, keyed by the bare platform id.
	platform := resourceByType(t, envelopes, awscloud.ResourceTypeSignerSigningPlatform)
	if got, want := platform.Payload["resource_id"], testPlatformID; got != want {
		t.Fatalf("platform resource_id = %#v, want %q", got, want)
	}
	platformAttrs := attributesOf(t, platform)
	assertAttribute(t, platformAttrs, "category", "AWSIoT")
	assertAttribute(t, platformAttrs, "target", "Lambda")
	assertAttribute(t, platformAttrs, "max_size_in_mb", int32(250))
	assertAttribute(t, platformAttrs, "revocation_supported", true)

	// Signing-profile resource node.
	profile := resourceByType(t, envelopes, awscloud.ResourceTypeSignerSigningProfile)
	if got, want := profile.Payload["resource_id"], testProfileARN; got != want {
		t.Fatalf("profile resource_id = %#v, want %q", got, want)
	}
	if got, want := profile.Payload["arn"], testProfileARN; got != want {
		t.Fatalf("profile arn = %#v, want %q", got, want)
	}
	if got, want := profile.Payload["state"], "Active"; got != want {
		t.Fatalf("profile state = %#v, want %q", got, want)
	}
	profileAttrs := attributesOf(t, profile)
	assertAttribute(t, profileAttrs, "platform_id", testPlatformID)
	assertAttribute(t, profileAttrs, "profile_version", "AbCdEf123456")
	assertAttribute(t, profileAttrs, "signature_validity_type", "DAYS")
	assertAttribute(t, profileAttrs, "signature_validity_value", int32(135))
	assertAttribute(t, profileAttrs, "signing_image_format", "JSONEmbedded")
	assertAttribute(t, profileAttrs, "signing_parameter_names", []string{"release-channel"})

	// profile -> ACM certificate edge, keyed by the certificate ARN the ACM
	// scanner publishes as its certificate resource_id.
	profileACM := relationshipByType(t, envelopes, awscloud.RelationshipSignerProfileUsesACMCertificate)
	assertEdgeTarget(t, profileACM, awscloud.ResourceTypeACMCertificate, testCertificateARN)
	if got, want := profileACM.Payload["source_resource_id"], testProfileARN; got != want {
		t.Fatalf("profile->acm source_resource_id = %#v, want %q", got, want)
	}
	if got, want := profileACM.Payload["target_arn"], testCertificateARN; got != want {
		t.Fatalf("profile->acm target_arn = %#v, want %q", got, want)
	}

	// profile -> signing-platform internal edge, keyed by the bare platform id.
	profilePlatform := relationshipByType(t, envelopes, awscloud.RelationshipSignerProfileUsesSigningPlatform)
	assertEdgeTarget(t, profilePlatform, awscloud.ResourceTypeSignerSigningPlatform, testPlatformID)
	if got, want := profilePlatform.Payload["source_resource_id"], testProfileARN; got != want {
		t.Fatalf("profile->platform source_resource_id = %#v, want %q", got, want)
	}
	if got := profilePlatform.Payload["target_arn"]; got != "" {
		t.Fatalf("profile->platform target_arn = %#v, want empty (platforms carry no ARN)", got)
	}

	// No signing-material private key, signed-object payload, or signing-job
	// leakage anywhere in the resource payloads.
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		attrs, _ := envelope.Payload["attributes"].(map[string]any)
		for _, forbidden := range []string{
			"private_key", "signing_material", "signed_object", "signing_parameters",
			"jobs", "signing_jobs", "payload", "certificate_body", "revocation_record",
		} {
			if _, exists := attrs[forbidden]; exists {
				t.Fatalf("%s attribute persisted; Signer scanner must stay metadata-only", forbidden)
			}
		}
	}
}

func TestScannerOmitsRelationshipsWhenDependenciesAbsent(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Profiles: []SigningProfile{{
		ARN:  testProfileARN,
		Name: "lambda_release",
		// No platform id, no certificate: no edges.
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, envelope := range envelopes {
		if envelope.FactKind == facts.AWSRelationshipFactKind {
			t.Fatalf("unexpected relationship emitted: %#v", envelope.Payload)
		}
	}
}

func TestScannerOmitsACMEdgeForNonARNCertificateButKeepsValue(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Profiles: []SigningProfile{{
		ARN:            testProfileARN,
		Name:           "lambda_release",
		CertificateARN: "not-an-arn",
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == awscloud.RelationshipSignerProfileUsesACMCertificate {
			t.Fatalf("ACM edge emitted for a non-ARN certificate identifier")
		}
	}
	profile := resourceByType(t, envelopes, awscloud.ResourceTypeSignerSigningProfile)
	assertAttribute(t, attributesOf(t, profile), "certificate_arn", "not-an-arn")
}

func TestScannerRelationshipsSatisfyGraphJoinGuard(t *testing.T) {
	boundary := testBoundary()
	profile := SigningProfile{
		ARN:            testProfileARN,
		Name:           "lambda_release",
		PlatformID:     testPlatformID,
		CertificateARN: testCertificateARN,
	}
	var observations []awscloud.RelationshipObservation
	for _, rel := range []*awscloud.RelationshipObservation{
		profileACMCertificateRelationship(boundary, profile),
		profileSigningPlatformRelationship(boundary, profile),
	} {
		if rel == nil {
			t.Fatalf("expected non-nil relationship for fully populated fixture")
		}
		observations = append(observations, *rel)
	}
	relguard.AssertObservations(t, observations...)
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceS3

	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func TestScannerRequiresClient(t *testing.T) {
	_, err := (Scanner{}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want client-required error")
	}
}

func TestScannerEmitsThrottleWarningFacts(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		Profiles: []SigningProfile{{ARN: testProfileARN, Name: "lambda_release"}},
		Warnings: []awscloud.WarningObservation{{
			Boundary:       testBoundary(),
			WarningKind:    awscloud.WarningThrottleSustained,
			ErrorClass:     "throttled",
			Message:        "Signer ListSigningPlatforms throttled after SDK retries; platform metadata omitted for this scan",
			SourceRecordID: "signer_platforms_throttled",
		}},
	}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	warning := warningByKind(t, envelopes, awscloud.WarningThrottleSustained)
	if got := warning.Payload["error_class"]; got != "throttled" {
		t.Fatalf("warning error_class = %#v, want throttled", got)
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceSigner,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:signer:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 14, 18, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	snapshot Snapshot
}

func (c fakeClient) Snapshot(context.Context) (Snapshot, error) {
	return c.snapshot, nil
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

func relationshipByType(t *testing.T, envelopes []facts.Envelope, relationshipType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			return envelope
		}
	}
	t.Fatalf("missing relationship_type %q in %#v", relationshipType, envelopes)
	return facts.Envelope{}
}

func warningByKind(t *testing.T, envelopes []facts.Envelope, warningKind string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSWarningFactKind {
			continue
		}
		if got, _ := envelope.Payload["warning_kind"].(string); got == warningKind {
			return envelope
		}
	}
	t.Fatalf("missing warning_kind %q in %#v", warningKind, envelopes)
	return facts.Envelope{}
}

func assertEdgeTarget(t *testing.T, envelope facts.Envelope, targetType, targetResourceID string) {
	t.Helper()
	if got := envelope.Payload["target_type"]; got != targetType {
		t.Fatalf("target_type = %#v, want %q", got, targetType)
	}
	if got := envelope.Payload["target_resource_id"]; got != targetResourceID {
		t.Fatalf("target_resource_id = %#v, want %q", got, targetResourceID)
	}
}

func attributesOf(t *testing.T, envelope facts.Envelope) map[string]any {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	return attributes
}

func assertAttribute(t *testing.T, attributes map[string]any, key string, want any) {
	t.Helper()
	got, exists := attributes[key]
	if !exists {
		t.Fatalf("missing attribute %q in %#v", key, attributes)
	}
	if !valuesEqual(got, want) {
		t.Fatalf("attribute %q = %#v, want %#v", key, got, want)
	}
}

func valuesEqual(got any, want any) bool {
	switch want := want.(type) {
	case []string:
		gotSlice, ok := got.([]string)
		if !ok || len(gotSlice) != len(want) {
			return false
		}
		for i := range want {
			if gotSlice[i] != want[i] {
				return false
			}
		}
		return true
	default:
		return got == want
	}
}
