// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsmemorydb "github.com/aws/aws-sdk-go-v2/service/memorydb"
	awsmemorydbtypes "github.com/aws/aws-sdk-go-v2/service/memorydb/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	memorydbservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/memorydb"
)

// TestAPIClientExcludesMutationAndSecretReads is the exclusion gate for the
// MemoryDB adapter. It runs before behavior tests to guarantee the SDK surface
// the adapter depends on can only describe metadata. The adapter contract must
// never gain a Create/Delete/Update/Modify/Reset/Failover/Purchase/Tag/Untag
// method, and the scanner-owned User type must never carry password material or
// the raw ACL access string.
func TestAPIClientExcludesMutationAndSecretReads(t *testing.T) {
	apiType := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < apiType.NumMethod(); i++ {
		name := apiType.Method(i).Name
		for _, forbidden := range []string{
			"Create", "Delete", "Update", "Modify", "Reset",
			"Failover", "Purchase", "TagResource", "UntagResource",
			"BatchUpdate", "Copy",
		} {
			if strings.HasPrefix(name, forbidden) || name == forbidden {
				t.Fatalf("apiClient exposes mutation method %q; MemoryDB adapter must stay read-only", name)
			}
		}
	}

	userType := reflect.TypeOf(memorydbservice.User{})
	for _, forbidden := range []string{"Passwords", "Password", "AuthToken", "AccessString"} {
		if _, ok := userType.FieldByName(forbidden); ok {
			t.Fatalf("User type exposes %q; MemoryDB adapter must never persist password material or the raw access string", forbidden)
		}
	}

	aclType := reflect.TypeOf(memorydbservice.ACL{})
	for _, forbidden := range []string{"Passwords", "Password", "AccessString", "AuthToken"} {
		if _, ok := aclType.FieldByName(forbidden); ok {
			t.Fatalf("ACL type exposes %q; MemoryDB ACL must stay metadata-only", forbidden)
		}
	}

	snapshotType := reflect.TypeOf(memorydbservice.SnapshotMetadata{})
	for _, forbidden := range []string{"ClusterConfiguration", "Shards", "ShardDetail", "KmsKeyId", "EngineVersion", "AuthToken"} {
		if _, ok := snapshotType.FieldByName(forbidden); ok {
			t.Fatalf("SnapshotMetadata exposes %q; MemoryDB snapshot must persist name/source/status only", forbidden)
		}
	}
}

func TestClientListsMemoryDBMetadataOnly(t *testing.T) {
	clusterARN := "arn:aws:memorydb:us-east-1:123456789012:cluster/orders-cache"
	subnetGroupARN := "arn:aws:memorydb:us-east-1:123456789012:subnetgroup/orders-cache"
	parameterGroupARN := "arn:aws:memorydb:us-east-1:123456789012:parametergroup/orders-redis7"
	userARN := "arn:aws:memorydb:us-east-1:123456789012:user/orders-app"
	aclARN := "arn:aws:memorydb:us-east-1:123456789012:acl/orders-app-acl"
	snapshotARN := "arn:aws:memorydb:us-east-1:123456789012:snapshot/orders-2026-05-27"
	kmsKeyARN := "arn:aws:kms:us-east-1:123456789012:key/orders"
	snsTopicARN := "arn:aws:sns:us-east-1:123456789012:memorydb-events"

	api := &fakeMemoryDBAPI{
		clusterPages: []*awsmemorydb.DescribeClustersOutput{{
			Clusters: []awsmemorydbtypes.Cluster{{
				ARN:                aws.String(clusterARN),
				Name:               aws.String("orders-cache"),
				Description:        aws.String("orders memorydb cluster"),
				Status:             aws.String("available"),
				Engine:             aws.String("redis"),
				EngineVersion:      aws.String("7.1"),
				NodeType:           aws.String("db.r7g.large"),
				NumberOfShards:     aws.Int32(2),
				ACLName:            aws.String("orders-app-acl"),
				ParameterGroupName: aws.String("orders-redis7"),
				SubnetGroupName:    aws.String("orders-cache"),
				SecurityGroups: []awsmemorydbtypes.SecurityGroupMembership{{
					SecurityGroupId: aws.String("sg-123"),
					Status:          aws.String("active"),
				}},
				KmsKeyId:                aws.String(kmsKeyARN),
				SnsTopicArn:             aws.String(snsTopicARN),
				TLSEnabled:              aws.Bool(true),
				DataTiering:             awsmemorydbtypes.DataTieringStatusFalse,
				AutoMinorVersionUpgrade: aws.Bool(true),
				SnapshotRetentionLimit:  aws.Int32(7),
				SnapshotWindow:          aws.String("05:00-06:00"),
				MaintenanceWindow:       aws.String("sun:05:00-sun:06:00"),
				AvailabilityMode:        awsmemorydbtypes.AZStatusMultiAZ,
				NetworkType:             awsmemorydbtypes.NetworkTypeIpv4,
				IpDiscovery:             awsmemorydbtypes.IpDiscoveryIpv4,
				Shards: []awsmemorydbtypes.Shard{{
					Name:          aws.String("0001"),
					NumberOfNodes: aws.Int32(2),
				}},
			}},
		}},
		subnetGroupPages: []*awsmemorydb.DescribeSubnetGroupsOutput{{
			SubnetGroups: []awsmemorydbtypes.SubnetGroup{{
				ARN:         aws.String(subnetGroupARN),
				Name:        aws.String("orders-cache"),
				Description: aws.String("orders cache subnets"),
				VpcId:       aws.String("vpc-123"),
				Subnets: []awsmemorydbtypes.Subnet{{
					Identifier: aws.String("subnet-a"),
				}, {
					Identifier: aws.String("subnet-b"),
				}},
			}},
		}},
		parameterGroupPages: []*awsmemorydb.DescribeParameterGroupsOutput{{
			ParameterGroups: []awsmemorydbtypes.ParameterGroup{{
				ARN:         aws.String(parameterGroupARN),
				Name:        aws.String("orders-redis7"),
				Family:      aws.String("memorydb_redis7"),
				Description: aws.String("orders redis 7 params"),
			}},
		}},
		userPages: []*awsmemorydb.DescribeUsersOutput{{
			Users: []awsmemorydbtypes.User{{
				ARN:                  aws.String(userARN),
				Name:                 aws.String("orders-app"),
				Status:               aws.String("active"),
				MinimumEngineVersion: aws.String("6.0"),
				AccessString:         aws.String("on ~* +@all"),
				Authentication: &awsmemorydbtypes.Authentication{
					Type:          awsmemorydbtypes.AuthenticationTypePassword,
					PasswordCount: aws.Int32(2),
				},
				ACLNames: []string{"orders-app-acl"},
			}},
		}},
		aclPages: []*awsmemorydb.DescribeACLsOutput{{
			ACLs: []awsmemorydbtypes.ACL{{
				ARN:                  aws.String(aclARN),
				Name:                 aws.String("orders-app-acl"),
				Status:               aws.String("active"),
				MinimumEngineVersion: aws.String("6.0"),
				UserNames:            []string{"orders-app"},
				Clusters:             []string{"orders-cache"},
			}},
		}},
		snapshotPages: []*awsmemorydb.DescribeSnapshotsOutput{{
			Snapshots: []awsmemorydbtypes.Snapshot{{
				ARN:    aws.String(snapshotARN),
				Name:   aws.String("orders-2026-05-27"),
				Status: aws.String("available"),
				Source: aws.String("manual"),
				ClusterConfiguration: &awsmemorydbtypes.ClusterConfiguration{
					Name:          aws.String("orders-cache"),
					EngineVersion: aws.String("7.1"),
					NodeType:      aws.String("db.r7g.large"),
				},
				KmsKeyId: aws.String(kmsKeyARN),
			}},
		}},
		tags: map[string][]awsmemorydbtypes.Tag{
			clusterARN:        {{Key: aws.String("Environment"), Value: aws.String("prod")}},
			subnetGroupARN:    {{Key: aws.String("Network"), Value: aws.String("private")}},
			parameterGroupARN: {{Key: aws.String("Environment"), Value: aws.String("prod")}},
			userARN:           {{Key: aws.String("Environment"), Value: aws.String("prod")}},
			aclARN:            {{Key: aws.String("Environment"), Value: aws.String("prod")}},
			snapshotARN:       {{Key: aws.String("Environment"), Value: aws.String("prod")}},
		},
	}
	adapter := &Client{client: api, boundary: testBoundary()}

	clusters, err := adapter.ListClusters(context.Background())
	if err != nil {
		t.Fatalf("ListClusters() error = %v", err)
	}
	if got, want := len(clusters), 1; got != want {
		t.Fatalf("len(clusters) = %d, want %d", got, want)
	}
	cluster := clusters[0]
	if cluster.Name != "orders-cache" {
		t.Fatalf("cluster.Name = %q, want orders-cache", cluster.Name)
	}
	if cluster.NumberOfShards != 2 {
		t.Fatalf("cluster.NumberOfShards = %d, want 2", cluster.NumberOfShards)
	}
	if cluster.NumberOfReplicasPerShard != 1 {
		t.Fatalf("cluster.NumberOfReplicasPerShard = %d, want 1 (nodes per shard minus primary)", cluster.NumberOfReplicasPerShard)
	}
	if cluster.ACLName != "orders-app-acl" {
		t.Fatalf("cluster.ACLName = %q, want orders-app-acl", cluster.ACLName)
	}
	if !cluster.TLSEnabled {
		t.Fatalf("cluster.TLSEnabled = false, want true")
	}
	if cluster.KMSKeyID != kmsKeyARN {
		t.Fatalf("cluster.KMSKeyID = %q, want %q", cluster.KMSKeyID, kmsKeyARN)
	}
	if cluster.SNSTopicARN != snsTopicARN {
		t.Fatalf("cluster.SNSTopicARN = %q, want %q", cluster.SNSTopicARN, snsTopicARN)
	}
	if got, want := cluster.SecurityGroupIDs, []string{"sg-123"}; !stringSlicesEqual(got, want) {
		t.Fatalf("cluster.SecurityGroupIDs = %#v, want %#v", got, want)
	}
	if cluster.Tags["Environment"] != "prod" {
		t.Fatalf("cluster.Tags = %#v, want Environment=prod", cluster.Tags)
	}

	subnetGroups, err := adapter.ListSubnetGroups(context.Background())
	if err != nil {
		t.Fatalf("ListSubnetGroups() error = %v", err)
	}
	if subnetGroups[0].VPCID != "vpc-123" {
		t.Fatalf("subnetGroups[0].VPCID = %q, want vpc-123", subnetGroups[0].VPCID)
	}
	if got, want := subnetGroups[0].SubnetIDs, []string{"subnet-a", "subnet-b"}; !stringSlicesEqual(got, want) {
		t.Fatalf("subnetGroups[0].SubnetIDs = %#v, want %#v", got, want)
	}

	parameterGroups, err := adapter.ListParameterGroups(context.Background())
	if err != nil {
		t.Fatalf("ListParameterGroups() error = %v", err)
	}
	if parameterGroups[0].Family != "memorydb_redis7" {
		t.Fatalf("parameterGroups[0].Family = %q, want memorydb_redis7", parameterGroups[0].Family)
	}

	users, err := adapter.ListUsers(context.Background())
	if err != nil {
		t.Fatalf("ListUsers() error = %v", err)
	}
	if got, want := len(users), 1; got != want {
		t.Fatalf("len(users) = %d, want %d", got, want)
	}
	user := users[0]
	if user.PasswordCount != 2 {
		t.Fatalf("user.PasswordCount = %d, want 2", user.PasswordCount)
	}
	if user.AuthenticationType != "password" {
		t.Fatalf("user.AuthenticationType = %q, want password", user.AuthenticationType)
	}
	if !user.AccessStringPresent {
		t.Fatalf("user.AccessStringPresent = false, want true (AWS reported a grant string)")
	}

	acls, err := adapter.ListACLs(context.Background())
	if err != nil {
		t.Fatalf("ListACLs() error = %v", err)
	}
	if got, want := acls[0].UserNames, []string{"orders-app"}; !stringSlicesEqual(got, want) {
		t.Fatalf("acls[0].UserNames = %#v, want %#v", got, want)
	}
	if got, want := acls[0].ClusterNames, []string{"orders-cache"}; !stringSlicesEqual(got, want) {
		t.Fatalf("acls[0].ClusterNames = %#v, want %#v", got, want)
	}

	snapshots, err := adapter.ListSnapshots(context.Background())
	if err != nil {
		t.Fatalf("ListSnapshots() error = %v", err)
	}
	if got, want := len(snapshots), 1; got != want {
		t.Fatalf("len(snapshots) = %d, want %d", got, want)
	}
	snapshot := snapshots[0]
	if snapshot.Name != "orders-2026-05-27" {
		t.Fatalf("snapshot.Name = %q, want orders-2026-05-27", snapshot.Name)
	}
	if snapshot.SourceClusterName != "orders-cache" {
		t.Fatalf("snapshot.SourceClusterName = %q, want orders-cache (from cluster configuration name)", snapshot.SourceClusterName)
	}
	if snapshot.Status != "available" {
		t.Fatalf("snapshot.Status = %q, want available", snapshot.Status)
	}
	if snapshot.Source != "manual" {
		t.Fatalf("snapshot.Source = %q, want manual", snapshot.Source)
	}
}

func TestClientPaginatesClusters(t *testing.T) {
	api := &fakeMemoryDBAPI{
		clusterPages: []*awsmemorydb.DescribeClustersOutput{{
			Clusters:  []awsmemorydbtypes.Cluster{{Name: aws.String("first")}},
			NextToken: aws.String("next"),
		}, {
			Clusters: []awsmemorydbtypes.Cluster{{Name: aws.String("second")}},
		}},
	}
	adapter := &Client{client: api, boundary: testBoundary()}

	clusters, err := adapter.ListClusters(context.Background())
	if err != nil {
		t.Fatalf("ListClusters() error = %v", err)
	}
	if got, want := len(clusters), 2; got != want {
		t.Fatalf("len(clusters) = %d, want %d", got, want)
	}
	if got, want := api.clusterTokens, []string{"", "next"}; !stringSlicesEqual(got, want) {
		t.Fatalf("DescribeClusters tokens = %#v, want %#v", got, want)
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceMemoryDB,
	}
}

type fakeMemoryDBAPI struct {
	clusterPages  []*awsmemorydb.DescribeClustersOutput
	clusterCalls  int
	clusterTokens []string

	subnetGroupPages []*awsmemorydb.DescribeSubnetGroupsOutput
	subnetGroupCalls int

	parameterGroupPages []*awsmemorydb.DescribeParameterGroupsOutput
	parameterGroupCalls int

	userPages []*awsmemorydb.DescribeUsersOutput
	userCalls int

	aclPages []*awsmemorydb.DescribeACLsOutput
	aclCalls int

	snapshotPages []*awsmemorydb.DescribeSnapshotsOutput
	snapshotCalls int

	tags map[string][]awsmemorydbtypes.Tag
}

func (f *fakeMemoryDBAPI) DescribeClusters(
	_ context.Context,
	input *awsmemorydb.DescribeClustersInput,
	_ ...func(*awsmemorydb.Options),
) (*awsmemorydb.DescribeClustersOutput, error) {
	f.clusterTokens = append(f.clusterTokens, aws.ToString(input.NextToken))
	if f.clusterCalls >= len(f.clusterPages) {
		return &awsmemorydb.DescribeClustersOutput{}, nil
	}
	page := f.clusterPages[f.clusterCalls]
	f.clusterCalls++
	return page, nil
}

func (f *fakeMemoryDBAPI) DescribeSubnetGroups(
	_ context.Context,
	_ *awsmemorydb.DescribeSubnetGroupsInput,
	_ ...func(*awsmemorydb.Options),
) (*awsmemorydb.DescribeSubnetGroupsOutput, error) {
	if f.subnetGroupCalls >= len(f.subnetGroupPages) {
		return &awsmemorydb.DescribeSubnetGroupsOutput{}, nil
	}
	page := f.subnetGroupPages[f.subnetGroupCalls]
	f.subnetGroupCalls++
	return page, nil
}

func (f *fakeMemoryDBAPI) DescribeParameterGroups(
	_ context.Context,
	_ *awsmemorydb.DescribeParameterGroupsInput,
	_ ...func(*awsmemorydb.Options),
) (*awsmemorydb.DescribeParameterGroupsOutput, error) {
	if f.parameterGroupCalls >= len(f.parameterGroupPages) {
		return &awsmemorydb.DescribeParameterGroupsOutput{}, nil
	}
	page := f.parameterGroupPages[f.parameterGroupCalls]
	f.parameterGroupCalls++
	return page, nil
}

func (f *fakeMemoryDBAPI) DescribeUsers(
	_ context.Context,
	_ *awsmemorydb.DescribeUsersInput,
	_ ...func(*awsmemorydb.Options),
) (*awsmemorydb.DescribeUsersOutput, error) {
	if f.userCalls >= len(f.userPages) {
		return &awsmemorydb.DescribeUsersOutput{}, nil
	}
	page := f.userPages[f.userCalls]
	f.userCalls++
	return page, nil
}

func (f *fakeMemoryDBAPI) DescribeACLs(
	_ context.Context,
	_ *awsmemorydb.DescribeACLsInput,
	_ ...func(*awsmemorydb.Options),
) (*awsmemorydb.DescribeACLsOutput, error) {
	if f.aclCalls >= len(f.aclPages) {
		return &awsmemorydb.DescribeACLsOutput{}, nil
	}
	page := f.aclPages[f.aclCalls]
	f.aclCalls++
	return page, nil
}

func (f *fakeMemoryDBAPI) DescribeSnapshots(
	_ context.Context,
	_ *awsmemorydb.DescribeSnapshotsInput,
	_ ...func(*awsmemorydb.Options),
) (*awsmemorydb.DescribeSnapshotsOutput, error) {
	if f.snapshotCalls >= len(f.snapshotPages) {
		return &awsmemorydb.DescribeSnapshotsOutput{}, nil
	}
	page := f.snapshotPages[f.snapshotCalls]
	f.snapshotCalls++
	return page, nil
}

func (f *fakeMemoryDBAPI) ListTags(
	_ context.Context,
	input *awsmemorydb.ListTagsInput,
	_ ...func(*awsmemorydb.Options),
) (*awsmemorydb.ListTagsOutput, error) {
	if f.tags == nil {
		return &awsmemorydb.ListTagsOutput{}, nil
	}
	tags := f.tags[aws.ToString(input.ResourceArn)]
	return &awsmemorydb.ListTagsOutput{TagList: tags}, nil
}

var _ apiClient = (*fakeMemoryDBAPI)(nil)

var _ memorydbservice.Client = (*Client)(nil)

func stringSlicesEqual(got []string, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
