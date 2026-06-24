// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package emr

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceEMR,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:emr:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        7,
		ObservedAt:          time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC),
	}
}

func TestScannerEmitsClusterInstanceGroupAndAllClusterRelationships(t *testing.T) {
	clusterARN := "arn:aws:elasticmapreduce:us-east-1:123456789012:cluster/j-ABC123"
	client := fakeClient{
		clusters: []Cluster{{
			ARN:                 clusterARN,
			ID:                  "j-ABC123",
			Name:                "analytics",
			State:               "RUNNING",
			ReleaseLabel:        "emr-7.1.0",
			ServiceRole:         "EMR_DefaultRole",
			AutoScalingRole:     "arn:aws:iam::123456789012:role/EMR_AutoScaling_DefaultRole",
			InstanceProfile:     "arn:aws:iam::123456789012:instance-profile/EMR_EC2_DefaultRole",
			SecurityConfigName:  "prod-sec-config",
			LogEncryptionKMSKey: "arn:aws:kms:us-east-1:123456789012:key/abc-def",
			SubnetID:            "subnet-aaa",
			RequestedSubnetIDs:  []string{"subnet-aaa", "subnet-bbb"},
			SecurityGroupIDs:    []string{"sg-master", "sg-slave", "sg-master"},
			Tags:                map[string]string{"team": "data"},
			InstanceGroups: []InstanceGroup{{
				ID:           "ig-111",
				Name:         "Master",
				GroupType:    "MASTER",
				InstanceType: "m5.xlarge",
				State:        "RUNNING",
			}},
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	cluster := resourceByType(t, envelopes, awscloud.ResourceTypeEMRCluster)
	if got := cluster.Payload["resource_id"]; got != clusterARN {
		t.Fatalf("cluster resource_id = %v, want %q", got, clusterARN)
	}
	assertResourceType(t, envelopes, awscloud.ResourceTypeEMRInstanceGroup)

	// Subnet join: bare subnet id, target_type aws_ec2_subnet, deduplicated.
	assertEdge(t, envelopes, awscloud.RelationshipEMRClusterUsesSubnet, "subnet-aaa", awscloud.ResourceTypeEC2Subnet, "")
	assertEdge(t, envelopes, awscloud.RelationshipEMRClusterUsesSubnet, "subnet-bbb", awscloud.ResourceTypeEC2Subnet, "")
	if got := countRelationships(envelopes, awscloud.RelationshipEMRClusterUsesSubnet); got != 2 {
		t.Fatalf("subnet edge count = %d, want 2 (deduplicated)", got)
	}
	// Security group join: bare sg id, aws_ec2_security_group, deduplicated.
	if got := countRelationships(envelopes, awscloud.RelationshipEMRClusterUsesSecurityGroup); got != 2 {
		t.Fatalf("security group edge count = %d, want 2 (deduplicated)", got)
	}
	// IAM role join: ServiceRole name (no ARN) + AutoScalingRole ARN.
	assertEdge(t, envelopes, awscloud.RelationshipEMRClusterUsesIAMRole, "EMR_DefaultRole", awscloud.ResourceTypeIAMRole, "")
	assertEdge(t, envelopes, awscloud.RelationshipEMRClusterUsesIAMRole,
		"arn:aws:iam::123456789012:role/EMR_AutoScaling_DefaultRole", awscloud.ResourceTypeIAMRole,
		"arn:aws:iam::123456789012:role/EMR_AutoScaling_DefaultRole")
	// Instance profile join: ARN target carries target_arn.
	assertEdge(t, envelopes, awscloud.RelationshipEMRClusterUsesInstanceProfile,
		"arn:aws:iam::123456789012:instance-profile/EMR_EC2_DefaultRole", awscloud.ResourceTypeIAMInstanceProfile,
		"arn:aws:iam::123456789012:instance-profile/EMR_EC2_DefaultRole")
	// Security configuration join: name only.
	assertEdge(t, envelopes, awscloud.RelationshipEMRClusterUsesSecurityConfiguration,
		"prod-sec-config", awscloud.ResourceTypeEMRSecurityConfiguration, "")
	// KMS join: ARN key carries target_arn.
	assertEdge(t, envelopes, awscloud.RelationshipEMRClusterUsesKMSKey,
		"arn:aws:kms:us-east-1:123456789012:key/abc-def", awscloud.ResourceTypeKMSKey,
		"arn:aws:kms:us-east-1:123456789012:key/abc-def")
	// Instance group membership join: scoped id.
	assertEdge(t, envelopes, awscloud.RelationshipEMRClusterHasInstanceGroup,
		clusterARN+"/ig-111", awscloud.ResourceTypeEMRInstanceGroup, "")
}

func TestScannerEmitsInstanceFleetClusterRelationship(t *testing.T) {
	clusterARN := "arn:aws:elasticmapreduce:us-east-1:123456789012:cluster/j-FLEET"
	client := fakeClient{
		clusters: []Cluster{{
			ARN:                clusterARN,
			ID:                 "j-FLEET",
			Name:               "fleeted",
			State:              "WAITING",
			InstanceCollection: "INSTANCE_FLEET",
			InstanceFleets: []InstanceFleet{{
				ID:        "if-222",
				Name:      "core",
				FleetType: "CORE",
				State:     "RUNNING",
			}},
		}},
	}
	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	assertResourceType(t, envelopes, awscloud.ResourceTypeEMRInstanceFleet)
	assertEdge(t, envelopes, awscloud.RelationshipEMRClusterHasInstanceFleet,
		clusterARN+"/if-222", awscloud.ResourceTypeEMRInstanceFleet, "")
}

func TestScannerEmitsSecurityConfigurationNameOnly(t *testing.T) {
	client := fakeClient{
		securityConfigs: []SecurityConfiguration{{
			Name:      "kerberos-config",
			CreatedAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
		}},
	}
	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	config := resourceByType(t, envelopes, awscloud.ResourceTypeEMRSecurityConfiguration)
	if got := config.Payload["resource_id"]; got != "kerberos-config" {
		t.Fatalf("security config resource_id = %v, want kerberos-config", got)
	}
	// No policy body must ever appear in attributes. The only attribute is
	// created_at; the scanner-owned type has no field for a policy body so a
	// leak path does not exist.
	attributes, _ := config.Payload["attributes"].(map[string]any)
	for key := range attributes {
		if key != "created_at" {
			t.Fatalf("security config exposes unexpected attribute %q; only created_at is allowed", key)
		}
	}
}

func TestScannerEmitsServerlessApplicationAndRelationships(t *testing.T) {
	appARN := "arn:aws:emr-serverless:us-east-1:123456789012:/applications/00abc"
	client := fakeClient{
		applications: []ServerlessApplication{{
			ARN:              appARN,
			ID:               "00abc",
			Name:             "spark-app",
			State:            "STARTED",
			ReleaseLabel:     "emr-7.1.0",
			Type:             "SPARK",
			DiskEncryptKMS:   "arn:aws:kms:us-east-1:123456789012:key/serverless-key",
			SubnetIDs:        []string{"subnet-ccc"},
			SecurityGroupIDs: []string{"sg-app"},
		}},
	}
	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	app := resourceByType(t, envelopes, awscloud.ResourceTypeEMRServerlessApplication)
	if got := app.Payload["resource_id"]; got != appARN {
		t.Fatalf("application resource_id = %v, want %q", got, appARN)
	}
	assertEdge(t, envelopes, awscloud.RelationshipEMRServerlessApplicationUsesSubnet,
		"subnet-ccc", awscloud.ResourceTypeEC2Subnet, "")
	assertEdge(t, envelopes, awscloud.RelationshipEMRServerlessApplicationUsesSecurityGroup,
		"sg-app", awscloud.ResourceTypeEC2SecurityGroup, "")
	assertEdge(t, envelopes, awscloud.RelationshipEMRServerlessApplicationUsesKMSKey,
		"arn:aws:kms:us-east-1:123456789012:key/serverless-key", awscloud.ResourceTypeKMSKey,
		"arn:aws:kms:us-east-1:123456789012:key/serverless-key")
}

func TestScannerEmitsStudioVPCSubnetRoleKMSAndSessionMapping(t *testing.T) {
	studioARN := "arn:aws:elasticmapreduce:us-east-1:123456789012:studio/es-XYZ"
	client := fakeClient{
		studios: []Studio{{
			ARN:               studioARN,
			ID:                "es-XYZ",
			Name:              "data-studio",
			AuthMode:          "SSO",
			VPCID:             "vpc-123",
			SubnetIDs:         []string{"subnet-ddd"},
			EngineSecGroupID:  "sg-engine",
			WorkspaceSecGroup: "sg-workspace",
			ServiceRole:       "arn:aws:iam::123456789012:role/EMRStudioService",
			UserRole:          "arn:aws:iam::123456789012:role/EMRStudioUser",
			EncryptionKeyARN:  "arn:aws:kms:us-east-1:123456789012:key/studio-key",
			SessionMappings: []StudioSessionMapping{{
				StudioID:         "es-XYZ",
				IdentityID:       "id-1",
				IdentityName:     "analysts",
				IdentityType:     "GROUP",
				SessionPolicyARN: "arn:aws:iam::aws:policy/AmazonEMRFullAccessPolicy_v2",
			}},
		}},
	}
	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	assertResourceType(t, envelopes, awscloud.ResourceTypeEMRStudio)
	mapping := resourceByType(t, envelopes, awscloud.ResourceTypeEMRStudioSessionMapping)
	mappingID := studioARN + "/session-mapping/group:id-1"
	if got := mapping.Payload["resource_id"]; got != mappingID {
		t.Fatalf("session mapping resource_id = %v, want %q", got, mappingID)
	}

	// Studio->VPC is the only direct VPC join EMR exposes (clusters and
	// applications derive VPC from subnet membership downstream).
	assertEdge(t, envelopes, awscloud.RelationshipEMRStudioInVPC, "vpc-123", awscloud.ResourceTypeEC2VPC, "")
	assertEdge(t, envelopes, awscloud.RelationshipEMRStudioUsesSubnet, "subnet-ddd", awscloud.ResourceTypeEC2Subnet, "")
	if got := countRelationships(envelopes, awscloud.RelationshipEMRStudioUsesSecurityGroup); got != 2 {
		t.Fatalf("studio security group edge count = %d, want 2", got)
	}
	if got := countRelationships(envelopes, awscloud.RelationshipEMRStudioUsesIAMRole); got != 2 {
		t.Fatalf("studio IAM role edge count = %d, want 2", got)
	}
	assertEdge(t, envelopes, awscloud.RelationshipEMRStudioUsesKMSKey,
		"arn:aws:kms:us-east-1:123456789012:key/studio-key", awscloud.ResourceTypeKMSKey,
		"arn:aws:kms:us-east-1:123456789012:key/studio-key")
	assertEdge(t, envelopes, awscloud.RelationshipEMRStudioHasSessionMapping,
		mappingID, awscloud.ResourceTypeEMRStudioSessionMapping, "")
}

// TestSessionMappingOmitsLastModified proves the session-mapping observation
// never emits a last_modified_at attribute. ListStudioSessionMappings returns
// SessionMappingSummary, which only carries CreationTime; the AWS SDK exposes
// LastModifiedTime exclusively through the per-mapping GetStudioSessionMapping
// detail call, which the metadata-only scanner does not make. Emitting
// last_modified_at would always be null in production, so the attribute must
// not exist.
func TestSessionMappingOmitsLastModified(t *testing.T) {
	studioARN := "arn:aws:elasticmapreduce:us-east-1:123456789012:studio/es-LM"
	client := fakeClient{
		studios: []Studio{{
			ARN:  studioARN,
			ID:   "es-LM",
			Name: "lm-studio",
			SessionMappings: []StudioSessionMapping{{
				StudioID:     "es-LM",
				IdentityID:   "id-9",
				IdentityName: "engineers",
				IdentityType: "GROUP",
				CreatedAt:    time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
			}},
		}},
	}
	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	mapping := resourceByType(t, envelopes, awscloud.ResourceTypeEMRStudioSessionMapping)
	attributes, _ := mapping.Payload["attributes"].(map[string]any)
	if _, exists := attributes["last_modified_at"]; exists {
		t.Fatalf("session mapping emits last_modified_at, but ListStudioSessionMappings never reports it; attribute must be removed")
	}
	if got := attributes["created_at"]; got == nil {
		t.Fatalf("session mapping created_at = nil, want the mapped CreationTime")
	}
}

func TestScannerSurfacesListErrors(t *testing.T) {
	client := fakeClient{clustersErr: errBoom}
	if _, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary()); err == nil {
		t.Fatalf("Scan() error = nil, want surfaced ListClusters error")
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceLambda
	if _, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary); err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func TestScannerRequiresClient(t *testing.T) {
	if _, err := (Scanner{}).Scan(context.Background(), testBoundary()); err == nil {
		t.Fatalf("Scan() error = nil, want client-required error")
	}
}

// errBoom is a sentinel error used to prove list failures are surfaced.
var errBoom = &boomError{}

type boomError struct{}

func (*boomError) Error() string { return "boom" }

type fakeClient struct {
	clusters        []Cluster
	clustersErr     error
	securityConfigs []SecurityConfiguration
	applications    []ServerlessApplication
	studios         []Studio
}

func (c fakeClient) ListClusters(context.Context) ([]Cluster, error) {
	return c.clusters, c.clustersErr
}

func (c fakeClient) ListSecurityConfigurations(context.Context) ([]SecurityConfiguration, error) {
	return c.securityConfigs, nil
}

func (c fakeClient) ListServerlessApplications(context.Context) ([]ServerlessApplication, error) {
	return c.applications, nil
}

func (c fakeClient) ListStudios(context.Context) ([]Studio, error) {
	return c.studios, nil
}

func assertResourceType(t *testing.T, envelopes []facts.Envelope, resourceType string) {
	t.Helper()
	_ = resourceByType(t, envelopes, resourceType)
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
	t.Fatalf("missing resource_type %q", resourceType)
	return facts.Envelope{}
}

// assertEdge confirms exactly one relationship of the given type targets
// targetID with the expected target_type and target_arn. An empty wantARN
// asserts the edge carries no target_arn.
func assertEdge(
	t *testing.T,
	envelopes []facts.Envelope,
	relationshipType string,
	targetID string,
	wantType string,
	wantARN string,
) {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		payload := envelope.Payload
		if payload["relationship_type"] != relationshipType || payload["target_resource_id"] != targetID {
			continue
		}
		if got := payload["target_type"]; got != wantType {
			t.Fatalf("%s -> %s target_type = %v, want %q", relationshipType, targetID, got, wantType)
		}
		if got, _ := payload["target_type"].(string); got == "" {
			t.Fatalf("%s -> %s has empty target_type", relationshipType, targetID)
		}
		if got := payload["target_arn"]; got != wantARN {
			t.Fatalf("%s -> %s target_arn = %v, want %q", relationshipType, targetID, got, wantARN)
		}
		return
	}
	t.Fatalf("missing relationship %q targeting %q", relationshipType, targetID)
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
