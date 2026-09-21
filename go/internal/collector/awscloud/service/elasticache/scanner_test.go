// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package elasticache

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsElastiCacheMetadataOnlyFactsAndRelationships(t *testing.T) {
	clusterARN := "arn:aws:elasticache:us-east-1:123456789012:cluster:orders-cache"
	replicationGroupARN := "arn:aws:elasticache:us-east-1:123456789012:replicationgroup:orders"
	subnetGroupARN := "arn:aws:elasticache:us-east-1:123456789012:subnetgroup:orders-cache"
	parameterGroupARN := "arn:aws:elasticache:us-east-1:123456789012:parametergroup:orders-redis7"
	userARN := "arn:aws:elasticache:us-east-1:123456789012:user:orders-app"
	userGroupARN := "arn:aws:elasticache:us-east-1:123456789012:usergroup:orders-app-group"
	snapshotARN := "arn:aws:elasticache:us-east-1:123456789012:snapshot:orders-2026-05-27"
	kmsKeyARN := "arn:aws:kms:us-east-1:123456789012:key/orders"

	snapshot := SnapshotMetadata{
		ARN:                  snapshotARN,
		Name:                 "orders-2026-05-27",
		Status:               "available",
		SnapshotSource:       "manual",
		SourceCacheClusterID: "orders-cache-001",
		SourceReplicationGrp: "orders",
		Tags:                 map[string]string{"Environment": "prod"},
	}

	client := fakeClient{
		clusters: []CacheCluster{{
			ARN:                       clusterARN,
			ID:                        "orders-cache-001",
			Engine:                    "redis",
			EngineVersion:             "7.1",
			Status:                    "available",
			NodeType:                  "cache.r7g.large",
			NumCacheNodes:             1,
			PreferredAvailabilityZone: "us-east-1a",
			SubnetGroupName:           "orders-cache",
			VPCID:                     "vpc-123",
			SubnetIDs:                 []string{"subnet-a", "subnet-b"},
			SecurityGroupIDs:          []string{"sg-123"},
			ParameterGroupName:        "orders-redis7",
			ReplicationGroupID:        "orders",
			KMSKeyID:                  kmsKeyARN,
			TransitEncryptionEnabled:  true,
			AtRestEncryptionEnabled:   true,
			AuthTokenEnabled:          true,
			SnapshotRetentionLimit:    7,
			SnapshotWindow:            "05:00-06:00",
			AutoMinorVersionUpgrade:   true,
			NotificationTopicARN:      "arn:aws:sns:us-east-1:123456789012:elasticache-events",
			NetworkType:               "ipv4",
			IPDiscovery:               "ipv4",
			Tags:                      map[string]string{"Environment": "prod"},
		}},
		replicationGroups: []ReplicationGroup{{
			ARN:                      replicationGroupARN,
			ID:                       "orders",
			Description:              "orders redis cluster",
			Status:                   "available",
			MemberClusters:           []string{"orders-cache-001"},
			AutomaticFailover:        "enabled",
			MultiAZ:                  "enabled",
			ClusterEnabled:           true,
			NodeType:                 "cache.r7g.large",
			KMSKeyID:                 kmsKeyARN,
			TransitEncryptionEnabled: true,
			AtRestEncryptionEnabled:  true,
			AuthTokenEnabled:         true,
			SnapshotRetentionLimit:   7,
			SnapshotWindow:           "05:00-06:00",
			DataTiering:              "disabled",
			NetworkType:              "ipv4",
			IPDiscovery:              "ipv4",
			Tags:                     map[string]string{"Environment": "prod"},
		}},
		subnetGroups: []SubnetGroup{{
			ARN:         subnetGroupARN,
			Name:        "orders-cache",
			Description: "orders cache subnets",
			VPCID:       "vpc-123",
			SubnetIDs:   []string{"subnet-a", "subnet-b"},
			Tags:        map[string]string{"Network": "private"},
		}},
		parameterGroups: []ParameterGroup{{
			ARN:         parameterGroupARN,
			Name:        "orders-redis7",
			Family:      "redis7",
			Description: "orders redis 7 params",
			IsGlobal:    false,
			Tags:        map[string]string{"Environment": "prod"},
		}},
		users: []User{{
			ARN:                  userARN,
			ID:                   "orders-app",
			Name:                 "orders-app",
			Engine:               "redis",
			Status:               "active",
			AuthenticationType:   "password",
			PasswordCount:        2,
			MinimumEngineVersion: "6.0",
			UserGroupIDs:         []string{"orders-app-group"},
			Tags:                 map[string]string{"Environment": "prod"},
		}},
		userGroups: []UserGroup{{
			ARN:     userGroupARN,
			ID:      "orders-app-group",
			Engine:  "redis",
			Status:  "active",
			UserIDs: []string{"orders-app"},
			Tags:    map[string]string{"Environment": "prod"},
		}},
		snapshots: []SnapshotMetadata{snapshot},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	cluster := resourceByType(t, envelopes, awscloud.ResourceTypeElastiCacheCacheCluster)
	if got, want := cluster.Payload["arn"], clusterARN; got != want {
		t.Fatalf("cluster arn = %#v, want %q", got, want)
	}
	if got, want := cluster.Payload["state"], "available"; got != want {
		t.Fatalf("cluster state = %#v, want %q", got, want)
	}
	clusterAttributes := attributesOf(t, cluster)
	assertAttribute(t, clusterAttributes, "engine", "redis")
	assertAttribute(t, clusterAttributes, "engine_version", "7.1")
	assertAttribute(t, clusterAttributes, "node_type", "cache.r7g.large")
	assertAttribute(t, clusterAttributes, "transit_encryption_enabled", true)
	assertAttribute(t, clusterAttributes, "at_rest_encryption_enabled", true)
	assertAttribute(t, clusterAttributes, "auth_token_enabled", true)
	assertAttribute(t, clusterAttributes, "cache_subnet_group_name", "orders-cache")
	assertAttribute(t, clusterAttributes, "replication_group_id", "orders")
	for _, forbidden := range []string{
		"auth_token",
		"auth_password",
		"password",
		"cache_data",
		"cache_keys",
		"cache_values",
		"snapshot_data",
		"node_snapshots",
	} {
		if _, exists := clusterAttributes[forbidden]; exists {
			t.Fatalf("%s attribute persisted; ElastiCache scanner must stay metadata-only", forbidden)
		}
	}

	replicationGroup := resourceByType(t, envelopes, awscloud.ResourceTypeElastiCacheReplicationGroup)
	if got, want := replicationGroup.Payload["arn"], replicationGroupARN; got != want {
		t.Fatalf("replication group arn = %#v, want %q", got, want)
	}
	replicationGroupAttributes := attributesOf(t, replicationGroup)
	assertAttribute(t, replicationGroupAttributes, "automatic_failover", "enabled")
	assertAttribute(t, replicationGroupAttributes, "multi_az", "enabled")
	assertAttribute(t, replicationGroupAttributes, "auth_token_enabled", true)
	for _, forbidden := range []string{"auth_token", "auth_password", "password"} {
		if _, exists := replicationGroupAttributes[forbidden]; exists {
			t.Fatalf("%s attribute persisted on replication group", forbidden)
		}
	}

	subnetGroup := resourceByType(t, envelopes, awscloud.ResourceTypeElastiCacheSubnetGroup)
	subnetGroupAttributes := attributesOf(t, subnetGroup)
	assertAttribute(t, subnetGroupAttributes, "vpc_id", "vpc-123")
	assertAttribute(t, subnetGroupAttributes, "subnet_ids", []string{"subnet-a", "subnet-b"})

	parameterGroup := resourceByType(t, envelopes, awscloud.ResourceTypeElastiCacheParameterGroup)
	parameterGroupAttributes := attributesOf(t, parameterGroup)
	assertAttribute(t, parameterGroupAttributes, "family", "redis7")
	assertAttribute(t, parameterGroupAttributes, "description", "orders redis 7 params")

	user := resourceByType(t, envelopes, awscloud.ResourceTypeElastiCacheUser)
	userAttributes := attributesOf(t, user)
	assertAttribute(t, userAttributes, "engine", "redis")
	assertAttribute(t, userAttributes, "authentication_type", "password")
	assertAttribute(t, userAttributes, "password_count", int32(2))
	for _, forbidden := range []string{
		"access_string",
		"passwords",
		"password",
		"auth_password",
		"auth_token",
	} {
		if _, exists := userAttributes[forbidden]; exists {
			t.Fatalf("%s attribute persisted on user; ElastiCache User must redact Passwords/AccessString", forbidden)
		}
	}

	userGroup := resourceByType(t, envelopes, awscloud.ResourceTypeElastiCacheUserGroup)
	userGroupAttributes := attributesOf(t, userGroup)
	assertAttribute(t, userGroupAttributes, "engine", "redis")
	assertAttribute(t, userGroupAttributes, "user_ids", []string{"orders-app"})

	snapshotResource := resourceByType(t, envelopes, awscloud.ResourceTypeElastiCacheSnapshot)
	if got, want := snapshotResource.Payload["name"], "orders-2026-05-27"; got != want {
		t.Fatalf("snapshot name = %#v, want %q", got, want)
	}
	if got, want := snapshotResource.Payload["state"], "available"; got != want {
		t.Fatalf("snapshot state = %#v, want %q", got, want)
	}
	snapshotAttributes := attributesOf(t, snapshotResource)
	assertAttribute(t, snapshotAttributes, "snapshot_source", "manual")
	for _, forbidden := range []string{
		"node_snapshots",
		"cache_node_id",
		"snapshot_data",
		"engine_version",
		"port",
		"num_cache_nodes",
		"kms_key_id",
		"auth_token",
		"snapshot_window",
		"snapshot_retention_limit",
	} {
		if _, exists := snapshotAttributes[forbidden]; exists {
			t.Fatalf("%s attribute persisted on snapshot; ElastiCache snapshot must stay name/source/status only", forbidden)
		}
	}

	assertRelationshipTarget(t, envelopes, awscloud.RelationshipElastiCacheClusterInVPC, "vpc-123")
	assertRelationshipTargetARNEmpty(t, envelopes, awscloud.RelationshipElastiCacheClusterInVPC)
	assertRelationshipTarget(t, envelopes, awscloud.RelationshipElastiCacheClusterInSubnet, "subnet-a")
	assertRelationshipTarget(t, envelopes, awscloud.RelationshipElastiCacheClusterInSubnet, "subnet-b")
	assertRelationshipTargetARNEmpty(t, envelopes, awscloud.RelationshipElastiCacheClusterInSubnet)
	assertRelationshipTarget(t, envelopes, awscloud.RelationshipElastiCacheClusterUsesKMSKey, kmsKeyARN)
	assertRelationshipTarget(t, envelopes, awscloud.RelationshipElastiCacheReplicationGroupHasCluster, clusterARN)
	assertRelationshipTargetAttribute(t, envelopes, awscloud.RelationshipElastiCacheReplicationGroupHasCluster, "cache_cluster_id", "orders-cache-001")
	assertRelationshipTarget(t, envelopes, awscloud.RelationshipElastiCacheUserGroupHasUser, userARN)
	assertRelationshipTargetAttribute(t, envelopes, awscloud.RelationshipElastiCacheUserGroupHasUser, "user_id", "orders-app")
	// SourceRecordIDs incorporate the relationship type so a source with
	// multiple edges to the same target stays distinct in the envelope source
	// ref (matches the RDS scanner pattern).
	assertRelationshipSourceRecordID(t, envelopes, awscloud.RelationshipElastiCacheClusterInVPC, clusterARN+"->elasticache_cluster_in_vpc:vpc-123")
	assertRelationshipSourceRecordID(t, envelopes, awscloud.RelationshipElastiCacheReplicationGroupHasCluster, replicationGroupARN+"->elasticache_replication_group_has_cluster:"+clusterARN)
	assertRelationshipSourceRecordID(t, envelopes, awscloud.RelationshipElastiCacheUserGroupHasUser, userGroupARN+"->elasticache_user_group_has_user:"+userARN)
}

func TestScannerSkipsRelationshipsWithoutTargets(t *testing.T) {
	client := fakeClient{clusters: []CacheCluster{{
		ARN:             "arn:aws:elasticache:us-east-1:123456789012:cluster:orders",
		ID:              "orders",
		SubnetGroupName: "missing-subnet-group",
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
	client := fakeClient{clusters: []CacheCluster{{
		ARN:      "arn:aws:elasticache:us-east-1:123456789012:cluster:orders",
		ID:       "orders",
		KMSKeyID: "alias/orders",
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	relationship := relationshipByType(t, envelopes, awscloud.RelationshipElastiCacheClusterUsesKMSKey)
	if got, want := relationship.Payload["target_resource_id"], "alias/orders"; got != want {
		t.Fatalf("target_resource_id = %#v, want %q", got, want)
	}
	if got := relationship.Payload["target_arn"]; got != "" {
		t.Fatalf("target_arn = %#v, want empty for non-ARN KMS identifier", got)
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

	envelopes, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if len(envelopes) != 0 {
		t.Fatalf("envelopes = %d, want 0 for empty input", len(envelopes))
	}
}

func TestScannerRequiresClient(t *testing.T) {
	_, err := (Scanner{Client: nil}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want client required")
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceElastiCache,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:elasticache:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 27, 18, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	clusters          []CacheCluster
	replicationGroups []ReplicationGroup
	subnetGroups      []SubnetGroup
	parameterGroups   []ParameterGroup
	users             []User
	userGroups        []UserGroup
	snapshots         []SnapshotMetadata
}

func (c fakeClient) ListCacheClusters(context.Context) ([]CacheCluster, error) {
	return c.clusters, nil
}

func (c fakeClient) ListReplicationGroups(context.Context) ([]ReplicationGroup, error) {
	return c.replicationGroups, nil
}

func (c fakeClient) ListCacheSubnetGroups(context.Context) ([]SubnetGroup, error) {
	return c.subnetGroups, nil
}

func (c fakeClient) ListCacheParameterGroups(context.Context) ([]ParameterGroup, error) {
	return c.parameterGroups, nil
}

func (c fakeClient) ListUsers(context.Context) ([]User, error) {
	return c.users, nil
}

func (c fakeClient) ListUserGroups(context.Context) ([]UserGroup, error) {
	return c.userGroups, nil
}

func (c fakeClient) ListSnapshots(context.Context) ([]SnapshotMetadata, error) {
	return c.snapshots, nil
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

func assertRelationshipTargetARNEmpty(
	t *testing.T,
	envelopes []facts.Envelope,
	relationshipType string,
) {
	t.Helper()
	found := false
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got != relationshipType {
			continue
		}
		found = true
		if got, _ := envelope.Payload["target_arn"].(string); got != "" {
			t.Fatalf("relationship %q target_arn = %q, want empty (AWS does not report ARN-shaped target identity)", relationshipType, got)
		}
	}
	if !found {
		t.Fatalf("no %q relationship found in envelopes", relationshipType)
	}
}

func assertRelationshipSourceRecordID(
	t *testing.T,
	envelopes []facts.Envelope,
	relationshipType string,
	want string,
) {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got != relationshipType {
			continue
		}
		if envelope.SourceRef.SourceRecordID == want {
			return
		}
	}
	t.Fatalf("relationship %q SourceRecordID %q not found", relationshipType, want)
}

func assertRelationshipTargetAttribute(
	t *testing.T,
	envelopes []facts.Envelope,
	relationshipType string,
	key string,
	want string,
) {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got != relationshipType {
			continue
		}
		attrs, ok := envelope.Payload["attributes"].(map[string]any)
		if !ok {
			continue
		}
		if got, _ := attrs[key].(string); got == want {
			return
		}
	}
	t.Fatalf("missing relationship %q with attribute %s=%q", relationshipType, key, want)
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
