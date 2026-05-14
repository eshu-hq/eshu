package rds

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsRDSMetadataOnlyFactsAndRelationships(t *testing.T) {
	instanceARN := "arn:aws:rds:us-east-1:123456789012:db:orders-writer"
	clusterARN := "arn:aws:rds:us-east-1:123456789012:cluster:orders"
	subnetGroupARN := "arn:aws:rds:us-east-1:123456789012:subgrp:orders-db"
	kmsARN := "arn:aws:kms:us-east-1:123456789012:key/orders"
	monitoringRoleARN := "arn:aws:iam::123456789012:role/rds-monitoring"
	client := fakeClient{
		instances: []DBInstance{{
			ARN:                              instanceARN,
			Identifier:                       "orders-writer",
			ResourceID:                       "db-ORDERSWRITER",
			Class:                            "db.r7g.large",
			Engine:                           "postgres",
			EngineVersion:                    "16.3",
			Status:                           "available",
			EndpointAddress:                  "orders.example.us-east-1.rds.amazonaws.com",
			EndpointPort:                     5432,
			HostedZoneID:                     "Z2R2ITUGPM61AM",
			AvailabilityZone:                 "us-east-1a",
			SecondaryAvailabilityZone:        "us-east-1b",
			MultiAZ:                          true,
			PubliclyAccessible:               false,
			StorageEncrypted:                 true,
			KMSKeyID:                         kmsARN,
			IAMDatabaseAuthenticationEnabled: true,
			DeletionProtection:               true,
			BackupRetentionPeriod:            7,
			DBSubnetGroupName:                "orders-db",
			VPCID:                            "vpc-123",
			VPCSecurityGroupIDs:              []string{"sg-123"},
			ClusterIdentifier:                "orders",
			ParameterGroups: []ParameterGroup{{
				Name:  "orders-postgres16",
				State: "in-sync",
			}},
			OptionGroups: []OptionGroup{{
				Name:  "orders-options",
				State: "in-sync",
			}},
			MonitoringRoleARN:           monitoringRoleARN,
			PerformanceInsightsEnabled:  true,
			PerformanceInsightsKMSKeyID: "arn:aws:kms:us-east-1:123456789012:key/pi",
			Tags:                        map[string]string{"Environment": "prod"},
		}},
		clusters: []DBCluster{{
			ARN:                              clusterARN,
			Identifier:                       "orders",
			ResourceID:                       "cluster-ORDERS",
			Engine:                           "aurora-postgresql",
			EngineVersion:                    "16.3",
			Status:                           "available",
			EndpointAddress:                  "orders.cluster.example.us-east-1.rds.amazonaws.com",
			ReaderEndpointAddress:            "orders.cluster-ro.example.us-east-1.rds.amazonaws.com",
			HostedZoneID:                     "Z2R2ITUGPM61AM",
			Port:                             5432,
			MultiAZ:                          true,
			StorageEncrypted:                 true,
			KMSKeyID:                         kmsARN,
			IAMDatabaseAuthenticationEnabled: true,
			DeletionProtection:               true,
			BackupRetentionPeriod:            7,
			DBSubnetGroupName:                "orders-db",
			VPCSecurityGroupIDs:              []string{"sg-123"},
			Members: []ClusterMember{{
				DBInstanceIdentifier: "orders-writer",
				IsWriter:             true,
			}},
			ParameterGroup:     "orders-cluster-params",
			AssociatedRoleARNs: []string{"arn:aws:iam::123456789012:role/rds-s3-import"},
			Tags:               map[string]string{"Tier": "data"},
		}},
		subnetGroups: []DBSubnetGroup{{
			ARN:         subnetGroupARN,
			Name:        "orders-db",
			Description: "orders database subnets",
			Status:      "Complete",
			VPCID:       "vpc-123",
			SubnetIDs:   []string{"subnet-a", "subnet-b"},
			Tags:        map[string]string{"Network": "private"},
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	instance := resourceByType(t, envelopes, awscloud.ResourceTypeRDSDBInstance)
	if got, want := instance.Payload["arn"], instanceARN; got != want {
		t.Fatalf("instance arn = %#v, want %q", got, want)
	}
	if got, want := instance.Payload["state"], "available"; got != want {
		t.Fatalf("instance state = %#v, want %q", got, want)
	}
	instanceAttributes := attributesOf(t, instance)
	assertAttribute(t, instanceAttributes, "engine", "postgres")
	assertAttribute(t, instanceAttributes, "engine_version", "16.3")
	assertAttribute(t, instanceAttributes, "endpoint_address", "orders.example.us-east-1.rds.amazonaws.com")
	assertAttribute(t, instanceAttributes, "endpoint_port", int32(5432))
	assertAttribute(t, instanceAttributes, "storage_encrypted", true)
	assertAttribute(t, instanceAttributes, "iam_database_authentication_enabled", true)
	assertAttribute(t, instanceAttributes, "db_subnet_group_name", "orders-db")
	assertAttribute(t, instanceAttributes, "vpc_security_group_ids", []string{"sg-123"})
	assertAttribute(t, instanceAttributes, "performance_insights_enabled", true)
	for _, forbidden := range []string{
		"master_username",
		"password",
		"secret",
		"snapshot",
		"log_contents",
		"database_name",
		"schema_names",
		"table_names",
		"performance_insights_samples",
	} {
		if _, exists := instanceAttributes[forbidden]; exists {
			t.Fatalf("%s attribute persisted; RDS scanner must stay metadata-only", forbidden)
		}
	}

	cluster := resourceByType(t, envelopes, awscloud.ResourceTypeRDSDBCluster)
	clusterAttributes := attributesOf(t, cluster)
	assertAttribute(t, clusterAttributes, "endpoint_address", "orders.cluster.example.us-east-1.rds.amazonaws.com")
	assertAttribute(t, clusterAttributes, "reader_endpoint_address", "orders.cluster-ro.example.us-east-1.rds.amazonaws.com")
	assertAttribute(t, clusterAttributes, "member_instance_ids", []string{"orders-writer"})
	if _, exists := clusterAttributes["master_username"]; exists {
		t.Fatalf("master_username attribute persisted; RDS cluster scanner must stay metadata-only")
	}

	subnetGroup := resourceByType(t, envelopes, awscloud.ResourceTypeRDSDBSubnetGroup)
	subnetAttributes := attributesOf(t, subnetGroup)
	assertAttribute(t, subnetAttributes, "subnet_ids", []string{"subnet-a", "subnet-b"})
	assertAttribute(t, subnetAttributes, "vpc_id", "vpc-123")

	memberRelationship := relationshipByType(t, envelopes, awscloud.RelationshipRDSDBInstanceMemberOfCluster)
	if got, want := memberRelationship.Payload["target_arn"], clusterARN; got != want {
		t.Fatalf("cluster membership target_arn = %#v, want %q", got, want)
	}
	assertAttribute(t, attributesOf(t, memberRelationship), "is_writer", true)
	assertRelationshipTarget(t, envelopes, awscloud.RelationshipRDSDBInstanceInSubnetGroup, subnetGroupARN)
	assertRelationshipTarget(t, envelopes, awscloud.RelationshipRDSDBClusterInSubnetGroup, subnetGroupARN)
	assertRelationshipTarget(t, envelopes, awscloud.RelationshipRDSDBInstanceUsesSecurityGroup, "arn:aws:ec2:us-east-1:123456789012:security-group/sg-123")
	assertRelationshipTarget(t, envelopes, awscloud.RelationshipRDSDBClusterUsesSecurityGroup, "arn:aws:ec2:us-east-1:123456789012:security-group/sg-123")
	assertRelationshipTarget(t, envelopes, awscloud.RelationshipRDSDBInstanceUsesKMSKey, kmsARN)
	assertRelationshipTarget(t, envelopes, awscloud.RelationshipRDSDBClusterUsesKMSKey, kmsARN)
	assertRelationshipTarget(t, envelopes, awscloud.RelationshipRDSDBInstanceUsesMonitoringRole, monitoringRoleARN)
	assertRelationshipTarget(t, envelopes, awscloud.RelationshipRDSDBClusterUsesIAMRole, "arn:aws:iam::123456789012:role/rds-s3-import")
	assertRelationshipTarget(t, envelopes, awscloud.RelationshipRDSDBInstanceUsesParameterGroup, "orders-postgres16")
	assertRelationshipTarget(t, envelopes, awscloud.RelationshipRDSDBClusterUsesParameterGroup, "orders-cluster-params")
	assertRelationshipTarget(t, envelopes, awscloud.RelationshipRDSDBInstanceUsesOptionGroup, "orders-options")
}

func TestScannerSkipsRelationshipsWithoutTargets(t *testing.T) {
	client := fakeClient{instances: []DBInstance{{
		ARN:               "arn:aws:rds:us-east-1:123456789012:db:orders-writer",
		Identifier:        "orders-writer",
		DBSubnetGroupName: "missing-subnet-group",
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if got := countRelationships(envelopes); got != 0 {
		t.Fatalf("relationship count = %d, want 0 without direct target identity", got)
	}
}

func TestScannerDoesNotTreatNonARNKMSIdentifierAsARN(t *testing.T) {
	client := fakeClient{instances: []DBInstance{{
		ARN:        "arn:aws:rds:us-east-1:123456789012:db:orders-writer",
		Identifier: "orders-writer",
		KMSKeyID:   "alias/orders",
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	relationship := relationshipByType(t, envelopes, awscloud.RelationshipRDSDBInstanceUsesKMSKey)
	if got, want := relationship.Payload["target_resource_id"], "alias/orders"; got != want {
		t.Fatalf("target_resource_id = %#v, want %q", got, want)
	}
	if got := relationship.Payload["target_arn"]; got != "" {
		t.Fatalf("target_arn = %#v, want empty for non-ARN KMS identifier", got)
	}
}

func TestScannerDoesNotTreatFallbackTargetIDsAsARNs(t *testing.T) {
	client := fakeClient{
		instances: []DBInstance{{
			ARN:               "arn:aws:rds:us-east-1:123456789012:db:orders-writer",
			Identifier:        "orders-writer",
			ClusterIdentifier: "orders",
			DBSubnetGroupName: "orders-db",
		}},
		clusters: []DBCluster{{
			Identifier:        "orders",
			ResourceID:        "cluster-ORDERS",
			DBSubnetGroupName: "orders-db",
		}},
		subnetGroups: []DBSubnetGroup{{
			Name: "orders-db",
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, relationshipType := range []string{
		awscloud.RelationshipRDSDBInstanceMemberOfCluster,
		awscloud.RelationshipRDSDBInstanceInSubnetGroup,
		awscloud.RelationshipRDSDBClusterInSubnetGroup,
	} {
		relationship := relationshipByType(t, envelopes, relationshipType)
		if got := relationship.Payload["target_arn"]; got != "" {
			t.Fatalf("%s target_arn = %#v, want empty for fallback target identity", relationshipType, got)
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

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceRDS,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:rds:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 14, 18, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	instances    []DBInstance
	clusters     []DBCluster
	subnetGroups []DBSubnetGroup
}

func (c fakeClient) ListDBInstances(context.Context) ([]DBInstance, error) {
	return c.instances, nil
}

func (c fakeClient) ListDBClusters(context.Context) ([]DBCluster, error) {
	return c.clusters, nil
}

func (c fakeClient) ListDBSubnetGroups(context.Context) ([]DBSubnetGroup, error) {
	return c.subnetGroups, nil
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

func assertRelationshipTarget(
	t *testing.T,
	envelopes []facts.Envelope,
	relationshipType string,
	targetID string,
) {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got != relationshipType {
			continue
		}
		if got, _ := envelope.Payload["target_resource_id"].(string); got == targetID {
			return
		}
		if got, _ := envelope.Payload["target_arn"].(string); got == targetID {
			return
		}
	}
	t.Fatalf("missing relationship %q target %q in %#v", relationshipType, targetID, envelopes)
}

func countRelationships(envelopes []facts.Envelope) int {
	var count int
	for _, envelope := range envelopes {
		if envelope.FactKind == facts.AWSRelationshipFactKind {
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
		gotStrings, ok := got.([]string)
		if !ok || len(gotStrings) != len(want) {
			return false
		}
		for i := range want {
			if gotStrings[i] != want[i] {
				return false
			}
		}
		return true
	default:
		return got == want
	}
}
