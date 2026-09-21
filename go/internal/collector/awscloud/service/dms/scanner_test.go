// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dms

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/internal/relguard"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	testInstanceARN     = "arn:aws:dms:us-east-1:123456789012:rep:ABCDEFGHIJKLMNOP"
	testEndpointSrcARN  = "arn:aws:dms:us-east-1:123456789012:endpoint:SOURCEENDPOINTAAA"
	testEndpointDstARN  = "arn:aws:dms:us-east-1:123456789012:endpoint:TARGETENDPOINTBBB"
	testTaskARN         = "arn:aws:dms:us-east-1:123456789012:task:MIGRATIONTASKCCCC"
	testKMSARN          = "arn:aws:kms:us-east-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
	testStreamARN       = "arn:aws:kinesis:us-east-1:123456789012:stream/cdc-events"
	testSecretARN       = "arn:aws:secretsmanager:us-east-1:123456789012:secret:dms/source-AbCdEf"
	testSubnetGroupName = "dms-subnet-group"
)

func fullSnapshot() Snapshot {
	return Snapshot{
		SubnetGroups: []ReplicationSubnetGroup{{
			Identifier:  testSubnetGroupName,
			Description: "DMS subnet group",
			Status:      "Complete",
			VPCID:       "vpc-0abc1234",
			SubnetIDs:   []string{"subnet-0a1b2c3d", "subnet-0e5f6a7b"},
			Tags:        map[string]string{"Environment": "prod"},
		}},
		ReplicationInstances: []ReplicationInstance{{
			ARN:                   testInstanceARN,
			Identifier:            "dms-prod",
			Class:                 "dms.r5.large",
			EngineVersion:         "3.5.2",
			Status:                "available",
			AllocatedStorageGiB:   100,
			MultiAZ:               true,
			PubliclyAccessible:    false,
			AvailabilityZone:      "us-east-1a",
			NetworkType:           "IPV4",
			KMSKeyID:              testKMSARN,
			SubnetGroupIdentifier: testSubnetGroupName,
			VPCID:                 "vpc-0abc1234",
			SubnetIDs:             []string{"subnet-0a1b2c3d", "subnet-0e5f6a7b"},
			SecurityGroupIDs:      []string{"sg-0aabbccdd", "sg-0eeff0011"},
			CreateTime:            time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
			Tags:                  map[string]string{"Team": "data"},
		}},
		Endpoints: []Endpoint{
			{
				ARN:                    testEndpointSrcARN,
				Identifier:             "source-postgres",
				EndpointType:           "source",
				EngineName:             "postgres",
				EngineDisplayName:      "PostgreSQL",
				SSLMode:                "require",
				Status:                 "active",
				DatabaseName:           "appdb",
				Port:                   5432,
				KMSKeyID:               testKMSARN,
				SecretsManagerSecretID: testSecretARN,
			},
			{
				ARN:              testEndpointDstARN,
				Identifier:       "target-stream",
				EndpointType:     "target",
				EngineName:       "kinesis",
				SSLMode:          "none",
				Status:           "active",
				KinesisStreamARN: testStreamARN,
				S3BucketName:     "dms-cdc-bucket",
			},
		},
		Tasks: []ReplicationTask{{
			ARN:                    testTaskARN,
			Identifier:             "prod-migration",
			MigrationType:          "full-load-and-cdc",
			Status:                 "running",
			SourceEndpointARN:      testEndpointSrcARN,
			TargetEndpointARN:      testEndpointDstARN,
			ReplicationInstanceARN: testInstanceARN,
			CreationDate:           time.Date(2026, 5, 14, 12, 30, 0, 0, time.UTC),
			Tags:                   map[string]string{"Pipeline": "cdc"},
		}},
	}
}

func TestScannerEmitsDMSMetadataAndRelationships(t *testing.T) {
	envelopes, err := (Scanner{Client: fakeClient{snapshot: fullSnapshot()}}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	// Replication instance node.
	instance := resourceByType(t, envelopes, awscloud.ResourceTypeDMSReplicationInstance)
	if got, want := instance.Payload["resource_id"], testInstanceARN; got != want {
		t.Fatalf("instance resource_id = %#v, want %q", got, want)
	}
	instAttrs := attributesOf(t, instance)
	assertAttribute(t, instAttrs, "replication_instance_class", "dms.r5.large")
	assertAttribute(t, instAttrs, "multi_az", true)
	assertAttribute(t, instAttrs, "kms_key_id", testKMSARN)

	// Subnet group node.
	group := resourceByType(t, envelopes, awscloud.ResourceTypeDMSReplicationSubnetGroup)
	if got, want := group.Payload["resource_id"], testSubnetGroupName; got != want {
		t.Fatalf("subnet group resource_id = %#v, want %q", got, want)
	}

	// instance -> subnet group edge.
	inGroup := relationshipByType(t, envelopes, awscloud.RelationshipDMSReplicationInstanceInSubnetGroup)
	assertEdgeTarget(t, inGroup, awscloud.ResourceTypeDMSReplicationSubnetGroup, testSubnetGroupName)

	// instance -> subnet edges (bare subnet ids the EC2 scanner publishes).
	subnetEdges := relationshipsByType(t, envelopes, awscloud.RelationshipDMSReplicationInstanceInSubnet)
	if len(subnetEdges) != 2 {
		t.Fatalf("instance->subnet edge count = %d, want 2", len(subnetEdges))
	}
	for _, edge := range subnetEdges {
		if got := edge.Payload["target_type"]; got != awscloud.ResourceTypeEC2Subnet {
			t.Fatalf("instance->subnet target_type = %#v, want %q", got, awscloud.ResourceTypeEC2Subnet)
		}
	}

	// instance -> security group edges (bare sg ids).
	sgEdges := relationshipsByType(t, envelopes, awscloud.RelationshipDMSReplicationInstanceUsesSecurityGroup)
	if len(sgEdges) != 2 {
		t.Fatalf("instance->sg edge count = %d, want 2", len(sgEdges))
	}

	// instance -> KMS edge keyed by the reported key ARN.
	instKMS := relationshipByType(t, envelopes, awscloud.RelationshipDMSReplicationInstanceUsesKMSKey)
	assertEdgeTarget(t, instKMS, awscloud.ResourceTypeKMSKey, testKMSARN)

	// subnet group -> VPC edge (bare vpc id).
	groupVPC := relationshipByType(t, envelopes, awscloud.RelationshipDMSReplicationSubnetGroupInVPC)
	assertEdgeTarget(t, groupVPC, awscloud.ResourceTypeEC2VPC, "vpc-0abc1234")
	if got := groupVPC.Payload["source_resource_id"]; got != testSubnetGroupName {
		t.Fatalf("subnet group->vpc source_resource_id = %#v, want %q", got, testSubnetGroupName)
	}

	// subnet group -> subnet edges.
	groupSubnets := relationshipsByType(t, envelopes, awscloud.RelationshipDMSReplicationSubnetGroupHasSubnet)
	if len(groupSubnets) != 2 {
		t.Fatalf("subnet group->subnet edge count = %d, want 2", len(groupSubnets))
	}

	// endpoint -> KMS edge.
	epKMS := relationshipByType(t, envelopes, awscloud.RelationshipDMSEndpointUsesKMSKey)
	assertEdgeTarget(t, epKMS, awscloud.ResourceTypeKMSKey, testKMSARN)

	// endpoint -> secret edge keyed by the secret ARN.
	epSecret := relationshipByType(t, envelopes, awscloud.RelationshipDMSEndpointUsesSecret)
	assertEdgeTarget(t, epSecret, awscloud.ResourceTypeSecretsManagerSecret, testSecretARN)

	// endpoint -> Kinesis stream edge.
	epStream := relationshipByType(t, envelopes, awscloud.RelationshipDMSEndpointTargetsKinesisStream)
	assertEdgeTarget(t, epStream, awscloud.ResourceTypeKinesisDataStream, testStreamARN)

	// endpoint -> S3 bucket edge keyed by the synthesized partition-aware ARN.
	epS3 := relationshipByType(t, envelopes, awscloud.RelationshipDMSEndpointTargetsS3Bucket)
	assertEdgeTarget(t, epS3, awscloud.ResourceTypeS3Bucket, "arn:aws:s3:::dms-cdc-bucket")

	// task -> source/target endpoint and runs-on-instance edges.
	taskSrc := relationshipByType(t, envelopes, awscloud.RelationshipDMSReplicationTaskUsesSourceEndpoint)
	assertEdgeTarget(t, taskSrc, awscloud.ResourceTypeDMSEndpoint, testEndpointSrcARN)
	taskDst := relationshipByType(t, envelopes, awscloud.RelationshipDMSReplicationTaskUsesTargetEndpoint)
	assertEdgeTarget(t, taskDst, awscloud.ResourceTypeDMSEndpoint, testEndpointDstARN)
	taskInstance := relationshipByType(t, envelopes, awscloud.RelationshipDMSReplicationTaskRunsOnInstance)
	assertEdgeTarget(t, taskInstance, awscloud.ResourceTypeDMSReplicationInstance, testInstanceARN)

	// No credential / secret-value / connection-string leakage in any payload.
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		attrs, _ := envelope.Payload["attributes"].(map[string]any)
		for _, forbidden := range []string{
			"password", "username", "server_name", "connection_attributes",
			"extra_connection_attributes", "external_table_definition",
			"ssl_key", "certificate_pem", "secret_value", "table_mappings",
			"task_settings",
		} {
			if _, exists := attrs[forbidden]; exists {
				t.Fatalf("%s attribute persisted; DMS scanner must stay metadata-only", forbidden)
			}
		}
	}
}

func TestScannerSynthesizesGovCloudBucketARN(t *testing.T) {
	boundary := testBoundary()
	boundary.Region = "us-gov-west-1"
	client := fakeClient{snapshot: Snapshot{Endpoints: []Endpoint{{
		ARN:          "arn:aws-us-gov:dms:us-gov-west-1:123456789012:endpoint:GOVENDPOINTAAAAA",
		Identifier:   "gov-target",
		EndpointType: "target",
		EngineName:   "s3",
		S3BucketName: "gov-cdc-bucket",
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	epS3 := relationshipByType(t, envelopes, awscloud.RelationshipDMSEndpointTargetsS3Bucket)
	wantARN := "arn:aws-us-gov:s3:::gov-cdc-bucket"
	if got := epS3.Payload["target_resource_id"]; got != wantARN {
		t.Fatalf("GovCloud endpoint->s3 target_resource_id = %#v, want %q", got, wantARN)
	}
	if got := epS3.Payload["target_arn"]; got != wantARN {
		t.Fatalf("GovCloud endpoint->s3 target_arn = %#v, want %q", got, wantARN)
	}
}

func TestScannerSynthesizesChinaBucketARN(t *testing.T) {
	boundary := testBoundary()
	boundary.Region = "cn-north-1"
	client := fakeClient{snapshot: Snapshot{Endpoints: []Endpoint{{
		ARN:          "arn:aws-cn:dms:cn-north-1:123456789012:endpoint:CNENDPOINTBBBBBBB",
		Identifier:   "cn-target",
		EndpointType: "target",
		EngineName:   "s3",
		S3BucketName: "cn-cdc-bucket",
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	epS3 := relationshipByType(t, envelopes, awscloud.RelationshipDMSEndpointTargetsS3Bucket)
	wantARN := "arn:aws-cn:s3:::cn-cdc-bucket"
	if got := epS3.Payload["target_arn"]; got != wantARN {
		t.Fatalf("China endpoint->s3 target_arn = %#v, want %q", got, wantARN)
	}
}

func TestScannerOmitsRelationshipsWhenDependenciesAbsent(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{
		ReplicationInstances: []ReplicationInstance{{
			ARN:        testInstanceARN,
			Identifier: "bare-instance",
			Status:     "available",
		}},
		Endpoints: []Endpoint{{
			ARN:          testEndpointSrcARN,
			Identifier:   "bare-endpoint",
			EndpointType: "source",
			EngineName:   "postgres",
		}},
		Tasks: []ReplicationTask{{
			ARN:           testTaskARN,
			Identifier:    "bare-task",
			MigrationType: "cdc",
			Status:        "stopped",
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

func TestScannerOmitsKMSEdgeArnForNonARNKey(t *testing.T) {
	client := fakeClient{snapshot: Snapshot{ReplicationInstances: []ReplicationInstance{{
		ARN:        testInstanceARN,
		Identifier: "alias-key-instance",
		KMSKeyID:   "1234abcd-12ab-34cd-56ef-1234567890ab",
	}}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	instKMS := relationshipByType(t, envelopes, awscloud.RelationshipDMSReplicationInstanceUsesKMSKey)
	if got, want := instKMS.Payload["target_resource_id"], "1234abcd-12ab-34cd-56ef-1234567890ab"; got != want {
		t.Fatalf("kms target_resource_id = %#v, want %q", got, want)
	}
	if got := instKMS.Payload["target_arn"]; got != "" {
		t.Fatalf("kms target_arn = %#v, want empty for non-ARN key identifier", got)
	}
}

func TestScannerRelationshipsSatisfyGraphJoinGuard(t *testing.T) {
	boundary := testBoundary()
	snapshot := fullSnapshot()
	var observations []awscloud.RelationshipObservation
	observations = append(observations, instanceRelationships(boundary, snapshot.ReplicationInstances[0])...)
	observations = append(observations, subnetGroupRelationships(boundary, snapshot.SubnetGroups[0])...)
	for _, endpoint := range snapshot.Endpoints {
		observations = append(observations, endpointRelationships(boundary, endpoint)...)
	}
	observations = append(observations, taskRelationships(boundary, snapshot.Tasks[0])...)
	if len(observations) == 0 {
		t.Fatalf("expected relationships for the fully populated fixture")
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
		ReplicationInstances: []ReplicationInstance{{ARN: testInstanceARN, Identifier: "dms-prod"}},
		Warnings: []awscloud.WarningObservation{{
			Boundary:       testBoundary(),
			WarningKind:    awscloud.WarningThrottleSustained,
			ErrorClass:     "throttled",
			Message:        "DMS DescribeEndpoints throttled after SDK retries; endpoint metadata omitted for this scan",
			SourceRecordID: "dms_endpoints_throttled",
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
		t.Fatalf("Scan() error = nil, want client-required error")
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceDMS,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:dms:1",
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
	edges := relationshipsByType(t, envelopes, relationshipType)
	if len(edges) == 0 {
		t.Fatalf("missing relationship_type %q in %#v", relationshipType, envelopes)
	}
	return edges[0]
}

func relationshipsByType(t *testing.T, envelopes []facts.Envelope, relationshipType string) []facts.Envelope {
	t.Helper()
	var matches []facts.Envelope
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			matches = append(matches, envelope)
		}
	}
	return matches
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
