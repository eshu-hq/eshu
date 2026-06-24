// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rolesanywhere

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	testTrustAnchorARN = "arn:aws:rolesanywhere:us-east-1:123456789012:trust-anchor/11111111-1111-1111-1111-111111111111"
	testProfileARN     = "arn:aws:rolesanywhere:us-east-1:123456789012:profile/22222222-2222-2222-2222-222222222222"
	testCRLARN         = "arn:aws:rolesanywhere:us-east-1:123456789012:crl/33333333-3333-3333-3333-333333333333"
	testRoleARN        = "arn:aws:iam::123456789012:role/build-runner"
	testCAARN          = "arn:aws:acm-pca:us-east-1:123456789012:certificate-authority/44444444-4444-4444-4444-444444444444"
)

func TestScannerEmitsRolesAnywhereMetadataAndRelationships(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		TrustAnchors: []TrustAnchor{{
			ARN:           testTrustAnchorARN,
			TrustAnchorID: "11111111-1111-1111-1111-111111111111",
			Name:          "corp-pca",
			Enabled:       true,
			SourceType:    "AWS_ACM_PCA",
			ACMPCAArn:     testCAARN,
			CreatedAt:     time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
			Tags:          map[string]string{"Environment": "prod"},
		}},
		Profiles: []Profile{{
			ARN:                   testProfileARN,
			ProfileID:             "22222222-2222-2222-2222-222222222222",
			Name:                  "ci-profile",
			Enabled:               true,
			DurationSeconds:       3600,
			AcceptRoleSessionName: true,
			HasSessionPolicy:      true,
			AttributeMappingCount: 2,
			RoleARNs:              []string{testRoleARN},
			ManagedPolicyARNs:     []string{"arn:aws:iam::aws:policy/ReadOnlyAccess"},
			CreatedAt:             time.Date(2026, 5, 15, 9, 0, 0, 0, time.UTC),
			Tags:                  map[string]string{"Team": "platform"},
		}},
		CRLs: []CRL{{
			ARN:            testCRLARN,
			CRLID:          "33333333-3333-3333-3333-333333333333",
			Name:           "corp-crl",
			Enabled:        true,
			TrustAnchorARN: testTrustAnchorARN,
			CreatedAt:      time.Date(2026, 5, 16, 8, 0, 0, 0, time.UTC),
		}},
	}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	// Trust anchor resource node.
	anchor := resourceByType(t, envelopes, awscloud.ResourceTypeRolesAnywhereTrustAnchor)
	if got, want := anchor.Payload["resource_id"], testTrustAnchorARN; got != want {
		t.Fatalf("trust anchor resource_id = %#v, want %q", got, want)
	}
	anchorAttrs := attributesOf(t, anchor)
	assertAttribute(t, anchorAttrs, "source_type", "AWS_ACM_PCA")
	assertAttribute(t, anchorAttrs, "enabled", true)

	// Profile resource node.
	profile := resourceByType(t, envelopes, awscloud.ResourceTypeRolesAnywhereProfile)
	if got, want := profile.Payload["resource_id"], testProfileARN; got != want {
		t.Fatalf("profile resource_id = %#v, want %q", got, want)
	}
	profileAttrs := attributesOf(t, profile)
	assertAttribute(t, profileAttrs, "duration_seconds", int32(3600))
	assertAttribute(t, profileAttrs, "session_policy_configured", true)
	assertAttribute(t, profileAttrs, "attribute_mapping_count", 2)
	assertAttribute(t, profileAttrs, "role_arns", []string{testRoleARN})

	// CRL resource node.
	crl := resourceByType(t, envelopes, awscloud.ResourceTypeRolesAnywhereCRL)
	if got, want := crl.Payload["resource_id"], testCRLARN; got != want {
		t.Fatalf("crl resource_id = %#v, want %q", got, want)
	}

	// profile -> IAM role edge, keyed by the role ARN the IAM scanner publishes.
	profileRole := relationshipByType(t, envelopes, awscloud.RelationshipRolesAnywhereProfileAssumesRole)
	assertEdgeTarget(t, profileRole, awscloud.ResourceTypeIAMRole, testRoleARN)
	if got, want := profileRole.Payload["source_resource_id"], testProfileARN; got != want {
		t.Fatalf("profile->role source_resource_id = %#v, want %q", got, want)
	}
	if got, want := profileRole.Payload["target_arn"], testRoleARN; got != want {
		t.Fatalf("profile->role target_arn = %#v, want %q", got, want)
	}

	// trust anchor -> ACM PCA CA edge, keyed by the CA ARN the acmpca scanner publishes.
	anchorCA := relationshipByType(t, envelopes, awscloud.RelationshipRolesAnywhereTrustAnchorUsesACMPCA)
	assertEdgeTarget(t, anchorCA, awscloud.ResourceTypeACMPCACertificateAuthority, testCAARN)
	if got, want := anchorCA.Payload["source_resource_id"], testTrustAnchorARN; got != want {
		t.Fatalf("anchor->ca source_resource_id = %#v, want %q", got, want)
	}

	// CRL -> trust anchor edge, keyed by the trust-anchor ARN the trust-anchor node publishes.
	crlAnchor := relationshipByType(t, envelopes, awscloud.RelationshipRolesAnywhereCRLValidatesTrustAnchor)
	assertEdgeTarget(t, crlAnchor, awscloud.ResourceTypeRolesAnywhereTrustAnchor, testTrustAnchorARN)
	if got, want := crlAnchor.Payload["source_resource_id"], testCRLARN; got != want {
		t.Fatalf("crl->anchor source_resource_id = %#v, want %q", got, want)
	}

	// No certificate material, CRL body, session policy, or credential leakage in any resource payload.
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		attrs, _ := envelope.Payload["attributes"].(map[string]any)
		for _, forbidden := range []string{
			"crl_data", "crl_body", "x509_certificate_data", "certificate_data",
			"certificate_bundle", "session_policy", "session_policy_document",
			"credentials", "private_key", "attribute_mappings", "mapping_rules",
		} {
			if _, exists := attrs[forbidden]; exists {
				t.Fatalf("%s attribute persisted; Roles Anywhere scanner must stay metadata-only", forbidden)
			}
		}
	}
}

func TestScannerSynthesizesNoLiteralPartition(t *testing.T) {
	boundary := testBoundary()
	boundary.Region = "us-gov-west-1"
	client := fakeClient{snapshot: Snapshot{
		Profiles: []Profile{{
			ARN:       "arn:aws-us-gov:rolesanywhere:us-gov-west-1:123456789012:profile/abc",
			ProfileID: "abc",
			Name:      "gov-profile",
			RoleARNs:  []string{"arn:aws-us-gov:iam::123456789012:role/gov-runner"},
		}},
	}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	profileRole := relationshipByType(t, envelopes, awscloud.RelationshipRolesAnywhereProfileAssumesRole)
	// The scanner forwards the reported role ARN verbatim; it must remain the
	// GovCloud partition ARN, never rewritten to a literal arn:aws:.
	if got, want := profileRole.Payload["target_resource_id"], "arn:aws-us-gov:iam::123456789012:role/gov-runner"; got != want {
		t.Fatalf("GovCloud profile->role target_resource_id = %#v, want %q", got, want)
	}
}

func TestScannerOmitsRelationshipsWhenDependenciesAbsent(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		TrustAnchors: []TrustAnchor{{
			ARN:        testTrustAnchorARN,
			Name:       "bundle-anchor",
			SourceType: "CERTIFICATE_BUNDLE",
			// No ACM PCA ARN: no trust-anchor->CA edge.
		}},
		Profiles: []Profile{{
			ARN:  testProfileARN,
			Name: "no-roles",
			// No role ARNs: no profile->role edge.
		}},
		CRLs: []CRL{{
			ARN:  testCRLARN,
			Name: "detached-crl",
			// No trust anchor ARN: no crl->anchor edge.
		}},
	}}

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

func TestScannerOmitsACMPCAEdgeForCertificateBundleAnchor(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		TrustAnchors: []TrustAnchor{{
			ARN:        testTrustAnchorARN,
			Name:       "bundle-anchor",
			SourceType: "CERTIFICATE_BUNDLE",
			// A certificate bundle anchor never carries an ACM PCA ARN.
		}},
	}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == awscloud.RelationshipRolesAnywhereTrustAnchorUsesACMPCA {
			t.Fatalf("unexpected ACM PCA edge for certificate-bundle trust anchor")
		}
	}
}

func TestScannerDeduplicatesProfileRoleEdges(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		Profiles: []Profile{{
			ARN:      testProfileARN,
			Name:     "dup-roles",
			RoleARNs: []string{testRoleARN, testRoleARN, "  "},
		}},
	}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	count := 0
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == awscloud.RelationshipRolesAnywhereProfileAssumesRole {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("profile->role edge count = %d, want 1 (duplicates and blanks dropped)", count)
	}
}

func TestScannerRelationshipsSatisfyGraphJoinGuard(t *testing.T) {
	boundary := testBoundary()
	anchor := TrustAnchor{ARN: testTrustAnchorARN, SourceType: "AWS_ACM_PCA", ACMPCAArn: testCAARN}
	profile := Profile{ARN: testProfileARN, RoleARNs: []string{testRoleARN}}
	crl := CRL{ARN: testCRLARN, TrustAnchorARN: testTrustAnchorARN}

	var observations []awscloud.RelationshipObservation
	observations = append(observations, profileRoleRelationships(boundary, profile)...)
	for _, rel := range []*awscloud.RelationshipObservation{
		trustAnchorACMPCARelationship(boundary, anchor),
		crlTrustAnchorRelationship(boundary, crl),
	} {
		if rel == nil {
			t.Fatalf("expected non-nil relationship for fully populated fixture")
		}
		observations = append(observations, *rel)
	}
	if len(observations) != 3 {
		t.Fatalf("relationship count = %d, want 3", len(observations))
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

func TestScannerEmitsThrottleWarningFacts(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		TrustAnchors: []TrustAnchor{{ARN: testTrustAnchorARN, Name: "anchor"}},
		Warnings: []awscloud.WarningObservation{{
			Boundary:       testBoundary(),
			WarningKind:    awscloud.WarningThrottleSustained,
			ErrorClass:     "throttled",
			Message:        "Roles Anywhere ListProfiles throttled after SDK retries; profile metadata omitted for this scan",
			SourceRecordID: "rolesanywhere_profiles_throttled",
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

func TestScannerRequiresClient(t *testing.T) {
	_, err := (Scanner{}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want missing-client error")
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceRolesAnywhere,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:rolesanywhere:1",
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
