// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package appflow

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsAppFlowMetadataResourcesAndRelationships(t *testing.T) {
	flowARN := "arn:aws:appflow:us-east-1:123456789012:flow/orders-sync"
	profileName := "salesforce-prod"
	secretARN := "arn:aws:secretsmanager:us-east-1:123456789012:secret:appflow!sf-Ab3xZq"
	kmsARN := "arn:aws:kms:us-east-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
	client := fakeClient{
		flows: []Flow{{
			ARN:                             flowARN,
			Name:                            "orders-sync",
			Description:                     "sync orders from S3 to Salesforce",
			Status:                          "Active",
			SourceConnectorType:             "S3",
			DestinationConnectorType:        "Salesforce",
			SourceS3Bucket:                  "orders-landing",
			DestinationConnectorProfileName: profileName,
			Destinations: []FlowDestination{
				{ConnectorType: "Salesforce", ConnectorProfileName: profileName},
			},
			KMSKeyARN:     kmsARN,
			TriggerType:   "Scheduled",
			CreatedAt:     time.Date(2026, 5, 20, 16, 0, 0, 0, time.UTC),
			LastUpdatedAt: time.Date(2026, 5, 21, 16, 0, 0, 0, time.UTC),
		}},
		profiles: []ConnectorProfile{{
			ARN:            "arn:aws:appflow:us-east-1:123456789012:connectorprofile/salesforce-prod",
			Name:           profileName,
			ConnectorType:  "Salesforce",
			ConnectionMode: "Public",
			CredentialsARN: secretARN,
			CreatedAt:      time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC),
			LastUpdatedAt:  time.Date(2026, 5, 21, 8, 0, 0, 0, time.UTC),
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	flow := resourceByType(t, envelopes, awscloud.ResourceTypeAppFlowFlow)
	if got, want := flow.Payload["resource_id"], flowARN; got != want {
		t.Fatalf("flow resource_id = %#v, want %q", got, want)
	}
	if got, want := flow.Payload["name"], "orders-sync"; got != want {
		t.Fatalf("flow name = %#v, want %q", got, want)
	}
	flowAttrs := attributesOf(t, flow)
	if got, want := flowAttrs["source_connector_type"], "S3"; got != want {
		t.Fatalf("flow source_connector_type = %#v, want %q", got, want)
	}
	if got, want := flowAttrs["trigger_type"], "Scheduled"; got != want {
		t.Fatalf("flow trigger_type = %#v, want %q", got, want)
	}
	// HARD CONTRACT: field mappings (task transforms) and flow run records must
	// never be persisted; they can carry literal transferred data values.
	for _, forbidden := range []string{
		"tasks", "field_mappings", "task_transforms", "mappings",
		"records", "run_records", "execution_records",
	} {
		if _, exists := flowAttrs[forbidden]; exists {
			t.Fatalf("flow %s attribute persisted; field mappings and run records must never be stored", forbidden)
		}
	}

	profile := resourceByType(t, envelopes, awscloud.ResourceTypeAppFlowConnectorProfile)
	if got, want := profile.Payload["resource_id"], profileName; got != want {
		t.Fatalf("profile resource_id = %#v, want %q", got, want)
	}
	profileAttrs := attributesOf(t, profile)
	if got, want := profileAttrs["connector_type"], "Salesforce"; got != want {
		t.Fatalf("profile connector_type = %#v, want %q", got, want)
	}
	if got, want := profileAttrs["connection_mode"], "Public"; got != want {
		t.Fatalf("profile connection_mode = %#v, want %q", got, want)
	}
	// HARD CONTRACT: connector credentials and OAuth tokens must never be
	// persisted. Only the Secrets Manager credentials ARN reference is allowed.
	for _, forbidden := range []string{
		"credentials", "credentials_arn", "access_token", "refresh_token",
		"oauth_token", "client_secret", "api_key", "password", "secret_value",
	} {
		if _, exists := profileAttrs[forbidden]; exists {
			t.Fatalf("profile %s attribute persisted; connector credentials and tokens must never be stored", forbidden)
		}
	}

	flowS3 := relationshipByType(t, envelopes, awscloud.RelationshipAppFlowFlowReadsFromS3Bucket)
	if got, want := flowS3.Payload["source_resource_id"], flowARN; got != want {
		t.Fatalf("flow->s3 source_resource_id = %#v, want %q (must match flow node resource_id)", got, want)
	}
	if got, want := flowS3.Payload["target_resource_id"], "arn:aws:s3:::orders-landing"; got != want {
		t.Fatalf("flow->s3 target_resource_id = %#v, want %q", got, want)
	}
	if got, want := flowS3.Payload["target_arn"], "arn:aws:s3:::orders-landing"; got != want {
		t.Fatalf("flow->s3 target_arn = %#v, want %q", got, want)
	}
	if got, want := flowS3.Payload["target_type"], awscloud.ResourceTypeS3Bucket; got != want {
		t.Fatalf("flow->s3 target_type = %#v, want %q", got, want)
	}

	flowProfile := relationshipByType(t, envelopes, awscloud.RelationshipAppFlowFlowUsesConnectorProfile)
	if got, want := flowProfile.Payload["source_resource_id"], flowARN; got != want {
		t.Fatalf("flow->profile source_resource_id = %#v, want %q", got, want)
	}
	if got, want := flowProfile.Payload["target_resource_id"], profileName; got != want {
		t.Fatalf("flow->profile target_resource_id = %#v, want %q (must match profile node resource_id)", got, want)
	}
	if got, want := flowProfile.Payload["target_type"], awscloud.ResourceTypeAppFlowConnectorProfile; got != want {
		t.Fatalf("flow->profile target_type = %#v, want %q", got, want)
	}

	flowKMS := relationshipByType(t, envelopes, awscloud.RelationshipAppFlowFlowUsesKMSKey)
	if got, want := flowKMS.Payload["target_resource_id"], kmsARN; got != want {
		t.Fatalf("flow->kms target_resource_id = %#v, want %q", got, want)
	}
	if got, want := flowKMS.Payload["target_arn"], kmsARN; got != want {
		t.Fatalf("flow->kms target_arn = %#v, want %q", got, want)
	}
	if got, want := flowKMS.Payload["target_type"], awscloud.ResourceTypeKMSKey; got != want {
		t.Fatalf("flow->kms target_type = %#v, want %q", got, want)
	}

	profileSecret := relationshipByType(t, envelopes, awscloud.RelationshipAppFlowConnectorProfileUsesSecret)
	if got, want := profileSecret.Payload["source_resource_id"], profileName; got != want {
		t.Fatalf("profile->secret source_resource_id = %#v, want %q", got, want)
	}
	if got, want := profileSecret.Payload["target_resource_id"], secretARN; got != want {
		t.Fatalf("profile->secret target_resource_id = %#v, want %q", got, want)
	}
	if got, want := profileSecret.Payload["target_arn"], secretARN; got != want {
		t.Fatalf("profile->secret target_arn = %#v, want %q", got, want)
	}
	if got, want := profileSecret.Payload["target_type"], awscloud.ResourceTypeSecretsManagerSecret; got != want {
		t.Fatalf("profile->secret target_type = %#v, want %q", got, want)
	}

	relguard.AssertObservations(t, allRelationshipObservations(testBoundary(), client.flows, client.profiles)...)
}

func TestScannerEmitsDestinationS3Relationship(t *testing.T) {
	client := fakeClient{flows: []Flow{{
		ARN:                      "arn:aws:appflow:us-east-1:123456789012:flow/export",
		Name:                     "export",
		SourceConnectorType:      "Salesforce",
		DestinationConnectorType: "S3",
		DestinationS3Bucket:      "exports-bucket",
		Destinations: []FlowDestination{
			{ConnectorType: "S3", S3Bucket: "exports-bucket"},
		},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	rel := relationshipByType(t, envelopes, awscloud.RelationshipAppFlowFlowWritesToS3Bucket)
	if got, want := rel.Payload["target_resource_id"], "arn:aws:s3:::exports-bucket"; got != want {
		t.Fatalf("flow->s3 destination target_resource_id = %#v, want %q", got, want)
	}
	if got := countRelationships(envelopes, awscloud.RelationshipAppFlowFlowReadsFromS3Bucket); got != 0 {
		t.Fatalf("flow->s3 read relationship count = %d, want 0 for non-S3 source", got)
	}
}

// TestScannerEmitsEdgePerDestination pins that a fan-out flow with multiple
// destinations emits one S3 write edge per S3 destination bucket and one
// connector-profile edge per distinct destination profile, rather than collapsing
// to a single destination. A flattened scanner would silently drop the second
// bucket and the profile edge, producing incomplete graph evidence.
func TestScannerEmitsEdgePerDestination(t *testing.T) {
	flowARN := "arn:aws:appflow:us-east-1:123456789012:flow/fanout"
	client := fakeClient{flows: []Flow{{
		ARN:                 flowARN,
		Name:                "fanout",
		SourceConnectorType: "Salesforce",
		Destinations: []FlowDestination{
			{ConnectorType: "S3", S3Bucket: "primary-out"},
			{ConnectorType: "S3", S3Bucket: "secondary-out"},
			{ConnectorType: "Salesforce", ConnectorProfileName: "salesforce-prod"},
		},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	if got := countRelationships(envelopes, awscloud.RelationshipAppFlowFlowWritesToS3Bucket); got != 2 {
		t.Fatalf("flow->s3 write relationship count = %d, want 2 (one per S3 destination)", got)
	}
	wantBuckets := map[string]bool{
		"arn:aws:s3:::primary-out":   false,
		"arn:aws:s3:::secondary-out": false,
	}
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got != awscloud.RelationshipAppFlowFlowWritesToS3Bucket {
			continue
		}
		if got, _ := envelope.Payload["source_resource_id"].(string); got != flowARN {
			t.Fatalf("flow->s3 source_resource_id = %q, want %q (flow node id)", got, flowARN)
		}
		target, _ := envelope.Payload["target_resource_id"].(string)
		if _, ok := wantBuckets[target]; !ok {
			t.Fatalf("unexpected flow->s3 target_resource_id %q", target)
		}
		wantBuckets[target] = true
	}
	for bucket, seen := range wantBuckets {
		if !seen {
			t.Fatalf("missing flow->s3 destination edge for %q", bucket)
		}
	}

	if got := countRelationships(envelopes, awscloud.RelationshipAppFlowFlowUsesConnectorProfile); got != 1 {
		t.Fatalf("flow->profile relationship count = %d, want 1 (destination profile)", got)
	}
	profile := relationshipByType(t, envelopes, awscloud.RelationshipAppFlowFlowUsesConnectorProfile)
	if got, want := profile.Payload["target_resource_id"], "salesforce-prod"; got != want {
		t.Fatalf("flow->profile target_resource_id = %#v, want %q", got, want)
	}

	relguard.AssertObservations(t, allRelationshipObservations(testBoundary(), client.flows, client.profiles)...)
}

func TestScannerOmitsKMSRelationshipForManagedKey(t *testing.T) {
	client := fakeClient{flows: []Flow{{
		ARN:       "arn:aws:appflow:us-east-1:123456789012:flow/managed",
		Name:      "managed",
		KMSKeyARN: "",
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if got := countRelationships(envelopes, awscloud.RelationshipAppFlowFlowUsesKMSKey); got != 0 {
		t.Fatalf("flow->kms relationship count = %d, want 0 for AppFlow-managed key", got)
	}
}

func TestScannerOmitsSecretRelationshipForNonSecretsManagerARN(t *testing.T) {
	// A connector profile whose credentials ARN is not a Secrets Manager ARN
	// (e.g. an IAM ARN that incidentally contains the substring) must not emit a
	// dangling profile->secret edge.
	client := fakeClient{profiles: []ConnectorProfile{{
		Name:           "no-secret",
		CredentialsARN: "arn:aws:iam::123456789012:role/secretsmanager-access",
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if got := countRelationships(envelopes, awscloud.RelationshipAppFlowConnectorProfileUsesSecret); got != 0 {
		t.Fatalf("profile->secret relationship count = %d, want 0 for non-secretsmanager ARN", got)
	}
}

func TestScannerCollapsesSameProfileSourceAndDestination(t *testing.T) {
	client := fakeClient{flows: []Flow{{
		ARN:                             "arn:aws:appflow:us-east-1:123456789012:flow/loop",
		Name:                            "loop",
		SourceConnectorProfileName:      "shared-profile",
		DestinationConnectorProfileName: "shared-profile",
		Destinations: []FlowDestination{
			{ConnectorType: "Salesforce", ConnectorProfileName: "shared-profile"},
		},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if got := countRelationships(envelopes, awscloud.RelationshipAppFlowFlowUsesConnectorProfile); got != 1 {
		t.Fatalf("flow->profile relationship count = %d, want 1 (source==destination collapses)", got)
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceKMS

	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func TestScannerRequiresClient(t *testing.T) {
	_, err := (Scanner{}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want client required")
	}
}

// TestClientInterfaceExcludesDataAndCredentialAPIs reflects over the scanner
// Client contract and fails if it exposes any record/data-pull, run, mutation,
// or credential-read surface. AppFlow is metadata-only: the contract must not be
// able to start flows, read flow run records, read field mappings, or read
// connector credentials beyond the Secrets Manager ARN reference.
func TestClientInterfaceExcludesDataAndCredentialAPIs(t *testing.T) {
	forbidden := []string{
		"StartFlow", "StopFlow", "RunFlow", "DescribeFlowExecutionRecords",
		"ListFlowExecution", "GetFlowExecution",
		"CreateFlow", "UpdateFlow", "DeleteFlow",
		"CreateConnectorProfile", "UpdateConnectorProfile", "DeleteConnectorProfile",
		"Credentials", "Credential", "Token", "Secret", "Password",
		"Record", "Mapping", "Task", "Field",
	}
	clientType := reflect.TypeOf((*Client)(nil)).Elem()
	for i := 0; i < clientType.NumMethod(); i++ {
		method := clientType.Method(i)
		for _, banned := range forbidden {
			if strings.Contains(method.Name, banned) {
				t.Fatalf("Client interface method %q resembles forbidden operation %q; AppFlow scanner contract is metadata-only", method.Name, banned)
			}
		}
	}
}

func allRelationshipObservations(
	boundary awscloud.Boundary,
	flows []Flow,
	profiles []ConnectorProfile,
) []awscloud.RelationshipObservation {
	var observations []awscloud.RelationshipObservation
	add := func(obs *awscloud.RelationshipObservation) {
		if obs != nil {
			observations = append(observations, *obs)
		}
	}
	for _, flow := range flows {
		add(flowS3SourceRelationship(boundary, flow))
		observations = append(observations, flowS3DestinationRelationships(boundary, flow)...)
		add(flowKMSKeyRelationship(boundary, flow))
		observations = append(observations, flowConnectorProfileRelationships(boundary, flow)...)
	}
	for _, profile := range profiles {
		add(connectorProfileSecretRelationship(boundary, profile))
	}
	return observations
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceAppFlow,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:appflow:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 30, 12, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	flows    []Flow
	profiles []ConnectorProfile
}

func (c fakeClient) ListFlows(context.Context) ([]Flow, error) { return c.flows, nil }
func (c fakeClient) ListConnectorProfiles(context.Context) ([]ConnectorProfile, error) {
	return c.profiles, nil
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
	t.Fatalf("missing resource_type %q in %d envelopes", resourceType, len(envelopes))
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
	t.Fatalf("missing relationship_type %q in %d envelopes", relationshipType, len(envelopes))
	return facts.Envelope{}
}

func countRelationships(envelopes []facts.Envelope, relationshipType string) int {
	var count int
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

func attributesOf(t *testing.T, envelope facts.Envelope) map[string]any {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	return attributes
}
