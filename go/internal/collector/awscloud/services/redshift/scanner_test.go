// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package redshift

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsProvisionedRedshiftMetadataAndRelationships(t *testing.T) {
	clusterARN := "arn:aws:redshift:us-east-1:123456789012:cluster:analytics"
	parameterGroupARN := "arn:aws:redshift:us-east-1:123456789012:parametergroup:analytics-params"
	subnetGroupARN := "arn:aws:redshift:us-east-1:123456789012:subnetgroup:analytics-subnets"
	snapshotARN := "arn:aws:redshift:us-east-1:123456789012:snapshot:analytics/rs:analytics-2026-05-20-00"
	kmsKeyARN := "arn:aws:kms:us-east-1:123456789012:key/analytics"
	iamRoleARN := "arn:aws:iam::123456789012:role/redshift-analytics"
	scheduledActionRoleARN := "arn:aws:iam::123456789012:role/redshift-pauser"

	client := fakeClient{
		clusters: []Cluster{{
			ARN:                              clusterARN,
			Identifier:                       "analytics",
			NodeType:                         "ra3.xlplus",
			ClusterStatus:                    "available",
			ClusterAvailabilityStatus:        "Available",
			DBName:                           "analytics",
			Endpoint:                         "analytics.abc123.us-east-1.redshift.amazonaws.com",
			EndpointPort:                     5439,
			HostedZoneID:                     "Z2QHD5K2",
			ClusterCreateTime:                time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
			AutomatedSnapshotRetentionPeriod: 7,
			ManualSnapshotRetentionPeriod:    -1,
			ClusterSecurityGroups:            []string{"legacy-csg"},
			VPCSecurityGroupIDs:              []string{"sg-redshift-1"},
			ClusterParameterGroup:            "analytics-params",
			ClusterSubnetGroupName:           "analytics-subnets",
			VPCID:                            "vpc-redshift",
			AvailabilityZone:                 "us-east-1a",
			PreferredMaintenanceWindow:       "sun:05:00-sun:06:00",
			PendingModifiedValuesPresent:     false,
			ClusterVersion:                   "1.0",
			AllowVersionUpgrade:              true,
			NumberOfNodes:                    4,
			PubliclyAccessible:               false,
			Encrypted:                        true,
			KMSKeyID:                         kmsKeyARN,
			EnhancedVPCRouting:               true,
			IAMRoleARNs:                      []string{iamRoleARN},
			MaintenanceTrackName:             "current",
			MultiAZ:                          true,
			Tags:                             map[string]string{"Environment": "prod"},
		}},
		parameterGroups: []ClusterParameterGroup{{
			ARN:         parameterGroupARN,
			Name:        "analytics-params",
			Family:      "redshift-1.0",
			Description: "analytics workload",
			Tags:        map[string]string{"Tier": "data"},
		}},
		subnetGroups: []ClusterSubnetGroup{{
			ARN:         subnetGroupARN,
			Name:        "analytics-subnets",
			VPCID:       "vpc-redshift",
			Description: "analytics subnets",
			Status:      "Complete",
			SubnetIDs:   []string{"subnet-a", "subnet-b"},
			Tags:        map[string]string{"Network": "private"},
		}},
		snapshots: []ClusterSnapshot{{
			ARN:                           snapshotARN,
			Identifier:                    "rs:analytics-2026-05-20-00",
			ClusterIdentifier:             "analytics",
			SnapshotType:                  "automated",
			Status:                        "available",
			NodeType:                      "ra3.xlplus",
			NumberOfNodes:                 4,
			DBName:                        "analytics",
			VPCID:                         "vpc-redshift",
			Encrypted:                     true,
			KMSKeyID:                      kmsKeyARN,
			SnapshotCreateTime:            time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC),
			SnapshotRetentionStartTime:    time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC),
			ManualSnapshotRetentionPeriod: -1,
			EngineFullVersion:             "1.0.71215",
			AvailabilityZone:              "us-east-1a",
			Tags:                          map[string]string{"Backup": "true"},
		}},
		scheduledActions: []ScheduledAction{{
			Name:                    "pause-analytics-overnight",
			Schedule:                "cron(0 23 * * ? *)",
			IAMRoleARN:              scheduledActionRoleARN,
			Description:             "pause overnight",
			State:                   "ACTIVE",
			StartTime:               time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			NextInvocationTime:      time.Date(2026, 5, 27, 23, 0, 0, 0, time.UTC),
			TargetActionName:        "PauseCluster",
			TargetClusterIdentifier: "analytics",
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	cluster := resourceByID(t, envelopes, awscloud.ResourceTypeRedshiftCluster, clusterARN)
	assertNoForbidden(t, cluster, []string{
		"master_user_password",
		"master_password",
		"password",
		"secret",
		"query_results",
		"table_data",
		"row_data",
		"database_contents",
	})
	clusterAttributes := attributesOf(t, cluster)
	assertAttribute(t, clusterAttributes, "node_type", "ra3.xlplus")
	assertAttribute(t, clusterAttributes, "endpoint_address", "analytics.abc123.us-east-1.redshift.amazonaws.com")
	assertAttribute(t, clusterAttributes, "endpoint_port", int32(5439))
	assertAttribute(t, clusterAttributes, "encrypted", true)
	assertAttribute(t, clusterAttributes, "publicly_accessible", false)
	assertAttribute(t, clusterAttributes, "iam_role_arns", []string{iamRoleARN})
	assertAttribute(t, clusterAttributes, "multi_az", true)
	if got, want := cluster.Payload["state"], "available"; got != want {
		t.Fatalf("cluster state = %#v, want %q", got, want)
	}
	if _, exists := clusterAttributes["master_username"]; exists {
		t.Fatalf("master_username must not be persisted; Redshift scanner is metadata-only")
	}

	parameterGroup := resourceByID(t, envelopes, awscloud.ResourceTypeRedshiftClusterParameterGroup, parameterGroupARN)
	assertAttribute(t, attributesOf(t, parameterGroup), "family", "redshift-1.0")
	subnetGroup := resourceByID(t, envelopes, awscloud.ResourceTypeRedshiftClusterSubnetGroup, subnetGroupARN)
	assertAttribute(t, attributesOf(t, subnetGroup), "subnet_ids", []string{"subnet-a", "subnet-b"})

	snapshot := resourceByID(t, envelopes, awscloud.ResourceTypeRedshiftClusterSnapshot, snapshotARN)
	assertNoForbidden(t, snapshot, []string{
		"snapshot_data",
		"snapshot_contents",
		"data",
		"contents",
		"master_user_password",
		"master_password",
		"password",
		"row_data",
	})
	snapshotAttributes := attributesOf(t, snapshot)
	assertAttribute(t, snapshotAttributes, "cluster_identifier", "analytics")
	assertAttribute(t, snapshotAttributes, "snapshot_type", "automated")
	assertAttribute(t, snapshotAttributes, "encrypted", true)
	assertAttribute(t, snapshotAttributes, "node_type", "ra3.xlplus")

	scheduledAction := resourceByID(t, envelopes, awscloud.ResourceTypeRedshiftScheduledAction, "pause-analytics-overnight")
	scheduledActionAttributes := attributesOf(t, scheduledAction)
	assertAttribute(t, scheduledActionAttributes, "schedule", "cron(0 23 * * ? *)")
	assertAttribute(t, scheduledActionAttributes, "target_action_name", "PauseCluster")
	assertAttribute(t, scheduledActionAttributes, "target_cluster_identifier", "analytics")

	assertRelationshipTarget(t, envelopes, awscloud.RelationshipRedshiftClusterInSubnetGroup, subnetGroupARN)
	assertRelationshipTarget(t, envelopes, awscloud.RelationshipRedshiftClusterUsesKMSKey, kmsKeyARN)
	assertRelationshipTarget(t, envelopes, awscloud.RelationshipRedshiftClusterUsesIAMRole, iamRoleARN)
	assertRelationshipTarget(t, envelopes, awscloud.RelationshipRedshiftClusterUsesParameterGroup, parameterGroupARN)
	assertRelationshipTarget(t, envelopes, awscloud.RelationshipRedshiftClusterUsesSecurityGroup, "arn:aws:ec2:us-east-1:123456789012:security-group/sg-redshift-1")
	assertRelationshipTarget(t, envelopes, awscloud.RelationshipRedshiftClusterInVPC, "vpc-redshift")
	assertRelationshipTarget(t, envelopes, awscloud.RelationshipRedshiftClusterSubnetGroupInVPC, "vpc-redshift")
	assertRelationshipTarget(t, envelopes, awscloud.RelationshipRedshiftClusterSnapshotOfCluster, clusterARN)
	assertRelationshipTarget(t, envelopes, awscloud.RelationshipRedshiftClusterSnapshotUsesKMSKey, kmsKeyARN)
	assertRelationshipTarget(t, envelopes, awscloud.RelationshipRedshiftScheduledActionTargetsCluster, clusterARN)
	assertRelationshipTarget(t, envelopes, awscloud.RelationshipRedshiftScheduledActionUsesIAMRole, scheduledActionRoleARN)
}

func TestScannerEmitsServerlessRedshiftMetadataAndRelationships(t *testing.T) {
	namespaceARN := "arn:aws:redshift-serverless:us-east-1:123456789012:namespace/analytics-ns"
	workgroupARN := "arn:aws:redshift-serverless:us-east-1:123456789012:workgroup/analytics-wg"
	kmsKeyARN := "arn:aws:kms:us-east-1:123456789012:key/analytics-ns"
	iamRoleARN := "arn:aws:iam::123456789012:role/redshift-serverless"

	client := fakeClient{
		namespaces: []ServerlessNamespace{{
			ARN:            namespaceARN,
			Name:           "analytics-ns",
			NamespaceID:    "ns-abc",
			Status:         "AVAILABLE",
			DBName:         "analytics",
			DefaultIAMRole: iamRoleARN,
			IAMRoleARNs:    []string{iamRoleARN},
			KMSKeyID:       kmsKeyARN,
			LogExports:     []string{"connectionlog", "userlog"},
			CreationDate:   time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
			Tags:           map[string]string{"Owner": "analytics"},
		}},
		workgroups: []ServerlessWorkgroup{{
			ARN:                workgroupARN,
			Name:               "analytics-wg",
			WorkgroupID:        "wg-abc",
			NamespaceName:      "analytics-ns",
			Status:             "AVAILABLE",
			BaseCapacity:       64,
			MaxCapacity:        512,
			EnhancedVPCRouting: true,
			PubliclyAccessible: false,
			ConfigParameters: []ServerlessConfigParameter{{
				Key:   "datestyle",
				Value: "ISO, MDY",
			}},
			SubnetIDs:        []string{"subnet-a", "subnet-b"},
			SecurityGroupIDs: []string{"sg-redshift-wg"},
			EndpointAddress:  "analytics-wg.123456789012.us-east-1.redshift-serverless.amazonaws.com",
			EndpointPort:     5439,
			CreationDate:     time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC),
			Tags:             map[string]string{"Owner": "analytics"},
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	namespace := resourceByID(t, envelopes, awscloud.ResourceTypeRedshiftServerlessNamespace, namespaceARN)
	assertNoForbidden(t, namespace, []string{
		"master_user_password",
		"master_password",
		"password",
		"admin_password",
		"admin_user_password",
		"secret",
		"query_results",
		"row_data",
	})
	namespaceAttributes := attributesOf(t, namespace)
	assertAttribute(t, namespaceAttributes, "db_name", "analytics")
	assertAttribute(t, namespaceAttributes, "iam_role_arns", []string{iamRoleARN})
	assertAttribute(t, namespaceAttributes, "log_exports", []string{"connectionlog", "userlog"})

	workgroup := resourceByID(t, envelopes, awscloud.ResourceTypeRedshiftServerlessWorkgroup, workgroupARN)
	workgroupAttributes := attributesOf(t, workgroup)
	assertAttribute(t, workgroupAttributes, "namespace_name", "analytics-ns")
	assertAttribute(t, workgroupAttributes, "base_capacity", int32(64))
	assertAttribute(t, workgroupAttributes, "max_capacity", int32(512))
	assertAttribute(t, workgroupAttributes, "endpoint_address", "analytics-wg.123456789012.us-east-1.redshift-serverless.amazonaws.com")
	assertAttribute(t, workgroupAttributes, "endpoint_port", int32(5439))

	assertRelationshipTarget(t, envelopes, awscloud.RelationshipRedshiftServerlessWorkgroupInNamespace, namespaceARN)
	assertRelationshipTarget(t, envelopes, awscloud.RelationshipRedshiftServerlessWorkgroupUsesSubnet, "subnet-a")
	assertRelationshipTarget(t, envelopes, awscloud.RelationshipRedshiftServerlessWorkgroupUsesSecurityGroup, "arn:aws:ec2:us-east-1:123456789012:security-group/sg-redshift-wg")
	assertRelationshipTarget(t, envelopes, awscloud.RelationshipRedshiftServerlessNamespaceUsesKMSKey, kmsKeyARN)
	assertRelationshipTarget(t, envelopes, awscloud.RelationshipRedshiftServerlessNamespaceUsesIAMRole, iamRoleARN)
}

func TestScannerSkipsRelationshipsWithoutTargets(t *testing.T) {
	client := fakeClient{
		clusters: []Cluster{{
			ARN:                    "arn:aws:redshift:us-east-1:123456789012:cluster:analytics",
			Identifier:             "analytics",
			ClusterSubnetGroupName: "missing-subnet-group",
			ClusterParameterGroup:  "missing-parameter-group",
			KMSKeyID:               "",
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	for _, relationshipType := range []string{
		awscloud.RelationshipRedshiftClusterInSubnetGroup,
		awscloud.RelationshipRedshiftClusterUsesParameterGroup,
		awscloud.RelationshipRedshiftClusterUsesKMSKey,
	} {
		if got := countRelationships(envelopes, relationshipType); got != 0 {
			t.Fatalf("relationship %q count = %d, want 0 without target identity", relationshipType, got)
		}
	}
}

func TestScannerDoesNotTreatNonARNKMSIdentifierAsARN(t *testing.T) {
	client := fakeClient{
		clusters: []Cluster{{
			ARN:        "arn:aws:redshift:us-east-1:123456789012:cluster:analytics",
			Identifier: "analytics",
			KMSKeyID:   "alias/analytics",
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	relationship := relationshipByType(t, envelopes, awscloud.RelationshipRedshiftClusterUsesKMSKey)
	if got, want := relationship.Payload["target_resource_id"], "alias/analytics"; got != want {
		t.Fatalf("target_resource_id = %#v, want %q", got, want)
	}
	if got := relationship.Payload["target_arn"]; got != "" {
		t.Fatalf("target_arn = %#v, want empty for non-ARN KMS identifier", got)
	}
}

func TestScannerSkipsScheduledActionsWithoutTargetCluster(t *testing.T) {
	client := fakeClient{
		scheduledActions: []ScheduledAction{{
			Name:             "no-target-action",
			Schedule:         "cron(0 23 * * ? *)",
			TargetActionName: "PauseCluster",
		}},
	}
	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if got := countRelationships(envelopes, awscloud.RelationshipRedshiftScheduledActionTargetsCluster); got != 0 {
		t.Fatalf("targets_cluster relationship count = %d, want 0 without target cluster", got)
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceRDS

	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func TestScannerDefaultsServiceKindWhenEmpty(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = ""

	envelopes, err := (Scanner{Client: fakeClient{
		clusters: []Cluster{{
			ARN:        "arn:aws:redshift:us-east-1:123456789012:cluster:analytics",
			Identifier: "analytics",
		}},
	}}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if len(envelopes) == 0 {
		t.Fatalf("envelopes = %d, want at least one cluster fact", len(envelopes))
	}
	for _, envelope := range envelopes {
		if got, _ := envelope.Payload["service_kind"].(string); got != awscloud.ServiceRedshift {
			t.Fatalf("envelope service_kind = %q, want %q", got, awscloud.ServiceRedshift)
		}
	}
}

func TestScannerRequiresClient(t *testing.T) {
	_, err := (Scanner{}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want client required error")
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceRedshift,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:redshift:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 27, 18, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	clusters         []Cluster
	parameterGroups  []ClusterParameterGroup
	subnetGroups     []ClusterSubnetGroup
	snapshots        []ClusterSnapshot
	scheduledActions []ScheduledAction
	namespaces       []ServerlessNamespace
	workgroups       []ServerlessWorkgroup
}

func (c fakeClient) ListClusters(context.Context) ([]Cluster, error) {
	return c.clusters, nil
}

func (c fakeClient) ListClusterParameterGroups(context.Context) ([]ClusterParameterGroup, error) {
	return c.parameterGroups, nil
}

func (c fakeClient) ListClusterSubnetGroups(context.Context) ([]ClusterSubnetGroup, error) {
	return c.subnetGroups, nil
}

func (c fakeClient) ListClusterSnapshots(context.Context) ([]ClusterSnapshot, error) {
	return c.snapshots, nil
}

func (c fakeClient) ListScheduledActions(context.Context) ([]ScheduledAction, error) {
	return c.scheduledActions, nil
}

func (c fakeClient) ListServerlessNamespaces(context.Context) ([]ServerlessNamespace, error) {
	return c.namespaces, nil
}

func (c fakeClient) ListServerlessWorkgroups(context.Context) ([]ServerlessWorkgroup, error) {
	return c.workgroups, nil
}

func resourceByID(t *testing.T, envelopes []facts.Envelope, resourceType string, resourceID string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if got, _ := envelope.Payload["resource_type"].(string); got != resourceType {
			continue
		}
		if got, _ := envelope.Payload["resource_id"].(string); got == resourceID {
			return envelope
		}
	}
	t.Fatalf("missing resource %q id %q in %d envelopes", resourceType, resourceID, len(envelopes))
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
	t.Fatalf("missing relationship %q target %q in envelopes", relationshipType, targetID)
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

func assertNoForbidden(t *testing.T, envelope facts.Envelope, forbidden []string) {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		return
	}
	for _, key := range forbidden {
		if _, exists := attributes[key]; exists {
			t.Fatalf("attribute %q persisted; Redshift scanner must stay metadata-only", key)
		}
		// Also catch any attribute key that contains "password" or "secret" substrings unexpectedly.
		for attributeKey := range attributes {
			if strings.Contains(strings.ToLower(attributeKey), "password") {
				t.Fatalf("attribute %q persisted; Redshift scanner must never persist passwords", attributeKey)
			}
		}
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
