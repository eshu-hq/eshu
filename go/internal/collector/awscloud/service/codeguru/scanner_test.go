// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codeguru

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	testAssociationARN = "arn:aws:codeguru-reviewer:us-east-1:123456789012:association:11111111-2222-3333-4444-555555555555"
	testGroupARN       = "arn:aws:codeguru-profiler:us-east-1:123456789012:profilingGroup/payments-api"
	testConnectionARN  = "arn:aws:codestar-connections:us-east-1:123456789012:connection/abcd"
	wantCodeCommitARN  = "arn:aws:codecommit:us-east-1:123456789012:payments-api"
)

func TestScannerEmitsCodeGuruMetadataAndRelationships(t *testing.T) {
	enabled := true
	client := fakeClient{snapshot: Snapshot{
		RepositoryAssociations: []RepositoryAssociation{{
			ARN:              testAssociationARN,
			AssociationID:    "11111111-2222-3333-4444-555555555555",
			Name:             "payments-api",
			Owner:            "123456789012",
			ProviderType:     "CodeCommit",
			State:            "Associated",
			KMSKeyID:         "arn:aws:kms:us-east-1:123456789012:key/abc",
			EncryptionOption: "CUSTOMER_MANAGED_CMK",
			CreatedAt:        time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
			Tags:             map[string]string{"Team": "payments"},
		}},
		ProfilingGroups: []ProfilingGroup{{
			ARN:              testGroupARN,
			Name:             "payments-api",
			ComputePlatform:  "AWSLambda",
			ProfilingEnabled: &enabled,
			CreatedAt:        time.Date(2026, 5, 14, 12, 5, 0, 0, time.UTC),
			Tags:             map[string]string{"Team": "payments"},
		}},
	}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	// Repository association resource node.
	association := resourceByType(t, envelopes, awscloud.ResourceTypeCodeGuruRepositoryAssociation)
	if got, want := association.Payload["resource_id"], testAssociationARN; got != want {
		t.Fatalf("association resource_id = %#v, want %q", got, want)
	}
	if got, want := association.Payload["state"], "Associated"; got != want {
		t.Fatalf("association state = %#v, want %q", got, want)
	}
	assocAttrs := attributesOf(t, association)
	assertAttribute(t, assocAttrs, "provider_type", "CodeCommit")
	assertAttribute(t, assocAttrs, "owner", "123456789012")
	assertAttribute(t, assocAttrs, "encryption_option", "CUSTOMER_MANAGED_CMK")

	// Profiling group resource node.
	group := resourceByType(t, envelopes, awscloud.ResourceTypeCodeGuruProfilingGroup)
	if got, want := group.Payload["resource_id"], testGroupARN; got != want {
		t.Fatalf("profiling group resource_id = %#v, want %q", got, want)
	}
	groupAttrs := attributesOf(t, group)
	assertAttribute(t, groupAttrs, "compute_platform", "AWSLambda")
	assertAttribute(t, groupAttrs, "profiling_enabled", true)

	// association -> CodeCommit repo edge, keyed by the synthesized partition-aware
	// ARN the CodeCommit scanner publishes for its repository node.
	edge := relationshipByType(t, envelopes, awscloud.RelationshipCodeGuruAssociationReviewsCodeCommitRepository)
	assertEdgeTarget(t, edge, awscloud.ResourceTypeCodeCommitRepository, wantCodeCommitARN)
	if got, want := edge.Payload["source_resource_id"], testAssociationARN; got != want {
		t.Fatalf("edge source_resource_id = %#v, want %q", got, want)
	}
	if got, want := edge.Payload["target_arn"], wantCodeCommitARN; got != want {
		t.Fatalf("edge target_arn = %#v, want %q", got, want)
	}

	// No profiling data, findings, or recommendation content anywhere.
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		attrs, _ := envelope.Payload["attributes"].(map[string]any)
		for _, forbidden := range []string{
			"profile", "profiles", "samples", "flame_graph", "frames",
			"recommendations", "findings", "code_review", "anomalies",
			"recommendation_feedback", "metered_lines_of_code", "source_code",
		} {
			if _, exists := attrs[forbidden]; exists {
				t.Fatalf("%s attribute persisted; CodeGuru scanner must stay metadata-only", forbidden)
			}
		}
	}
}

func TestScannerSynthesizesGovCloudCodeCommitARN(t *testing.T) {
	boundary := testBoundary()
	boundary.Region = "us-gov-west-1"
	client := fakeClient{snapshot: Snapshot{RepositoryAssociations: []RepositoryAssociation{{
		ARN:          "arn:aws-us-gov:codeguru-reviewer:us-gov-west-1:123456789012:association:gov",
		Name:         "gov-repo",
		Owner:        "123456789012",
		ProviderType: "CodeCommit",
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	edge := relationshipByType(t, envelopes, awscloud.RelationshipCodeGuruAssociationReviewsCodeCommitRepository)
	wantARN := "arn:aws-us-gov:codecommit:us-gov-west-1:123456789012:gov-repo"
	if got := edge.Payload["target_resource_id"]; got != wantARN {
		t.Fatalf("GovCloud edge target_resource_id = %#v, want %q", got, wantARN)
	}
	if got := edge.Payload["target_arn"]; got != wantARN {
		t.Fatalf("GovCloud edge target_arn = %#v, want %q", got, wantARN)
	}
}

func TestScannerSynthesizesChinaCodeCommitARN(t *testing.T) {
	boundary := testBoundary()
	boundary.Region = "cn-north-1"
	client := fakeClient{snapshot: Snapshot{RepositoryAssociations: []RepositoryAssociation{{
		ARN:          "arn:aws-cn:codeguru-reviewer:cn-north-1:123456789012:association:cn",
		Name:         "cn-repo",
		Owner:        "123456789012",
		ProviderType: "CodeCommit",
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	edge := relationshipByType(t, envelopes, awscloud.RelationshipCodeGuruAssociationReviewsCodeCommitRepository)
	wantARN := "arn:aws-cn:codecommit:cn-north-1:123456789012:cn-repo"
	if got := edge.Payload["target_arn"]; got != wantARN {
		t.Fatalf("China edge target_arn = %#v, want %q", got, wantARN)
	}
}

func TestScannerSkipsEdgeForNonCodeCommitProvider(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{RepositoryAssociations: []RepositoryAssociation{{
		ARN:           testAssociationARN,
		Name:          "web-app",
		Owner:         "octo-org",
		ProviderType:  "GitHub",
		ConnectionARN: testConnectionARN,
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, envelope := range envelopes {
		if envelope.FactKind == facts.AWSRelationshipFactKind {
			t.Fatalf("unexpected edge for non-CodeCommit provider: %#v", envelope.Payload)
		}
	}
	// The GitHub connection reference is still recorded as a resource attribute.
	association := resourceByType(t, envelopes, awscloud.ResourceTypeCodeGuruRepositoryAssociation)
	assertAttribute(t, attributesOf(t, association), "connection_arn", testConnectionARN)
}

func TestScannerSkipsCodeCommitEdgeWhenOwnerMissing(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{RepositoryAssociations: []RepositoryAssociation{{
		ARN:          testAssociationARN,
		Name:         "payments-api",
		ProviderType: "CodeCommit",
		// No Owner account: the CodeCommit ARN cannot be synthesized, so the edge
		// is skipped rather than dangled.
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, envelope := range envelopes {
		if envelope.FactKind == facts.AWSRelationshipFactKind {
			t.Fatalf("unexpected edge when owner account missing: %#v", envelope.Payload)
		}
	}
}

func TestScannerRelationshipsSatisfyGraphJoinGuard(t *testing.T) {
	boundary := testBoundary()
	association := RepositoryAssociation{
		ARN:          testAssociationARN,
		Name:         "payments-api",
		Owner:        "123456789012",
		ProviderType: "CodeCommit",
	}
	rel := associationCodeCommitRelationship(boundary, association)
	if rel == nil {
		t.Fatalf("expected non-nil relationship for fully populated CodeCommit association")
	}
	relguard.AssertObservations(t, *rel)
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
		ProfilingGroups: []ProfilingGroup{{ARN: testGroupARN, Name: "payments-api"}},
		Warnings: []awscloud.WarningObservation{{
			Boundary:       testBoundary(),
			WarningKind:    awscloud.WarningThrottleSustained,
			ErrorClass:     "throttled",
			Message:        "CodeGuru ListRepositoryAssociations throttled after SDK retries; association metadata omitted for this scan",
			SourceRecordID: "codeguru_associations_throttled",
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

func TestScannerHandlesEmptyAccountCleanly(t *testing.T) {
	envelopes, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if len(envelopes) != 0 {
		t.Fatalf("Scan() on empty account = %d envelopes, want 0", len(envelopes))
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceCodeGuru,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:codeguru:1",
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
	if got != want {
		t.Fatalf("attribute %q = %#v, want %#v", key, got, want)
	}
}
