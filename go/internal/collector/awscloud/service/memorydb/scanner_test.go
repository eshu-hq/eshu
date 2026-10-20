// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package memorydb

import (
	"context"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestScannerEmitsMemoryDBMetadataOnlyFactsAndRelationships(t *testing.T) {
	clusterARN := "arn:aws:memorydb:us-east-1:123456789012:cluster/orders-cache"
	subnetGroupARN := "arn:aws:memorydb:us-east-1:123456789012:subnetgroup/orders-cache"
	parameterGroupARN := "arn:aws:memorydb:us-east-1:123456789012:parametergroup/orders-redis7"
	userARN := "arn:aws:memorydb:us-east-1:123456789012:user/orders-app"
	aclARN := "arn:aws:memorydb:us-east-1:123456789012:acl/orders-app-acl"
	snapshotARN := "arn:aws:memorydb:us-east-1:123456789012:snapshot/orders-2026-05-27"
	kmsKeyARN := "arn:aws:kms:us-east-1:123456789012:key/orders"
	snsTopicARN := "arn:aws:sns:us-east-1:123456789012:memorydb-events"

	client := fakeClient{
		clusters: []Cluster{{
			ARN:                      clusterARN,
			Name:                     "orders-cache",
			Description:              "orders memorydb cluster",
			Status:                   "available",
			Engine:                   "redis",
			EngineVersion:            "7.1",
			NodeType:                 "db.r7g.large",
			NumberOfShards:           2,
			NumberOfReplicasPerShard: 1,
			ACLName:                  "orders-app-acl",
			ParameterGroupName:       "orders-redis7",
			SubnetGroupName:          "orders-cache",
			SecurityGroupIDs:         []string{"sg-123"},
			KMSKeyID:                 kmsKeyARN,
			SNSTopicARN:              snsTopicARN,
			TLSEnabled:               true,
			DataTiering:              "false",
			AutoMinorVersionUpgrade:  true,
			SnapshotRetentionLimit:   7,
			SnapshotWindow:           "05:00-06:00",
			MaintenanceWindow:        "sun:05:00-sun:06:00",
			AvailabilityMode:         "multiaz",
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
			Family:      "memorydb_redis7",
			Description: "orders redis 7 params",
			Tags:        map[string]string{"Environment": "prod"},
		}},
		users: []User{{
			ARN:                  userARN,
			Name:                 "orders-app",
			Status:               "active",
			AuthenticationType:   "password",
			PasswordCount:        2,
			AccessStringPresent:  true,
			MinimumEngineVersion: "6.0",
			ACLNames:             []string{"orders-app-acl"},
			Tags:                 map[string]string{"Environment": "prod"},
		}},
		acls: []ACL{{
			ARN:                  aclARN,
			Name:                 "orders-app-acl",
			Status:               "active",
			MinimumEngineVersion: "6.0",
			UserNames:            []string{"orders-app"},
			ClusterNames:         []string{"orders-cache"},
			Tags:                 map[string]string{"Environment": "prod"},
		}},
		snapshots: []SnapshotMetadata{{
			ARN:               snapshotARN,
			Name:              "orders-2026-05-27",
			Status:            "available",
			Source:            "manual",
			SourceClusterName: "orders-cache",
			Tags:              map[string]string{"Environment": "prod"},
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	cluster := resourceByType(t, envelopes, awscloud.ResourceTypeMemoryDBCluster)
	if got, want := cluster.Payload["arn"], clusterARN; got != want {
		t.Fatalf("cluster arn = %#v, want %q", got, want)
	}
	if got, want := cluster.Payload["state"], "available"; got != want {
		t.Fatalf("cluster state = %#v, want %q", got, want)
	}
	clusterAttributes := attributesOf(t, cluster)
	assertAttribute(t, clusterAttributes, "engine", "redis")
	assertAttribute(t, clusterAttributes, "engine_version", "7.1")
	assertAttribute(t, clusterAttributes, "node_type", "db.r7g.large")
	assertAttribute(t, clusterAttributes, "num_shards", int32(2))
	assertAttribute(t, clusterAttributes, "num_replicas_per_shard", int32(1))
	assertAttribute(t, clusterAttributes, "acl_name", "orders-app-acl")
	assertAttribute(t, clusterAttributes, "tls_enabled", true)
	assertAttribute(t, clusterAttributes, "subnet_group_name", "orders-cache")
	assertAttribute(t, clusterAttributes, "kms_key_id", kmsKeyARN)
	assertAttribute(t, clusterAttributes, "sns_topic_arn", snsTopicARN)
	for _, forbidden := range []string{
		"auth_token",
		"auth_password",
		"password",
		"passwords",
		"access_string",
		"cache_data",
		"cache_keys",
		"cache_values",
		"snapshot_data",
	} {
		if _, exists := clusterAttributes[forbidden]; exists {
			t.Fatalf("%s attribute persisted; MemoryDB scanner must stay metadata-only", forbidden)
		}
	}

	subnetGroup := resourceByType(t, envelopes, awscloud.ResourceTypeMemoryDBSubnetGroup)
	subnetGroupAttributes := attributesOf(t, subnetGroup)
	assertAttribute(t, subnetGroupAttributes, "vpc_id", "vpc-123")
	assertAttribute(t, subnetGroupAttributes, "subnet_ids", []string{"subnet-a", "subnet-b"})

	parameterGroup := resourceByType(t, envelopes, awscloud.ResourceTypeMemoryDBParameterGroup)
	parameterGroupAttributes := attributesOf(t, parameterGroup)
	assertAttribute(t, parameterGroupAttributes, "family", "memorydb_redis7")
	assertAttribute(t, parameterGroupAttributes, "description", "orders redis 7 params")
	// The parameter-group fact shape persists family, description, and tags
	// (parameter values are excluded by design). Lock the tag emission so the
	// documented contract and the emitted fact stay in agreement.
	if got, want := parameterGroup.Payload["tags"], map[string]string{"Environment": "prod"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("parameter group tags = %#v, want %#v", got, want)
	}

	user := resourceByType(t, envelopes, awscloud.ResourceTypeMemoryDBUser)
	userAttributes := attributesOf(t, user)
	assertAttribute(t, userAttributes, "authentication_type", "password")
	assertAttribute(t, userAttributes, "password_count", int32(2))
	assertAttribute(t, userAttributes, "access_string_present", true)
	for _, forbidden := range []string{
		"access_string",
		"passwords",
		"password",
		"auth_password",
		"auth_token",
	} {
		if _, exists := userAttributes[forbidden]; exists {
			t.Fatalf("%s attribute persisted on user; MemoryDB User must redact Passwords/AccessString", forbidden)
		}
	}

	acl := resourceByType(t, envelopes, awscloud.ResourceTypeMemoryDBACL)
	aclAttributes := attributesOf(t, acl)
	assertAttribute(t, aclAttributes, "user_names", []string{"orders-app"})
	assertAttribute(t, aclAttributes, "minimum_engine_version", "6.0")
	for _, forbidden := range []string{"access_string", "passwords", "password"} {
		if _, exists := aclAttributes[forbidden]; exists {
			t.Fatalf("%s attribute persisted on ACL; MemoryDB ACL must stay metadata-only", forbidden)
		}
	}

	snapshotResource := resourceByType(t, envelopes, awscloud.ResourceTypeMemoryDBSnapshot)
	if got, want := snapshotResource.Payload["name"], "orders-2026-05-27"; got != want {
		t.Fatalf("snapshot name = %#v, want %q", got, want)
	}
	if got, want := snapshotResource.Payload["state"], "available"; got != want {
		t.Fatalf("snapshot state = %#v, want %q", got, want)
	}
	snapshotAttributes := attributesOf(t, snapshotResource)
	assertAttribute(t, snapshotAttributes, "snapshot_source", "manual")
	assertAttribute(t, snapshotAttributes, "source_cluster_name", "orders-cache")
	for _, forbidden := range []string{
		"cluster_configuration",
		"shards",
		"shard_detail",
		"snapshot_data",
		"engine_version",
		"node_type",
		"kms_key_id",
		"snapshot_window",
		"snapshot_retention_limit",
		"auth_token",
	} {
		if _, exists := snapshotAttributes[forbidden]; exists {
			t.Fatalf("%s attribute persisted on snapshot; MemoryDB snapshot must stay name/source/status only", forbidden)
		}
	}

	assertRelationshipTarget(t, envelopes, awscloud.RelationshipMemoryDBClusterInSubnetGroup, subnetGroupARN)
	assertRelationshipTargetAttribute(t, envelopes, awscloud.RelationshipMemoryDBClusterInSubnetGroup, "subnet_group_name", "orders-cache")
	assertRelationshipTarget(t, envelopes, awscloud.RelationshipMemoryDBClusterUsesKMSKey, kmsKeyARN)
	assertRelationshipTarget(t, envelopes, awscloud.RelationshipMemoryDBClusterNotifiesSNSTopic, snsTopicARN)
	assertRelationshipTarget(t, envelopes, awscloud.RelationshipMemoryDBACLHasUser, userARN)
	assertRelationshipTargetAttribute(t, envelopes, awscloud.RelationshipMemoryDBACLHasUser, "user_name", "orders-app")
	// SourceRecordIDs incorporate the relationship type so a source with
	// multiple edges to the same target stays distinct in the envelope source
	// ref (matches the ElastiCache scanner pattern).
	assertRelationshipSourceRecordID(t, envelopes, awscloud.RelationshipMemoryDBClusterInSubnetGroup, clusterARN+"->memorydb_cluster_in_subnet_group:"+subnetGroupARN)
	assertRelationshipSourceRecordID(t, envelopes, awscloud.RelationshipMemoryDBClusterNotifiesSNSTopic, clusterARN+"->memorydb_cluster_notifies_sns_topic:"+snsTopicARN)
	assertRelationshipSourceRecordID(t, envelopes, awscloud.RelationshipMemoryDBACLHasUser, aclARN+"->memorydb_acl_has_user:"+userARN)
}

func TestScannerSkipsRelationshipsWithoutTargets(t *testing.T) {
	client := fakeClient{clusters: []Cluster{{
		ARN:             "arn:aws:memorydb:us-east-1:123456789012:cluster/orders",
		Name:            "orders",
		SubnetGroupName: "",
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
	client := fakeClient{clusters: []Cluster{{
		ARN:      "arn:aws:memorydb:us-east-1:123456789012:cluster/orders",
		Name:     "orders",
		KMSKeyID: "alias/orders",
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	relationship := relationshipByType(t, envelopes, awscloud.RelationshipMemoryDBClusterUsesKMSKey)
	if got, want := relationship.Payload["target_resource_id"], "alias/orders"; got != want {
		t.Fatalf("target_resource_id = %#v, want %q", got, want)
	}
	if got := relationship.Payload["target_arn"]; got != "" {
		t.Fatalf("target_arn = %#v, want empty for non-ARN KMS identifier", got)
	}
}

func TestScannerUpgradesSubnetGroupTargetToARN(t *testing.T) {
	subnetGroupARN := "arn:aws:memorydb:us-east-1:123456789012:subnetgroup/orders-cache"
	client := fakeClient{
		clusters: []Cluster{{
			ARN:             "arn:aws:memorydb:us-east-1:123456789012:cluster/orders",
			Name:            "orders",
			SubnetGroupName: "orders-cache",
		}},
		subnetGroups: []SubnetGroup{{
			ARN:  subnetGroupARN,
			Name: "orders-cache",
		}},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	relationship := relationshipByType(t, envelopes, awscloud.RelationshipMemoryDBClusterInSubnetGroup)
	if got, want := relationship.Payload["target_arn"], subnetGroupARN; got != want {
		t.Fatalf("target_arn = %#v, want %q (subnet group ARN resolved by name)", got, want)
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
