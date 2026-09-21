// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package controltower

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	testLandingZoneARN    = "arn:aws:controltower:us-east-1:123456789012:landingzone/1A2B3C4D5E6F"
	testEnabledControlARN = "arn:aws:controltower:us-east-1:123456789012:enabledcontrol/AB12CD34"
	testEnabledBaseARN    = "arn:aws:controltower:us-east-1:123456789012:enabledbaseline/EB12CD34"
	testControlID         = "arn:aws:controltower:us-east-1::control/AWS-GR_ENCRYPTED_VOLUMES"
	testBaselineID        = "arn:aws:controltower:us-east-1::baseline/17BSJV3IGJ2QSGA2"
	testOUARN             = "arn:aws:organizations::123456789012:ou/o-exampleorgid/ou-root-platform"
	testOUBareID          = "ou-root-platform"
	testAccountARN        = "arn:aws:organizations::123456789012:account/o-exampleorgid/111122223333"
	testAccountBareID     = "111122223333"
	testRootARN           = "arn:aws:organizations::123456789012:root/o-exampleorgid/r-root"
	testRootBareID        = "r-root"
)

func TestScannerEmitsControlTowerMetadataAndRelationships(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		LandingZone: &LandingZone{
			ARN:                    testLandingZoneARN,
			Version:                "3.3",
			LatestAvailableVersion: "3.3",
			Status:                 "ACTIVE",
			DriftStatus:            "IN_SYNC",
			Tags:                   map[string]string{"Environment": "prod"},
		},
		EnabledControls: []EnabledControl{{
			ARN:               testEnabledControlARN,
			ControlIdentifier: testControlID,
			TargetIdentifier:  testOUARN,
			Status:            "SUCCEEDED",
			DriftStatus:       "IN_SYNC",
		}},
		EnabledBaselines: []EnabledBaseline{{
			ARN:                testEnabledBaseARN,
			BaselineIdentifier: testBaselineID,
			BaselineVersion:    "4.0",
			TargetIdentifier:   testOUARN,
			Status:             "SUCCEEDED",
		}},
	}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	// Landing zone resource node, keyed by its ARN.
	lz := resourceByType(t, envelopes, awscloud.ResourceTypeControlTowerLandingZone)
	if got, want := lz.Payload["resource_id"], testLandingZoneARN; got != want {
		t.Fatalf("landing zone resource_id = %#v, want %q", got, want)
	}
	if got, want := lz.Payload["state"], "ACTIVE"; got != want {
		t.Fatalf("landing zone state = %#v, want %q", got, want)
	}
	lzAttrs := attributesOf(t, lz)
	assertAttribute(t, lzAttrs, "version", "3.3")
	assertAttribute(t, lzAttrs, "drift_status", "IN_SYNC")

	// Enabled control resource node, keyed by its ARN.
	control := resourceByType(t, envelopes, awscloud.ResourceTypeControlTowerEnabledControl)
	if got, want := control.Payload["resource_id"], testEnabledControlARN; got != want {
		t.Fatalf("control resource_id = %#v, want %q", got, want)
	}
	controlAttrs := attributesOf(t, control)
	assertAttribute(t, controlAttrs, "control_identifier", testControlID)
	assertAttribute(t, controlAttrs, "target_identifier", testOUARN)

	// Enabled baseline resource node, keyed by its ARN.
	baseline := resourceByType(t, envelopes, awscloud.ResourceTypeControlTowerEnabledBaseline)
	if got, want := baseline.Payload["resource_id"], testEnabledBaseARN; got != want {
		t.Fatalf("baseline resource_id = %#v, want %q", got, want)
	}
	baselineAttrs := attributesOf(t, baseline)
	assertAttribute(t, baselineAttrs, "baseline_identifier", testBaselineID)
	assertAttribute(t, baselineAttrs, "baseline_version", "4.0")

	// control -> Organizations OU edge, keyed by the bare ou-… id the
	// organizations scanner publishes, NOT the ARN.
	controlEdge := relationshipByType(t, envelopes, awscloud.RelationshipControlTowerControlGovernsTarget)
	assertEdgeTarget(t, controlEdge, awscloud.ResourceTypeOrganizationsOrganizationalUnit, testOUBareID)
	if got, want := controlEdge.Payload["source_resource_id"], testEnabledControlARN; got != want {
		t.Fatalf("control->ou source_resource_id = %#v, want %q", got, want)
	}
	// The OU is keyed by bare id, so target_arn must be blank (the edge is not
	// ARN-keyed). The original target ARN is preserved as an edge attribute.
	if got := controlEdge.Payload["target_arn"]; got != "" {
		t.Fatalf("control->ou target_arn = %#v, want empty (bare-id-keyed target)", got)
	}
	controlEdgeAttrs := attributesOf(t, controlEdge)
	assertAttribute(t, controlEdgeAttrs, "target_arn", testOUARN)

	// baseline -> Organizations OU edge, keyed by the bare ou-… id.
	baselineEdge := relationshipByType(t, envelopes, awscloud.RelationshipControlTowerBaselineGovernsTarget)
	assertEdgeTarget(t, baselineEdge, awscloud.ResourceTypeOrganizationsOrganizationalUnit, testOUBareID)
	if got, want := baselineEdge.Payload["source_resource_id"], testEnabledBaseARN; got != want {
		t.Fatalf("baseline->ou source_resource_id = %#v, want %q", got, want)
	}

	// baseline -> landing zone internal edge, keyed by the landing-zone ARN.
	lzEdge := relationshipByType(t, envelopes, awscloud.RelationshipControlTowerBaselineForLandingZone)
	assertEdgeTarget(t, lzEdge, awscloud.ResourceTypeControlTowerLandingZone, testLandingZoneARN)
	if got, want := lzEdge.Payload["source_resource_id"], testEnabledBaseARN; got != want {
		t.Fatalf("baseline->landing-zone source_resource_id = %#v, want %q", got, want)
	}

	assertNoGovernancePayload(t, envelopes)
}

func TestScannerResolvesAccountAndRootTargets(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		EnabledBaselines: []EnabledBaseline{
			{ARN: "arn:aws:controltower:us-east-1:123456789012:enabledbaseline/ACC", BaselineIdentifier: testBaselineID, TargetIdentifier: testAccountARN},
			{ARN: "arn:aws:controltower:us-east-1:123456789012:enabledbaseline/ROOT", BaselineIdentifier: testBaselineID, TargetIdentifier: testRootARN},
		},
	}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	var sawAccount, sawRoot bool
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if envelope.Payload["relationship_type"] != awscloud.RelationshipControlTowerBaselineGovernsTarget {
			continue
		}
		switch envelope.Payload["target_resource_id"] {
		case testAccountBareID:
			sawAccount = true
			if got := envelope.Payload["target_type"]; got != awscloud.ResourceTypeOrganizationsAccount {
				t.Fatalf("account target_type = %#v, want %q", got, awscloud.ResourceTypeOrganizationsAccount)
			}
		case testRootBareID:
			sawRoot = true
			if got := envelope.Payload["target_type"]; got != awscloud.ResourceTypeOrganizationsRoot {
				t.Fatalf("root target_type = %#v, want %q", got, awscloud.ResourceTypeOrganizationsRoot)
			}
		}
	}
	if !sawAccount {
		t.Fatalf("missing baseline->account edge keyed by bare account id %q", testAccountBareID)
	}
	if !sawRoot {
		t.Fatalf("missing baseline->root edge keyed by bare root id %q", testRootBareID)
	}
}

func TestScannerResolvesGovCloudAndChinaTargets(t *testing.T) {
	govOUARN := "arn:aws-us-gov:organizations::123456789012:ou/o-gov/ou-gov-team"
	cnOUARN := "arn:aws-cn:organizations::123456789012:ou/o-cn/ou-cn-team"
	client := fakeClient{snapshot: Snapshot{
		EnabledControls: []EnabledControl{
			{ARN: "arn:aws-us-gov:controltower:us-gov-west-1:123456789012:enabledcontrol/GOV", ControlIdentifier: testControlID, TargetIdentifier: govOUARN},
			{ARN: "arn:aws-cn:controltower:cn-north-1:123456789012:enabledcontrol/CN", ControlIdentifier: testControlID, TargetIdentifier: cnOUARN},
		},
	}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	wantTargets := map[string]bool{"ou-gov-team": false, "ou-cn-team": false}
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if target, ok := envelope.Payload["target_resource_id"].(string); ok {
			if _, want := wantTargets[target]; want {
				wantTargets[target] = true
				if got := envelope.Payload["target_type"]; got != awscloud.ResourceTypeOrganizationsOrganizationalUnit {
					t.Fatalf("target %q type = %#v, want OU", target, got)
				}
			}
		}
	}
	for target, seen := range wantTargets {
		if !seen {
			t.Fatalf("missing GovCloud/China OU edge keyed by bare id %q", target)
		}
	}
}

func TestScannerSkipsEdgeForUnresolvableTarget(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		EnabledControls: []EnabledControl{{
			ARN:               testEnabledControlARN,
			ControlIdentifier: testControlID,
			// A non-Organizations target ARN: the OU edge must be skipped, not
			// dangled. The control resource node is still emitted.
			TargetIdentifier: "arn:aws:controltower:us-east-1:123456789012:notarealtarget/xyz",
		}},
	}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if _, ok := findResource(envelopes, awscloud.ResourceTypeControlTowerEnabledControl); !ok {
		t.Fatalf("control resource node missing; only the edge should be skipped")
	}
	for _, envelope := range envelopes {
		if envelope.FactKind == facts.AWSRelationshipFactKind {
			t.Fatalf("unexpected relationship for unresolvable target: %#v", envelope.Payload)
		}
	}
}

func TestScannerOmitsLandingZoneEdgeWhenNoLandingZone(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		EnabledBaselines: []EnabledBaseline{{
			ARN:                testEnabledBaseARN,
			BaselineIdentifier: testBaselineID,
			TargetIdentifier:   testOUARN,
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
		if envelope.Payload["relationship_type"] == awscloud.RelationshipControlTowerBaselineForLandingZone {
			t.Fatalf("baseline->landing-zone edge emitted with no landing zone present")
		}
	}
}

func TestScannerHandlesEmptyAccount(t *testing.T) {
	envelopes, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if len(envelopes) != 0 {
		t.Fatalf("Scan() on empty account returned %d envelopes, want 0", len(envelopes))
	}
}

func TestScannerRelationshipsSatisfyGraphJoinGuard(t *testing.T) {
	boundary := testBoundary()
	control := EnabledControl{ARN: testEnabledControlARN, ControlIdentifier: testControlID, TargetIdentifier: testOUARN}
	baseline := EnabledBaseline{ARN: testEnabledBaseARN, BaselineIdentifier: testBaselineID, TargetIdentifier: testAccountARN}
	var observations []awscloud.RelationshipObservation
	for _, rel := range []*awscloud.RelationshipObservation{
		controlGovernsTargetRelationship(boundary, control),
		baselineGovernsTargetRelationship(boundary, baseline),
		baselineForLandingZoneRelationship(boundary, baseline, testLandingZoneARN),
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

func TestScannerEmitsThrottleWarningFacts(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		Warnings: []awscloud.WarningObservation{{
			WarningKind:    awscloud.WarningThrottleSustained,
			ErrorClass:     "throttled",
			Message:        "Control Tower ListEnabledControls throttled after SDK retries; enabled-control metadata omitted for this scan",
			SourceRecordID: "controltower_enabled_controls_throttled",
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

func assertNoGovernancePayload(t *testing.T, envelopes []facts.Envelope) {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		attrs, _ := envelope.Payload["attributes"].(map[string]any)
		for _, forbidden := range []string{
			"manifest", "manifest_json", "parameters", "parameter_values",
			"control_parameters", "baseline_parameters", "governance",
		} {
			if _, exists := attrs[forbidden]; exists {
				t.Fatalf("%s attribute persisted; Control Tower scanner must stay metadata-only", forbidden)
			}
		}
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceControlTower,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:controltower:1",
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
	envelope, ok := findResource(envelopes, resourceType)
	if !ok {
		t.Fatalf("missing resource_type %q in %#v", resourceType, envelopes)
	}
	return envelope
}

func findResource(envelopes []facts.Envelope, resourceType string) (facts.Envelope, bool) {
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if got, _ := envelope.Payload["resource_type"].(string); got == resourceType {
			return envelope, true
		}
	}
	return facts.Envelope{}, false
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
