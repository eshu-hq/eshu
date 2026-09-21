// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codecommit

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	testRepositoryARN = "arn:aws:codecommit:us-east-1:123456789012:payments-api"
	testKMSKeyARN     = "arn:aws:kms:us-east-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
	testSNSTopicARN   = "arn:aws:sns:us-east-1:123456789012:codecommit-notifications"
	testCloneURLHTTP  = "https://git-codecommit.us-east-1.amazonaws.com/v1/repos/payments-api"
	testCloneURLSSH   = "ssh://git-codecommit.us-east-1.amazonaws.com/v1/repos/payments-api"
)

func fullRepository() Repository {
	return Repository{
		ARN:            testRepositoryARN,
		Name:           "payments-api",
		ID:             "repo-1234",
		AccountID:      "123456789012",
		DefaultBranch:  "main",
		CloneURLHTTP:   testCloneURLHTTP,
		CloneURLSSH:    testCloneURLSSH,
		KMSKeyID:       testKMSKeyARN,
		CreatedAt:      time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC),
		LastModifiedAt: time.Date(2026, 5, 20, 14, 30, 0, 0, time.UTC),
		Triggers: []Trigger{
			{
				Name:           "notify-main",
				DestinationARN: testSNSTopicARN,
				Events:         []string{"all"},
				Branches:       []string{"main"},
			},
		},
		Tags: map[string]string{"Environment": "Prod"},
	}
}

func TestScannerEmitsRepositoryResourceWithMetadataOnly(t *testing.T) {
	envelopes, err := Scanner{Client: fakeClient{repositories: []Repository{fullRepository()}}}.
		Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	resource := resourceEnvelope(t, envelopes, awscloud.ResourceTypeCodeCommitRepository)
	if got, _ := resource.Payload["arn"].(string); got != testRepositoryARN {
		t.Fatalf("repository arn = %q, want %q", got, testRepositoryARN)
	}
	if got, _ := resource.Payload["resource_id"].(string); got != testRepositoryARN {
		t.Fatalf("repository resource_id = %q, want %q", got, testRepositoryARN)
	}
	attributes, _ := resource.Payload["attributes"].(map[string]any)
	if got, _ := attributes["default_branch"].(string); got != "main" {
		t.Fatalf("default_branch = %q, want main", got)
	}
	if got, _ := attributes["kms_key_id"].(string); got != testKMSKeyARN {
		t.Fatalf("kms_key_id = %q, want %q", got, testKMSKeyARN)
	}
	// Clone URL evidence is host-only: the full path (which can carry the repo
	// path and any credentials a clone URL string may embed) must not persist.
	if got, _ := attributes["clone_url_http_host"].(string); got != "git-codecommit.us-east-1.amazonaws.com" {
		t.Fatalf("clone_url_http_host = %q, want the host only", got)
	}
	if got, _ := attributes["clone_url_ssh_host"].(string); got != "git-codecommit.us-east-1.amazonaws.com" {
		t.Fatalf("clone_url_ssh_host = %q, want the host only", got)
	}
}

func TestScannerPublishesCodeToCloudCorrelationAnchors(t *testing.T) {
	envelopes, err := Scanner{Client: fakeClient{repositories: []Repository{fullRepository()}}}.
		Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	resource := resourceEnvelope(t, envelopes, awscloud.ResourceTypeCodeCommitRepository)
	anchors := stringSlice(resource.Payload["correlation_anchors"])
	// The repository name and full clone URLs are the join keys a CodeBuild
	// project, CodePipeline source action, or Amplify app reports for its Git
	// source, so they must be published as anchors for the code-to-cloud join.
	for _, want := range []string{"payments-api", testCloneURLHTTP, testCloneURLSSH} {
		if !containsString(anchors, want) {
			t.Fatalf("correlation anchors %v missing %q", anchors, want)
		}
	}
}

func TestScannerEmitsKMSKeyEncryptionEdge(t *testing.T) {
	envelopes, err := Scanner{Client: fakeClient{repositories: []Repository{fullRepository()}}}.
		Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	edge := relationshipEnvelope(t, envelopes, awscloud.RelationshipCodeCommitRepositoryEncryptedWithKMSKey)
	if got, _ := edge.Payload["target_type"].(string); got != awscloud.ResourceTypeKMSKey {
		t.Fatalf("kms edge target_type = %q, want %q", got, awscloud.ResourceTypeKMSKey)
	}
	if got, _ := edge.Payload["target_resource_id"].(string); got != testKMSKeyARN {
		t.Fatalf("kms edge target_resource_id = %q, want %q", got, testKMSKeyARN)
	}
	if got, _ := edge.Payload["source_resource_id"].(string); got != testRepositoryARN {
		t.Fatalf("kms edge source_resource_id = %q, want %q", got, testRepositoryARN)
	}
}

func TestScannerEmitsSNSTopicTriggerEdge(t *testing.T) {
	envelopes, err := Scanner{Client: fakeClient{repositories: []Repository{fullRepository()}}}.
		Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	edge := relationshipEnvelope(t, envelopes, awscloud.RelationshipCodeCommitRepositoryTriggersSNSTopic)
	if got, _ := edge.Payload["target_type"].(string); got != awscloud.ResourceTypeSNSTopic {
		t.Fatalf("sns edge target_type = %q, want %q", got, awscloud.ResourceTypeSNSTopic)
	}
	if got, _ := edge.Payload["target_resource_id"].(string); got != testSNSTopicARN {
		t.Fatalf("sns edge target_resource_id = %q, want %q", got, testSNSTopicARN)
	}
	if got, _ := edge.Payload["target_arn"].(string); got != testSNSTopicARN {
		t.Fatalf("sns edge target_arn = %q, want %q", got, testSNSTopicARN)
	}
}

func TestKMSEdgeKeysOnBareKeyID(t *testing.T) {
	repository := fullRepository()
	repository.KMSKeyID = "1234abcd-12ab-34cd-56ef-1234567890ab"
	relationship := kmsKeyRelationship(testBoundary(), repository)
	if relationship == nil {
		t.Fatal("expected a KMS edge for a bare key id")
	}
	// A bare key id must key on the bare id (matching the KMS scanner's
	// resource_id) and must not fabricate a target_arn that would never join.
	if relationship.TargetResourceID != repository.KMSKeyID {
		t.Fatalf("target_resource_id = %q, want bare key id %q", relationship.TargetResourceID, repository.KMSKeyID)
	}
	if relationship.TargetARN != "" {
		t.Fatalf("target_arn = %q, want empty for a bare key id", relationship.TargetARN)
	}
	relguard.AssertObservations(t, *relationship)
}

func TestRepositoryWithoutKeyEmitsNoKMSEdge(t *testing.T) {
	repository := fullRepository()
	repository.KMSKeyID = ""
	if relationship := kmsKeyRelationship(testBoundary(), repository); relationship != nil {
		t.Fatalf("expected no KMS edge when no key is reported, got %#v", relationship)
	}
}

func TestNonSNSTriggerDestinationEmitsNoEdge(t *testing.T) {
	repository := fullRepository()
	repository.Triggers = []Trigger{{
		Name:           "lambda-trigger",
		DestinationARN: "arn:aws:lambda:us-east-1:123456789012:function:on-push",
		Events:         []string{"all"},
	}}
	if rels := triggerRelationships(testBoundary(), repository); len(rels) != 0 {
		t.Fatalf("expected no SNS edge for a non-SNS trigger destination, got %#v", rels)
	}
}

// TestLambdaARNWithSNSSubstringEmitsNoEdge guards the ARN-service-segment match:
// a Lambda ARN whose resource portion produces a ":sns:" substring (a function
// literally named "sns" with an alias/version) must NOT be promoted to an SNS
// topic edge. A loose strings.Contains(":sns:") check would have mis-typed it.
func TestLambdaARNWithSNSSubstringEmitsNoEdge(t *testing.T) {
	repository := fullRepository()
	repository.Triggers = []Trigger{{
		Name:           "lambda-sns-named",
		DestinationARN: "arn:aws:lambda:us-east-1:123456789012:function:sns:PROD",
		Events:         []string{"all"},
	}}
	if rels := triggerRelationships(testBoundary(), repository); len(rels) != 0 {
		t.Fatalf("a Lambda ARN containing a ':sns:' substring must not emit an SNS edge, got %#v", rels)
	}
}

func TestDuplicateSNSTriggersCollapseToOneEdge(t *testing.T) {
	repository := fullRepository()
	repository.Triggers = []Trigger{
		{Name: "a", DestinationARN: testSNSTopicARN, Events: []string{"all"}},
		{Name: "b", DestinationARN: testSNSTopicARN, Events: []string{"createReference"}},
	}
	rels := triggerRelationships(testBoundary(), repository)
	if len(rels) != 1 {
		t.Fatalf("expected duplicate SNS destinations to collapse to one edge, got %d", len(rels))
	}
}

func TestEmittedRelationshipsSatisfyGraphJoinContract(t *testing.T) {
	repository := fullRepository()
	boundary := testBoundary()

	kms := kmsKeyRelationship(boundary, repository)
	if kms == nil {
		t.Fatal("kmsKeyRelationship did not emit an edge for a reported key")
	}
	relguard.AssertObservations(t, *kms)

	sns := triggerRelationships(boundary, repository)
	if len(sns) == 0 {
		t.Fatal("triggerRelationships did not emit an edge for an SNS trigger destination")
	}
	relguard.AssertObservations(t, sns...)
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceIAM
	if _, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary); err == nil {
		t.Fatal("Scan() error = nil, want service kind mismatch")
	}
}

func TestScannerRequiresClient(t *testing.T) {
	if _, err := (Scanner{}).Scan(context.Background(), testBoundary()); err == nil {
		t.Fatal("Scan() error = nil, want client-required error")
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceCodeCommit,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:codecommit:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	repositories []Repository
}

func (c fakeClient) ListRepositories(context.Context) ([]Repository, error) {
	return c.repositories, nil
}

func resourceEnvelope(t *testing.T, envelopes []facts.Envelope, resourceType string) facts.Envelope {
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

func relationshipEnvelope(t *testing.T, envelopes []facts.Envelope, relationshipType string) facts.Envelope {
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

func stringSlice(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		output := make([]string, 0, len(typed))
		for _, item := range typed {
			if s, ok := item.(string); ok {
				output = append(output, s)
			}
		}
		return output
	default:
		return nil
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
