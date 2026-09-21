// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package appstream

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	testFleetARN        = "arn:aws:appstream:us-east-1:123456789012:fleet/sales-fleet"
	testStackARN        = "arn:aws:appstream:us-east-1:123456789012:stack/sales-stack"
	testImageBuilderARN = "arn:aws:appstream:us-east-1:123456789012:image-builder/builder-1"
	testImageARN        = "arn:aws:appstream:us-east-1:123456789012:image/custom-image"
	testRoleARN         = "arn:aws:iam::123456789012:role/appstream-machine-role"
)

func fullSnapshot() Snapshot {
	return Snapshot{
		Fleets: []Fleet{{
			ARN:                         testFleetARN,
			Name:                        "sales-fleet",
			DisplayName:                 "Sales Fleet",
			State:                       "RUNNING",
			FleetType:                   "ON_DEMAND",
			InstanceType:                "stream.standard.medium",
			Platform:                    "WINDOWS_SERVER_2019",
			StreamView:                  "APP",
			IAMRoleARN:                  testRoleARN,
			ImageARN:                    testImageARN,
			ImageName:                   "custom-image",
			EnableDefaultInternetAccess: true,
			MaxConcurrentSessions:       10,
			MaxUserDurationInSeconds:    3600,
			CreatedTime:                 time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
			SubnetIDs:                   []string{"subnet-aaa", "subnet-bbb"},
			SecurityGroupIDs:            []string{"sg-111"},
			Tags:                        map[string]string{"Environment": "prod"},
		}},
		Stacks: []Stack{{
			ARN:                         testStackARN,
			Name:                        "sales-stack",
			DisplayName:                 "Sales Stack",
			ApplicationSettingsEnabled:  true,
			ApplicationSettingsS3Bucket: "appstream-settings-bucket",
			StorageConnectorBuckets:     []string{"appstream-home-folders"},
			CreatedTime:                 time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
			Tags:                        map[string]string{"Team": "sales"},
		}},
		ImageBuilders: []ImageBuilder{{
			ARN:              testImageBuilderARN,
			Name:             "builder-1",
			State:            "RUNNING",
			InstanceType:     "stream.standard.large",
			Platform:         "WINDOWS_SERVER_2019",
			IAMRoleARN:       testRoleARN,
			ImageARN:         testImageARN,
			SubnetIDs:        []string{"subnet-ccc"},
			SecurityGroupIDs: []string{"sg-222"},
		}},
		Images: []Image{{
			ARN:        testImageARN,
			Name:       "custom-image",
			State:      "AVAILABLE",
			Visibility: "PRIVATE",
			ImageType:  "custom",
			Platform:   "WINDOWS_SERVER_2019",
		}},
		FleetStackAssociations: []FleetStackAssociation{{
			FleetName: "sales-fleet",
			StackName: "sales-stack",
		}},
	}
}

func TestScannerEmitsAppStreamMetadataAndRelationships(t *testing.T) {
	envelopes, err := (Scanner{Client: fakeClient{snapshot: fullSnapshot()}}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	fleet := resourceByType(t, envelopes, awscloud.ResourceTypeAppStreamFleet)
	if got, want := fleet.Payload["resource_id"], testFleetARN; got != want {
		t.Fatalf("fleet resource_id = %#v, want %q", got, want)
	}
	if got, want := fleet.Payload["state"], "RUNNING"; got != want {
		t.Fatalf("fleet state = %#v, want %q", got, want)
	}
	fleetAttrs := attributesOf(t, fleet)
	assertAttribute(t, fleetAttrs, "fleet_type", "ON_DEMAND")
	assertAttribute(t, fleetAttrs, "instance_type", "stream.standard.medium")
	assertAttribute(t, fleetAttrs, "subnet_ids", []string{"subnet-aaa", "subnet-bbb"})

	stack := resourceByType(t, envelopes, awscloud.ResourceTypeAppStreamStack)
	if got, want := stack.Payload["resource_id"], testStackARN; got != want {
		t.Fatalf("stack resource_id = %#v, want %q", got, want)
	}
	stackAttrs := attributesOf(t, stack)
	assertAttribute(t, stackAttrs, "application_settings_enabled", true)
	assertAttribute(t, stackAttrs, "application_settings_s3_bucket", "appstream-settings-bucket")

	builder := resourceByType(t, envelopes, awscloud.ResourceTypeAppStreamImageBuilder)
	if got, want := builder.Payload["resource_id"], testImageBuilderARN; got != want {
		t.Fatalf("image builder resource_id = %#v, want %q", got, want)
	}

	image := resourceByType(t, envelopes, awscloud.ResourceTypeAppStreamImage)
	if got, want := image.Payload["resource_id"], testImageARN; got != want {
		t.Fatalf("image resource_id = %#v, want %q", got, want)
	}
	imageAttrs := attributesOf(t, image)
	assertAttribute(t, imageAttrs, "visibility", "PRIVATE")

	// fleet -> subnet edges keyed by bare subnet ids.
	subnetEdges := relationshipsByType(t, envelopes, awscloud.RelationshipAppStreamFleetUsesSubnet)
	if len(subnetEdges) != 2 {
		t.Fatalf("fleet->subnet edge count = %d, want 2", len(subnetEdges))
	}
	for _, edge := range subnetEdges {
		assertEdgeTargetType(t, edge, awscloud.ResourceTypeEC2Subnet)
		if got, _ := edge.Payload["source_resource_id"].(string); got != testFleetARN {
			t.Fatalf("fleet->subnet source_resource_id = %q, want %q", got, testFleetARN)
		}
	}

	// fleet -> security group edge keyed by bare sg id.
	sgEdge := relationshipByType(t, envelopes, awscloud.RelationshipAppStreamFleetUsesSecurityGroup)
	assertEdgeTarget(t, sgEdge, awscloud.ResourceTypeEC2SecurityGroup, "sg-111")

	// fleet -> IAM role edge keyed by role ARN.
	roleEdge := relationshipByType(t, envelopes, awscloud.RelationshipAppStreamFleetUsesIAMRole)
	assertEdgeTarget(t, roleEdge, awscloud.ResourceTypeIAMRole, testRoleARN)
	if got := roleEdge.Payload["target_arn"]; got != testRoleARN {
		t.Fatalf("fleet->role target_arn = %#v, want %q", got, testRoleARN)
	}

	// fleet -> image edge keyed by image ARN (the image node resource_id).
	imageEdge := relationshipByType(t, envelopes, awscloud.RelationshipAppStreamFleetUsesImage)
	assertEdgeTarget(t, imageEdge, awscloud.ResourceTypeAppStreamImage, testImageARN)

	// fleet <-> stack association keyed by the stack node resource_id (ARN).
	stackEdge := relationshipByType(t, envelopes, awscloud.RelationshipAppStreamFleetAssociatedWithStack)
	assertEdgeTarget(t, stackEdge, awscloud.ResourceTypeAppStreamStack, testStackARN)
	if got, _ := stackEdge.Payload["source_resource_id"].(string); got != testFleetARN {
		t.Fatalf("fleet->stack source_resource_id = %q, want %q", got, testFleetARN)
	}

	// image builder edges resolve too.
	builderRole := relationshipByType(t, envelopes, awscloud.RelationshipAppStreamImageBuilderUsesIAMRole)
	assertEdgeTarget(t, builderRole, awscloud.ResourceTypeIAMRole, testRoleARN)
	builderImage := relationshipByType(t, envelopes, awscloud.RelationshipAppStreamImageBuilderUsesImage)
	assertEdgeTarget(t, builderImage, awscloud.ResourceTypeAppStreamImage, testImageARN)

	// stack -> S3 bucket edges (app settings + storage connector), synthesized ARN.
	s3Edges := relationshipsByType(t, envelopes, awscloud.RelationshipAppStreamStackUsesS3Bucket)
	if len(s3Edges) != 2 {
		t.Fatalf("stack->s3 edge count = %d, want 2", len(s3Edges))
	}
	wantBucketARNs := map[string]bool{
		"arn:aws:s3:::appstream-settings-bucket": false,
		"arn:aws:s3:::appstream-home-folders":    false,
	}
	for _, edge := range s3Edges {
		assertEdgeTargetType(t, edge, awscloud.ResourceTypeS3Bucket)
		target, _ := edge.Payload["target_resource_id"].(string)
		if _, ok := wantBucketARNs[target]; !ok {
			t.Fatalf("unexpected stack->s3 target_resource_id %q", target)
		}
		wantBucketARNs[target] = true
	}
	for arn, seen := range wantBucketARNs {
		if !seen {
			t.Fatalf("missing stack->s3 edge for %q", arn)
		}
	}
}

func TestScannerSynthesizesGovCloudBucketARN(t *testing.T) {
	boundary := testBoundary()
	boundary.Region = "us-gov-west-1"
	snapshot := Snapshot{Stacks: []Stack{{
		ARN:                         "arn:aws-us-gov:appstream:us-gov-west-1:123456789012:stack/gov-stack",
		Name:                        "gov-stack",
		ApplicationSettingsS3Bucket: "gov-settings-bucket",
	}}}

	envelopes, err := (Scanner{Client: fakeClient{snapshot: snapshot}}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	edge := relationshipByType(t, envelopes, awscloud.RelationshipAppStreamStackUsesS3Bucket)
	wantARN := "arn:aws-us-gov:s3:::gov-settings-bucket"
	if got := edge.Payload["target_resource_id"]; got != wantARN {
		t.Fatalf("GovCloud stack->s3 target_resource_id = %#v, want %q", got, wantARN)
	}
	if got := edge.Payload["target_arn"]; got != wantARN {
		t.Fatalf("GovCloud stack->s3 target_arn = %#v, want %q", got, wantARN)
	}
}

func TestScannerSynthesizesChinaBucketARN(t *testing.T) {
	boundary := testBoundary()
	boundary.Region = "cn-north-1"
	snapshot := Snapshot{Stacks: []Stack{{
		ARN:                         "arn:aws-cn:appstream:cn-north-1:123456789012:stack/cn-stack",
		Name:                        "cn-stack",
		ApplicationSettingsS3Bucket: "cn-settings-bucket",
	}}}

	envelopes, err := (Scanner{Client: fakeClient{snapshot: snapshot}}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	edge := relationshipByType(t, envelopes, awscloud.RelationshipAppStreamStackUsesS3Bucket)
	wantARN := "arn:aws-cn:s3:::cn-settings-bucket"
	if got := edge.Payload["target_arn"]; got != wantARN {
		t.Fatalf("China stack->s3 target_arn = %#v, want %q", got, wantARN)
	}
}

func TestScannerResolvesAssociationStackByNameToStackResourceID(t *testing.T) {
	// The association API reports the stack by name; the edge must resolve it to
	// the stack node resource_id (its ARN), not key a dangling name.
	snapshot := Snapshot{
		Fleets: []Fleet{{ARN: testFleetARN, Name: "sales-fleet"}},
		Stacks: []Stack{{ARN: testStackARN, Name: "sales-stack"}},
		FleetStackAssociations: []FleetStackAssociation{{
			FleetName: "sales-fleet",
			StackName: "sales-stack",
		}},
	}
	envelopes, err := (Scanner{Client: fakeClient{snapshot: snapshot}}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	edge := relationshipByType(t, envelopes, awscloud.RelationshipAppStreamFleetAssociatedWithStack)
	assertEdgeTarget(t, edge, awscloud.ResourceTypeAppStreamStack, testStackARN)
	if got := edge.Payload["target_arn"]; got != testStackARN {
		t.Fatalf("association target_arn = %#v, want %q", got, testStackARN)
	}
}

func TestScannerOmitsRelationshipsWhenDependenciesAbsent(t *testing.T) {
	snapshot := Snapshot{
		Fleets: []Fleet{{ARN: testFleetARN, Name: "bare-fleet"}},
		Stacks: []Stack{{ARN: testStackARN, Name: "bare-stack"}},
	}
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

func TestScannerReturnsNilForEmptyAccount(t *testing.T) {
	envelopes, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if len(envelopes) != 0 {
		t.Fatalf("Scan() emitted %d envelopes for empty account, want 0", len(envelopes))
	}
}

func TestScannerRelationshipsSatisfyGraphJoinGuard(t *testing.T) {
	boundary := testBoundary()
	snapshot := fullSnapshot()
	fleet := snapshot.Fleets[0]
	stack := snapshot.Stacks[0]
	builder := snapshot.ImageBuilders[0]
	fleetIDByName, fleetARNByName := fleetIndex(snapshot.Fleets)
	stackIDByName := stackIndex(snapshot.Stacks)

	var observations []awscloud.RelationshipObservation
	observations = append(observations, fleetRelationships(boundary, fleet)...)
	observations = append(observations, imageBuilderRelationships(boundary, builder)...)
	observations = append(observations, stackS3Relationships(boundary, stack)...)
	assoc, ok := fleetStackRelationship(boundary, snapshot.FleetStackAssociations[0], fleetIDByName, fleetARNByName, stackIDByName)
	if !ok {
		t.Fatalf("expected fleet-stack association to resolve")
	}
	observations = append(observations, assoc)

	if len(observations) == 0 {
		t.Fatalf("expected non-empty observation set for fully populated fixture")
	}
	relguard.AssertObservations(t, observations...)
}

func TestScannerCanonicalizesPaddedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = "  " + awscloud.ServiceAppStream + "  "
	snapshot := Snapshot{Fleets: []Fleet{{ARN: testFleetARN, Name: "padded-fleet", State: "RUNNING"}}}

	envelopes, err := (Scanner{Client: fakeClient{snapshot: snapshot}}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if len(envelopes) == 0 {
		t.Fatalf("Scan() returned no envelopes")
	}
	for _, envelope := range envelopes {
		if got, want := envelope.Payload["service_kind"], awscloud.ServiceAppStream; got != want {
			t.Fatalf("envelope service_kind = %#v, want %q (padded service_kind must be canonicalized)", got, want)
		}
	}
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
		Fleets: []Fleet{{ARN: testFleetARN, Name: "sales-fleet"}},
		Warnings: []awscloud.WarningObservation{{
			Boundary:       testBoundary(),
			WarningKind:    awscloud.WarningThrottleSustained,
			ErrorClass:     "throttled",
			Message:        "AppStream DescribeImages throttled after SDK retries; image metadata omitted for this scan",
			SourceRecordID: "appstream_images_throttled",
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

func TestScannerRejectsNilClient(t *testing.T) {
	_, err := (Scanner{}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want client-required error")
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceAppStream,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:appstream:1",
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
