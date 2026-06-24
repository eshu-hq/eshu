// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package auditmanager

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	testAssessmentARN = "arn:aws:auditmanager:us-east-1:123456789012:assessment/a1b2c3d4-1111-2222-3333-444455556666"
	testFrameworkARN  = "arn:aws:auditmanager:us-east-1:123456789012:assessmentFramework/f1f2f3f4-aaaa-bbbb-cccc-ddddeeeeffff"
	testControlARN    = "arn:aws:auditmanager:us-east-1:123456789012:control/c1c2c3c4-9999-8888-7777-666655554444"
	testKMSARN        = "arn:aws:kms:us-east-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
)

func fullSnapshot() Snapshot {
	return Snapshot{
		KMSKeyARN: testKMSARN,
		Assessments: []Assessment{{
			ARN:                    testAssessmentARN,
			ID:                     "a1b2c3d4-1111-2222-3333-444455556666",
			Name:                   "soc2-prod",
			ComplianceType:         "SOC 2",
			Status:                 "ACTIVE",
			FrameworkARN:           testFrameworkARN,
			FrameworkID:            "f1f2f3f4-aaaa-bbbb-cccc-ddddeeeeffff",
			ReportsS3Destination:   "s3://audit-reports-bucket/exports",
			ReportsDestinationType: "S3",
			ScopeAccountIDs:        []string{"123456789012", "210987654321"},
			ScopeServiceNames:      nil,
			CreationTime:           time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
			LastUpdatedTime:        time.Date(2026, 5, 20, 9, 30, 0, 0, time.UTC),
			Tags:                   map[string]string{"Environment": "prod"},
		}},
		Frameworks: []Framework{{
			ARN:              testFrameworkARN,
			ID:               "f1f2f3f4-aaaa-bbbb-cccc-ddddeeeeffff",
			Name:             "SOC 2",
			ComplianceType:   "SOC 2",
			Type:             "Standard",
			ControlSetsCount: 5,
			ControlsCount:    61,
			CreatedAt:        time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
			LastUpdatedAt:    time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC),
		}},
		Controls: []Control{{
			ARN:            testControlARN,
			ID:             "c1c2c3c4-9999-8888-7777-666655554444",
			Name:           "Logging enabled",
			Type:           "Standard",
			ControlSources: "AWS Config, AWS Security Hub",
			CreatedAt:      time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
			LastUpdatedAt:  time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC),
		}},
	}
}

func TestScannerEmitsAuditManagerMetadataAndRelationships(t *testing.T) {
	envelopes, err := (Scanner{Client: fakeClient{snapshot: fullSnapshot()}}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	// Assessment resource node.
	assessment := resourceByType(t, envelopes, awscloud.ResourceTypeAuditManagerAssessment)
	if got, want := assessment.Payload["resource_id"], testAssessmentARN; got != want {
		t.Fatalf("assessment resource_id = %#v, want %q", got, want)
	}
	if got, want := assessment.Payload["state"], "ACTIVE"; got != want {
		t.Fatalf("assessment state = %#v, want %q", got, want)
	}
	aAttrs := attributesOf(t, assessment)
	assertAttribute(t, aAttrs, "compliance_type", "SOC 2")
	assertAttribute(t, aAttrs, "framework_id", "f1f2f3f4-aaaa-bbbb-cccc-ddddeeeeffff")
	assertAttribute(t, aAttrs, "scope_account_ids", []string{"123456789012", "210987654321"})

	// Framework resource node.
	framework := resourceByType(t, envelopes, awscloud.ResourceTypeAuditManagerFramework)
	if got, want := framework.Payload["resource_id"], testFrameworkARN; got != want {
		t.Fatalf("framework resource_id = %#v, want %q", got, want)
	}
	fAttrs := attributesOf(t, framework)
	assertAttribute(t, fAttrs, "framework_type", "Standard")
	assertAttribute(t, fAttrs, "controls_count", int32(61))

	// Control resource node.
	control := resourceByType(t, envelopes, awscloud.ResourceTypeAuditManagerControl)
	if got, want := control.Payload["resource_id"], testControlARN; got != want {
		t.Fatalf("control resource_id = %#v, want %q", got, want)
	}
	cAttrs := attributesOf(t, control)
	assertAttribute(t, cAttrs, "control_type", "Standard")
	assertAttribute(t, cAttrs, "control_sources", "AWS Config, AWS Security Hub")

	// assessment -> framework edge, keyed by the framework ARN.
	frameworkEdge := relationshipByType(t, envelopes, awscloud.RelationshipAuditManagerAssessmentUsesFramework)
	assertEdgeTarget(t, frameworkEdge, awscloud.ResourceTypeAuditManagerFramework, testFrameworkARN)
	if got, want := frameworkEdge.Payload["source_resource_id"], testAssessmentARN; got != want {
		t.Fatalf("assessment->framework source_resource_id = %#v, want %q", got, want)
	}

	// assessment -> S3 reports bucket edge, keyed by the synthesized bucket ARN.
	s3Edge := relationshipByType(t, envelopes, awscloud.RelationshipAuditManagerAssessmentReportsToS3)
	wantBucketARN := "arn:aws:s3:::audit-reports-bucket"
	assertEdgeTarget(t, s3Edge, awscloud.ResourceTypeS3Bucket, wantBucketARN)
	if got, want := s3Edge.Payload["target_arn"], wantBucketARN; got != want {
		t.Fatalf("assessment->s3 target_arn = %#v, want %q", got, want)
	}

	// assessment -> KMS key edge, keyed by the account settings key ARN.
	kmsEdge := relationshipByType(t, envelopes, awscloud.RelationshipAuditManagerAssessmentEncryptedWithKMSKey)
	assertEdgeTarget(t, kmsEdge, awscloud.ResourceTypeKMSKey, testKMSARN)
	if got, want := kmsEdge.Payload["target_arn"], testKMSARN; got != want {
		t.Fatalf("assessment->kms target_arn = %#v, want %q", got, want)
	}

	// assessment -> account edges, keyed by the partition-aware account root ARN.
	accountEdge := relationshipByType(t, envelopes, awscloud.RelationshipAuditManagerAssessmentInAccount)
	assertEdgeTarget(t, accountEdge, awscloud.ResourceTypeAWSAccount, "arn:aws:iam::123456789012:root")
	if accountEdges := countRelationships(envelopes, awscloud.RelationshipAuditManagerAssessmentInAccount); accountEdges != 2 {
		t.Fatalf("assessment->account edge count = %d, want 2", accountEdges)
	}

	// No evidence / narrative / report-url leakage anywhere in the resource payloads.
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		attrs, _ := envelope.Payload["attributes"].(map[string]any)
		for _, forbidden := range []string{
			"evidence", "evidence_content", "testing_information",
			"action_plan_instructions", "control_mapping_sources",
			"description", "report_url", "change_logs", "delegation_comment",
		} {
			if _, exists := attrs[forbidden]; exists {
				t.Fatalf("%s attribute persisted; Audit Manager scanner must stay metadata-only", forbidden)
			}
		}
	}
}

func TestScannerSynthesizesGovCloudBucketAndAccountARNs(t *testing.T) {
	boundary := testBoundary()
	boundary.Region = "us-gov-west-1"
	snapshot := Snapshot{Assessments: []Assessment{{
		ARN:                  "arn:aws-us-gov:auditmanager:us-gov-west-1:123456789012:assessment/gov-assess",
		ID:                   "gov-assess",
		Name:                 "gov",
		ReportsS3Destination: "s3://gov-reports-bucket",
		ScopeAccountIDs:      []string{"123456789012"},
	}}}

	envelopes, err := (Scanner{Client: fakeClient{snapshot: snapshot}}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	s3Edge := relationshipByType(t, envelopes, awscloud.RelationshipAuditManagerAssessmentReportsToS3)
	if got, want := s3Edge.Payload["target_resource_id"], "arn:aws-us-gov:s3:::gov-reports-bucket"; got != want {
		t.Fatalf("GovCloud assessment->s3 target_resource_id = %#v, want %q", got, want)
	}
	accountEdge := relationshipByType(t, envelopes, awscloud.RelationshipAuditManagerAssessmentInAccount)
	if got, want := accountEdge.Payload["target_resource_id"], "arn:aws-us-gov:iam::123456789012:root"; got != want {
		t.Fatalf("GovCloud assessment->account target_resource_id = %#v, want %q", got, want)
	}
}

func TestScannerSynthesizesChinaBucketARN(t *testing.T) {
	boundary := testBoundary()
	boundary.Region = "cn-north-1"
	snapshot := Snapshot{Assessments: []Assessment{{
		ARN:                  "arn:aws-cn:auditmanager:cn-north-1:123456789012:assessment/cn-assess",
		ID:                   "cn-assess",
		Name:                 "cn",
		ReportsS3Destination: "s3://cn-reports-bucket/exports",
	}}}

	envelopes, err := (Scanner{Client: fakeClient{snapshot: snapshot}}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	s3Edge := relationshipByType(t, envelopes, awscloud.RelationshipAuditManagerAssessmentReportsToS3)
	if got, want := s3Edge.Payload["target_arn"], "arn:aws-cn:s3:::cn-reports-bucket"; got != want {
		t.Fatalf("China assessment->s3 target_arn = %#v, want %q", got, want)
	}
}

func TestScannerOmitsRelationshipsWhenDependenciesAbsent(t *testing.T) {
	snapshot := Snapshot{Assessments: []Assessment{{
		ARN:  testAssessmentARN,
		ID:   "a1b2c3d4-1111-2222-3333-444455556666",
		Name: "bare",
		// No framework, no S3 destination, no KMS key, no scope accounts.
	}}}

	envelopes, err := (Scanner{Client: fakeClient{snapshot: snapshot}}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, envelope := range envelopes {
		if envelope.FactKind == facts.AWSRelationshipFactKind {
			t.Fatalf("unexpected relationship emitted: %#v", envelope.Payload)
		}
	}
}

func TestScannerOmitsKMSEdgeForNonARNKeyButKeepsValue(t *testing.T) {
	snapshot := Snapshot{
		KMSKeyARN: "alias/auditmanager",
		Assessments: []Assessment{{
			ARN:  testAssessmentARN,
			ID:   "a1b2c3d4-1111-2222-3333-444455556666",
			Name: "alias-key",
		}},
	}

	envelopes, err := (Scanner{Client: fakeClient{snapshot: snapshot}}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	kmsEdge := relationshipByType(t, envelopes, awscloud.RelationshipAuditManagerAssessmentEncryptedWithKMSKey)
	if got, want := kmsEdge.Payload["target_resource_id"], "alias/auditmanager"; got != want {
		t.Fatalf("kms target_resource_id = %#v, want %q", got, want)
	}
	if got := kmsEdge.Payload["target_arn"]; got != "" {
		t.Fatalf("kms target_arn = %#v, want empty for non-ARN key identifier", got)
	}
}

func TestScannerRelationshipsSatisfyGraphJoinGuard(t *testing.T) {
	boundary := testBoundary()
	assessment := fullSnapshot().Assessments[0]
	var observations []awscloud.RelationshipObservation
	rels := []*awscloud.RelationshipObservation{
		assessmentFrameworkRelationship(boundary, assessment),
		assessmentReportsS3Relationship(boundary, assessment),
		assessmentKMSRelationship(boundary, assessment, testKMSARN),
	}
	for _, accountID := range assessment.ScopeAccountIDs {
		rels = append(rels, assessmentAccountRelationship(boundary, assessment, accountID))
	}
	for _, rel := range rels {
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

func TestScannerEmitsThrottleWarningFacts(t *testing.T) {
	snapshot := Snapshot{
		Assessments: []Assessment{{ARN: testAssessmentARN, ID: "a", Name: "metrics"}},
		Warnings: []awscloud.WarningObservation{{
			Boundary:       testBoundary(),
			WarningKind:    awscloud.WarningThrottleSustained,
			ErrorClass:     "throttled",
			Message:        "Audit Manager ListControls throttled after SDK retries; control metadata omitted for this scan",
			SourceRecordID: "auditmanager_controls_throttled",
		}},
	}

	envelopes, err := (Scanner{Client: fakeClient{snapshot: snapshot}}).Scan(context.Background(), testBoundary())
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
		ServiceKind:         awscloud.ServiceAuditManager,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:auditmanager:1",
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

func countRelationships(envelopes []facts.Envelope, relationshipType string) int {
	count := 0
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			count++
		}
	}
	return count
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
