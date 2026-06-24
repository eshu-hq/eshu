// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package guardduty

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsGuardDutyMetadataOnlyFactsAndRelationships(t *testing.T) {
	detectorID := "12abc34d567e8fa901bc2d34eexample"
	destinationARN := "arn:aws:s3:::guardduty-findings"
	threatListLocation := "arn:aws:s3:::guardduty-threat-intel/list.txt"
	ipSetLocation := "arn:aws:s3:::guardduty-ip-set/list.txt"
	client := fakeClient{detectors: []Detector{{
		ID:                         detectorID,
		Status:                     "ENABLED",
		FindingPublishingFrequency: "FIFTEEN_MINUTES",
		CreatedAt:                  "2026-05-27T12:00:00Z",
		UpdatedAt:                  "2026-05-27T12:05:00Z",
		Tags:                       map[string]string{"Environment": "prod"},
		Features: []FeatureConfiguration{{
			Name:      "S3_DATA_EVENTS",
			Status:    "ENABLED",
			UpdatedAt: 1_779_876_000,
			AdditionalConfiguration: []FeatureConfiguration{{
				Name:      "EKS_AUDIT_LOGS",
				Status:    "DISABLED",
				UpdatedAt: 1_779_876_100,
			}},
		}},
		FindingCountsBySeverity: map[string]int64{"7": 3},
		FindingCountsByType: map[string]int64{
			"UnauthorizedAccess:IAMUser/InstanceCredentialExfiltration": 2,
		},
		Members: []MemberAccount{{
			AccountID:          "111122223333",
			AdministratorID:    "123456789012",
			DetectorID:         "member-detector-id",
			RelationshipStatus: "Enabled",
			UpdatedAt:          "2026-05-27T12:10:00Z",
		}},
		Filters: []FilterSummary{{
			Name: "archive-known-benign",
		}},
		PublishingDestinations: []PublishingDestination{{
			ID:              "dest-1",
			DestinationType: "S3",
			Status:          "PUBLISHING",
			DestinationARN:  destinationARN,
			Tags:            map[string]string{"Pipeline": "security"},
		}},
		ThreatIntelSets: []ThreatIntelSet{{
			ID:          "threat-1",
			Name:        "known-threats",
			Format:      "TXT",
			Status:      "ACTIVE",
			LocationARN: threatListLocation,
			Tags:        map[string]string{"Source": "security"},
		}},
		IPSets: []IPSet{{
			ID:          "ipset-1",
			Name:        "trusted-egress",
			Format:      "TXT",
			Status:      "ACTIVE",
			LocationARN: ipSetLocation,
			Tags:        map[string]string{"Source": "network"},
		}},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	detector := resourceByType(t, envelopes, awscloud.ResourceTypeGuardDutyDetector)
	if got, want := detector.Payload["resource_id"], detectorID; got != want {
		t.Fatalf("detector resource_id = %#v, want %q", got, want)
	}
	detectorAttributes := attributesOf(t, detector)
	assertAttribute(t, detectorAttributes, "finding_publishing_frequency", "FIFTEEN_MINUTES")
	assertAttribute(t, detectorAttributes, "finding_counts_by_severity", map[string]int64{"7": 3})
	assertAttribute(t, detectorAttributes, "finding_counts_by_type", map[string]int64{
		"UnauthorizedAccess:IAMUser/InstanceCredentialExfiltration": 2,
	})
	for _, forbidden := range []string{
		"findings",
		"finding_bodies",
		"service_action",
		"resource_details",
		"access_key_details",
		"network_interfaces",
		"remote_ip_details",
		"process",
	} {
		if _, exists := detectorAttributes[forbidden]; exists {
			t.Fatalf("%s attribute persisted; GuardDuty scanner must not store finding bodies", forbidden)
		}
	}

	filter := resourceByType(t, envelopes, awscloud.ResourceTypeGuardDutyFilter)
	filterAttributes := attributesOf(t, filter)
	if got, want := filter.Payload["name"], "archive-known-benign"; got != want {
		t.Fatalf("filter name = %#v, want %q", got, want)
	}
	for _, forbidden := range []string{"finding_criteria", "criterion", "criteria", "expression"} {
		if _, exists := filterAttributes[forbidden]; exists {
			t.Fatalf("%s attribute persisted; GuardDuty scanner must not store filter criteria", forbidden)
		}
	}

	publishing := resourceByType(t, envelopes, awscloud.ResourceTypeGuardDutyPublishingDestination)
	publishingAttributes := attributesOf(t, publishing)
	assertAttribute(t, publishingAttributes, "destination_type", "S3")
	assertAttribute(t, publishingAttributes, "destination_arn", destinationARN)

	threatSet := resourceByType(t, envelopes, awscloud.ResourceTypeGuardDutyThreatIntelSet)
	threatAttributes := attributesOf(t, threatSet)
	assertAttribute(t, threatAttributes, "location_arn", threatListLocation)
	for _, forbidden := range []string{"contents", "ip_addresses", "domains", "entries"} {
		if _, exists := threatAttributes[forbidden]; exists {
			t.Fatalf("%s attribute persisted; GuardDuty scanner must not store threat intel list contents", forbidden)
		}
	}

	ipSet := resourceByType(t, envelopes, awscloud.ResourceTypeGuardDutyIPSet)
	ipAttributes := attributesOf(t, ipSet)
	assertAttribute(t, ipAttributes, "location_arn", ipSetLocation)
	for _, forbidden := range []string{"contents", "ip_addresses", "entries"} {
		if _, exists := ipAttributes[forbidden]; exists {
			t.Fatalf("%s attribute persisted; GuardDuty scanner must not store IP set contents", forbidden)
		}
	}

	assertRelationshipType(t, envelopes, awscloud.RelationshipGuardDutyDetectorHasMemberAccount)
	assertRelationshipType(t, envelopes, awscloud.RelationshipGuardDutyDetectorPublishesToDestination)
	assertRelationshipType(t, envelopes, awscloud.RelationshipGuardDutyDetectorUsesThreatIntelSet)
	assertRelationshipType(t, envelopes, awscloud.RelationshipGuardDutyDetectorUsesIPSet)
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceEventBridge

	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceGuardDuty,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:guardduty:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 27, 12, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	detectors []Detector
}

func (c fakeClient) ListDetectors(context.Context) ([]Detector, error) {
	return c.detectors, nil
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

func assertRelationshipType(t *testing.T, envelopes []facts.Envelope, relationshipType string) {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			return
		}
	}
	t.Fatalf("missing relationship_type %q in %#v", relationshipType, envelopes)
}

func valuesEqual(got any, want any) bool {
	gotMap, gotOK := got.(map[string]int64)
	wantMap, wantOK := want.(map[string]int64)
	if gotOK && wantOK {
		if len(gotMap) != len(wantMap) {
			return false
		}
		for key, gotValue := range gotMap {
			if wantMap[key] != gotValue {
				return false
			}
		}
		return true
	}
	return got == want
}
