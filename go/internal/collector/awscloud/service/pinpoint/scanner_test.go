// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package pinpoint

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	testAppID       = "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4"
	testAppARN      = "arn:aws:mobiletargeting:us-east-1:123456789012:apps/" + testAppID
	testSegmentID   = "s1234567890abcdef1234567890abcdef"
	testSegmentARN  = "arn:aws:mobiletargeting:us-east-1:123456789012:apps/" + testAppID + "/segments/" + testSegmentID
	testSESIdentity = "arn:aws:ses:us-east-1:123456789012:identity/example.com"
)

func TestScannerEmitsPinpointMetadataAndRelationships(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Applications: []Application{{
		ID:           testAppID,
		ARN:          testAppARN,
		Name:         "marketing",
		CreationTime: time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
		Tags:         map[string]string{"Environment": "prod"},
		Segments: []Segment{{
			ID:               testSegmentID,
			ARN:              testSegmentARN,
			Name:             "active-users",
			ApplicationID:    testAppID,
			SegmentType:      "IMPORT",
			Version:          3,
			ImportedFromS3:   true,
			ImportFormat:     "CSV",
			ImportSize:       4200,
			CreationTime:     time.Date(2026, 5, 14, 12, 5, 0, 0, time.UTC),
			LastModifiedTime: time.Date(2026, 5, 20, 9, 30, 0, 0, time.UTC),
			Tags:             map[string]string{"Team": "growth"},
		}},
		Channels: []Channel{{
			ApplicationID:       testAppID,
			ChannelType:         "EMAIL",
			Enabled:             true,
			Version:             2,
			SESConfigurationSet: "marketing-config-set",
			SESIdentityARN:      testSESIdentity,
		}},
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	// Application resource node, keyed by the application id.
	application := resourceByType(t, envelopes, awscloud.ResourceTypePinpointApplication)
	if got, want := application.Payload["resource_id"], testAppID; got != want {
		t.Fatalf("application resource_id = %#v, want %q", got, want)
	}
	if got, want := application.Payload["arn"], testAppARN; got != want {
		t.Fatalf("application arn = %#v, want %q", got, want)
	}
	appAttrs := attributesOf(t, application)
	assertAttribute(t, appAttrs, "application_id", testAppID)

	// Segment resource node.
	segment := resourceByType(t, envelopes, awscloud.ResourceTypePinpointSegment)
	if got, want := segment.Payload["resource_id"], testSegmentARN; got != want {
		t.Fatalf("segment resource_id = %#v, want %q", got, want)
	}
	segAttrs := attributesOf(t, segment)
	assertAttribute(t, segAttrs, "segment_type", "IMPORT")
	assertAttribute(t, segAttrs, "imported_from_s3", true)
	assertAttribute(t, segAttrs, "import_format", "CSV")
	assertAttribute(t, segAttrs, "import_size", int32(4200))

	// Channel resource node, keyed by app-id/channel-type.
	channel := resourceByType(t, envelopes, awscloud.ResourceTypePinpointChannel)
	if got, want := channel.Payload["resource_id"], testAppID+"/EMAIL"; got != want {
		t.Fatalf("channel resource_id = %#v, want %q", got, want)
	}
	if got, want := channel.Payload["state"], "ENABLED"; got != want {
		t.Fatalf("channel state = %#v, want %q", got, want)
	}
	chAttrs := attributesOf(t, channel)
	assertAttribute(t, chAttrs, "enabled", true)
	assertAttribute(t, chAttrs, "ses_configuration_set", "marketing-config-set")
	// The SES identity attribute carries the bare identity NAME, never the ARN
	// and never the from-address.
	assertAttribute(t, chAttrs, "ses_identity", "example.com")

	// application -> segment edge, keyed by the application id the app node publishes.
	appSegment := relationshipByType(t, envelopes, awscloud.RelationshipPinpointApplicationHasSegment)
	assertEdgeTarget(t, appSegment, awscloud.ResourceTypePinpointSegment, testSegmentARN)
	if got, want := appSegment.Payload["source_resource_id"], testAppID; got != want {
		t.Fatalf("app->segment source_resource_id = %#v, want %q", got, want)
	}

	// channel -> application edge.
	channelApp := relationshipByType(t, envelopes, awscloud.RelationshipPinpointChannelInApplication)
	assertEdgeTarget(t, channelApp, awscloud.ResourceTypePinpointApplication, testAppID)

	// email channel -> SES identity edge, keyed by the bare identity name the SES
	// email-identity node publishes. target_arn must NOT be set (the SES node is
	// name-keyed, not ARN-keyed); the reported identity ARN is kept as evidence
	// in the edge attributes instead.
	channelIdentity := relationshipByType(t, envelopes, awscloud.RelationshipPinpointEmailChannelUsesSESIdentity)
	assertEdgeTarget(t, channelIdentity, awscloud.ResourceTypeSESEmailIdentity, "example.com")
	if got := channelIdentity.Payload["target_arn"]; got != "" {
		t.Fatalf("channel->identity target_arn = %#v, want empty (name-keyed target)", got)
	}
	identityEdgeAttrs := attributesOf(t, channelIdentity)
	assertAttribute(t, identityEdgeAttrs, "ses_identity_arn", testSESIdentity)

	// email channel -> SES configuration set edge, keyed by the config set name.
	channelConfig := relationshipByType(t, envelopes, awscloud.RelationshipPinpointEmailChannelUsesSESConfigurationSet)
	assertEdgeTarget(t, channelConfig, awscloud.ResourceTypeSESConfigurationSet, "marketing-config-set")

	// No endpoint / address / message / targeting leakage anywhere in the payloads.
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		attrs, _ := envelope.Payload["attributes"].(map[string]any)
		for _, forbidden := range []string{
			"endpoints", "endpoint_records", "addresses", "from_address",
			"dimensions", "segment_groups", "criteria", "message", "message_body",
			"template", "import_s3_url", "s3_url", "external_id", "phone_numbers",
		} {
			if _, exists := attrs[forbidden]; exists {
				t.Fatalf("%s attribute persisted; Pinpoint scanner must stay metadata-only and PII-free", forbidden)
			}
		}
	}
}

func TestScannerSkipsSESIdentityEdgeForNonIdentityARN(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Applications: []Application{{
		ID:   testAppID,
		ARN:  testAppARN,
		Name: "marketing",
		Channels: []Channel{{
			ApplicationID: testAppID,
			ChannelType:   "EMAIL",
			Enabled:       true,
			// A non-identity ARN must not key a dangling SES-identity edge.
			SESIdentityARN: "arn:aws:ses:us-east-1:123456789012:configuration-set/foo",
		}},
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == awscloud.RelationshipPinpointEmailChannelUsesSESIdentity {
			t.Fatalf("unexpected SES-identity edge for non-identity ARN: %#v", envelope.Payload)
		}
	}
}

func TestScannerOmitsEmailEdgesWhenChannelHasNoSESReferences(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{Applications: []Application{{
		ID:   testAppID,
		ARN:  testAppARN,
		Name: "marketing",
		Channels: []Channel{{
			ApplicationID: testAppID,
			ChannelType:   "SMS",
			Enabled:       false,
		}},
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		relType, _ := envelope.Payload["relationship_type"].(string)
		switch relType {
		case awscloud.RelationshipPinpointEmailChannelUsesSESIdentity,
			awscloud.RelationshipPinpointEmailChannelUsesSESConfigurationSet:
			t.Fatalf("unexpected email edge for SMS channel: %#v", envelope.Payload)
		}
	}
	channel := resourceByType(t, envelopes, awscloud.ResourceTypePinpointChannel)
	if got, want := channel.Payload["state"], "DISABLED"; got != want {
		t.Fatalf("disabled channel state = %#v, want %q", got, want)
	}
}

func TestScannerRelationshipsSatisfyGraphJoinGuard(t *testing.T) {
	boundary := testBoundary()
	application := Application{ID: testAppID, ARN: testAppARN, Name: "marketing"}
	applicationID := applicationResourceID(application)
	segment := Segment{ID: testSegmentID, ARN: testSegmentARN, Name: "active-users", ApplicationID: testAppID}
	channel := Channel{
		ApplicationID:       testAppID,
		ChannelType:         "EMAIL",
		Enabled:             true,
		SESConfigurationSet: "marketing-config-set",
		SESIdentityARN:      testSESIdentity,
	}
	var observations []awscloud.RelationshipObservation
	for _, rel := range []*awscloud.RelationshipObservation{
		applicationHasSegmentRelationship(boundary, applicationID, segment),
		channelInApplicationRelationship(boundary, applicationID, channel),
		emailChannelSESIdentityRelationship(boundary, channel),
		emailChannelSESConfigurationSetRelationship(boundary, channel),
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

func TestScannerReturnsCleanlyForEmptyAccount(t *testing.T) {
	envelopes, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if len(envelopes) != 0 {
		t.Fatalf("Scan() returned %d envelopes for empty account, want 0", len(envelopes))
	}
}

func TestScannerEmitsThrottleWarningFacts(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		Applications: []Application{{ID: testAppID, ARN: testAppARN, Name: "marketing"}},
		Warnings: []awscloud.WarningObservation{{
			Boundary:       testBoundary(),
			WarningKind:    awscloud.WarningThrottleSustained,
			ErrorClass:     "throttled",
			Message:        "Pinpoint GetSegments throttled after SDK retries; segment metadata omitted for this scan",
			SourceRecordID: "pinpoint_segments_throttled",
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
		ServiceKind:         awscloud.ServicePinpoint,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:pinpoint:1",
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
