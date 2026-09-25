// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sdk

import (
	"context"
	"testing"
	"time"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	awselasticache "github.com/aws/aws-sdk-go-v2/service/elasticache"
	awselasticachetypes "github.com/aws/aws-sdk-go-v2/service/elasticache/types"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
	elasticacheservice "github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/service/elasticache"
)

func TestClientListsElastiCacheMetadataOnly(t *testing.T) {
	clusterARN := "arn:aws:elasticache:us-east-1:123456789012:cluster:orders-cache"
	replicationGroupARN := "arn:aws:elasticache:us-east-1:123456789012:replicationgroup:orders"
	subnetGroupARN := "arn:aws:elasticache:us-east-1:123456789012:subnetgroup:orders-cache"
	parameterGroupARN := "arn:aws:elasticache:us-east-1:123456789012:parametergroup:orders-redis7"
	userARN := "arn:aws:elasticache:us-east-1:123456789012:user:orders-app"
	userGroupARN := "arn:aws:elasticache:us-east-1:123456789012:usergroup:orders-app-group"
	snapshotARN := "arn:aws:elasticache:us-east-1:123456789012:snapshot:orders-2026-05-27"
	api := &fakeElastiCacheAPI{
		cacheClusterPages: []*awselasticache.DescribeCacheClustersOutput{{
			CacheClusters: []awselasticachetypes.CacheCluster{{
				ARN:                       awsv2.String(clusterARN),
				CacheClusterId:            awsv2.String("orders-cache-001"),
				Engine:                    awsv2.String("redis"),
				EngineVersion:             awsv2.String("7.1"),
				CacheClusterStatus:        awsv2.String("available"),
				CacheNodeType:             awsv2.String("cache.r7g.large"),
				NumCacheNodes:             awsv2.Int32(1),
				PreferredAvailabilityZone: awsv2.String("us-east-1a"),
				CacheSubnetGroupName:      awsv2.String("orders-cache"),
				CacheParameterGroup: &awselasticachetypes.CacheParameterGroupStatus{
					CacheParameterGroupName: awsv2.String("orders-redis7"),
				},
				SecurityGroups: []awselasticachetypes.SecurityGroupMembership{{
					SecurityGroupId: awsv2.String("sg-123"),
					Status:          awsv2.String("active"),
				}},
				ReplicationGroupId:        awsv2.String("orders"),
				AtRestEncryptionEnabled:   awsv2.Bool(true),
				TransitEncryptionEnabled:  awsv2.Bool(true),
				AuthTokenEnabled:          awsv2.Bool(true),
				AuthTokenLastModifiedDate: awsv2.Time(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
				SnapshotRetentionLimit:    awsv2.Int32(7),
				SnapshotWindow:            awsv2.String("05:00-06:00"),
				AutoMinorVersionUpgrade:   awsv2.Bool(true),
				NotificationConfiguration: &awselasticachetypes.NotificationConfiguration{
					TopicArn: awsv2.String("arn:aws:sns:us-east-1:123456789012:elasticache-events"),
				},
				NetworkType: awselasticachetypes.NetworkTypeIpv4,
				IpDiscovery: awselasticachetypes.IpDiscoveryIpv4,
			}},
		}},
		replicationGroupPages: []*awselasticache.DescribeReplicationGroupsOutput{{
			ReplicationGroups: []awselasticachetypes.ReplicationGroup{{
				ARN:                      awsv2.String(replicationGroupARN),
				ReplicationGroupId:       awsv2.String("orders"),
				Description:              awsv2.String("orders redis cluster"),
				Status:                   awsv2.String("available"),
				MemberClusters:           []string{"orders-cache-001"},
				AutomaticFailover:        awselasticachetypes.AutomaticFailoverStatusEnabled,
				MultiAZ:                  awselasticachetypes.MultiAZStatusEnabled,
				ClusterEnabled:           awsv2.Bool(true),
				CacheNodeType:            awsv2.String("cache.r7g.large"),
				AtRestEncryptionEnabled:  awsv2.Bool(true),
				TransitEncryptionEnabled: awsv2.Bool(true),
				AuthTokenEnabled:         awsv2.Bool(true),
				KmsKeyId:                 awsv2.String("arn:aws:kms:us-east-1:123456789012:key/orders"),
				SnapshotRetentionLimit:   awsv2.Int32(7),
				SnapshotWindow:           awsv2.String("05:00-06:00"),
				DataTiering:              awselasticachetypes.DataTieringStatusDisabled,
				NetworkType:              awselasticachetypes.NetworkTypeIpv4,
				IpDiscovery:              awselasticachetypes.IpDiscoveryIpv4,
			}},
		}},
		subnetGroupPages: []*awselasticache.DescribeCacheSubnetGroupsOutput{{
			CacheSubnetGroups: []awselasticachetypes.CacheSubnetGroup{{
				ARN:                         awsv2.String(subnetGroupARN),
				CacheSubnetGroupName:        awsv2.String("orders-cache"),
				CacheSubnetGroupDescription: awsv2.String("orders cache subnets"),
				VpcId:                       awsv2.String("vpc-123"),
				Subnets: []awselasticachetypes.Subnet{{
					SubnetIdentifier: awsv2.String("subnet-a"),
				}, {
					SubnetIdentifier: awsv2.String("subnet-b"),
				}},
			}},
		}},
		parameterGroupPages: []*awselasticache.DescribeCacheParameterGroupsOutput{{
			CacheParameterGroups: []awselasticachetypes.CacheParameterGroup{{
				ARN:                       awsv2.String(parameterGroupARN),
				CacheParameterGroupName:   awsv2.String("orders-redis7"),
				CacheParameterGroupFamily: awsv2.String("redis7"),
				Description:               awsv2.String("orders redis 7 params"),
				IsGlobal:                  awsv2.Bool(false),
			}},
		}},
		userPages: []*awselasticache.DescribeUsersOutput{{
			Users: []awselasticachetypes.User{{
				ARN:                  awsv2.String(userARN),
				UserId:               awsv2.String("orders-app"),
				UserName:             awsv2.String("orders-app"),
				Engine:               awsv2.String("redis"),
				Status:               awsv2.String("active"),
				MinimumEngineVersion: awsv2.String("6.0"),
				AccessString:         awsv2.String("on ~* +@all"),
				Authentication: &awselasticachetypes.Authentication{
					Type:          awselasticachetypes.AuthenticationTypePassword,
					PasswordCount: awsv2.Int32(2),
				},
				UserGroupIds: []string{"orders-app-group"},
			}},
		}},
		userGroupPages: []*awselasticache.DescribeUserGroupsOutput{{
			UserGroups: []awselasticachetypes.UserGroup{{
				ARN:         awsv2.String(userGroupARN),
				UserGroupId: awsv2.String("orders-app-group"),
				Engine:      awsv2.String("redis"),
				Status:      awsv2.String("active"),
				UserIds:     []string{"orders-app"},
			}},
		}},
		snapshotPages: []*awselasticache.DescribeSnapshotsOutput{{
			Snapshots: []awselasticachetypes.Snapshot{{
				ARN:                awsv2.String(snapshotARN),
				SnapshotName:       awsv2.String("orders-2026-05-27"),
				SnapshotStatus:     awsv2.String("available"),
				SnapshotSource:     awsv2.String("manual"),
				CacheClusterId:     awsv2.String("orders-cache-001"),
				ReplicationGroupId: awsv2.String("orders"),
				Engine:             awsv2.String("redis"),
				EngineVersion:      awsv2.String("7.1"),
				KmsKeyId:           awsv2.String("arn:aws:kms:us-east-1:123456789012:key/orders"),
				NodeSnapshots: []awselasticachetypes.NodeSnapshot{{
					CacheClusterId: awsv2.String("orders-cache-001"),
					CacheNodeId:    awsv2.String("0001"),
				}},
			}},
		}},
		tags: map[string][]awselasticachetypes.Tag{
			clusterARN:          {{Key: awsv2.String("Environment"), Value: awsv2.String("prod")}},
			replicationGroupARN: {{Key: awsv2.String("Environment"), Value: awsv2.String("prod")}},
			subnetGroupARN:      {{Key: awsv2.String("Network"), Value: awsv2.String("private")}},
			parameterGroupARN:   {{Key: awsv2.String("Environment"), Value: awsv2.String("prod")}},
			userARN:             {{Key: awsv2.String("Environment"), Value: awsv2.String("prod")}},
			userGroupARN:        {{Key: awsv2.String("Environment"), Value: awsv2.String("prod")}},
			snapshotARN:         {{Key: awsv2.String("Environment"), Value: awsv2.String("prod")}},
		},
	}
	adapter := &Client{client: api, boundary: testBoundary()}

	clusters, err := adapter.ListCacheClusters(context.Background())
	if err != nil {
		t.Fatalf("ListCacheClusters() error = %v", err)
	}
	if got, want := len(clusters), 1; got != want {
		t.Fatalf("len(clusters) = %d, want %d", got, want)
	}
	cluster := clusters[0]
	if cluster.ID != "orders-cache-001" {
		t.Fatalf("cluster.ID = %q, want orders-cache-001", cluster.ID)
	}
	if cluster.SubnetGroupName != "orders-cache" {
		t.Fatalf("cluster.SubnetGroupName = %q, want orders-cache", cluster.SubnetGroupName)
	}
	if !cluster.AuthTokenEnabled {
		t.Fatalf("cluster.AuthTokenEnabled = false, want true")
	}
	if cluster.Tags["Environment"] != "prod" {
		t.Fatalf("cluster.Tags = %#v, want Environment=prod", cluster.Tags)
	}
	if cluster.VPCID == "" || cluster.VPCID != "vpc-123" {
		t.Fatalf("cluster.VPCID = %q, want vpc-123 (resolved from subnet group)", cluster.VPCID)
	}
	if got, want := cluster.SubnetIDs, []string{"subnet-a", "subnet-b"}; !stringSlicesEqual(got, want) {
		t.Fatalf("cluster.SubnetIDs = %#v, want %#v (resolved from subnet group)", got, want)
	}
	if got, want := cluster.SecurityGroupIDs, []string{"sg-123"}; !stringSlicesEqual(got, want) {
		t.Fatalf("cluster.SecurityGroupIDs = %#v, want %#v", got, want)
	}
	if cluster.ParameterGroupName != "orders-redis7" {
		t.Fatalf("cluster.ParameterGroupName = %q, want orders-redis7", cluster.ParameterGroupName)
	}
	if got, want := cluster.KMSKeyID, "arn:aws:kms:us-east-1:123456789012:key/orders"; got != want {
		t.Fatalf("cluster.KMSKeyID = %q, want %q (resolved from replication group)", got, want)
	}

	replicationGroups, err := adapter.ListReplicationGroups(context.Background())
	if err != nil {
		t.Fatalf("ListReplicationGroups() error = %v", err)
	}
	if got, want := len(replicationGroups), 1; got != want {
		t.Fatalf("len(replicationGroups) = %d, want %d", got, want)
	}
	rg := replicationGroups[0]
	if rg.ID != "orders" {
		t.Fatalf("rg.ID = %q, want orders", rg.ID)
	}
	if rg.AutomaticFailover != "enabled" {
		t.Fatalf("rg.AutomaticFailover = %q, want enabled", rg.AutomaticFailover)
	}
	if !rg.AuthTokenEnabled {
		t.Fatalf("rg.AuthTokenEnabled = false, want true")
	}

	subnetGroups, err := adapter.ListCacheSubnetGroups(context.Background())
	if err != nil {
		t.Fatalf("ListCacheSubnetGroups() error = %v", err)
	}
	if subnetGroups[0].VPCID != "vpc-123" {
		t.Fatalf("subnetGroups[0].VPCID = %q, want vpc-123", subnetGroups[0].VPCID)
	}

	parameterGroups, err := adapter.ListCacheParameterGroups(context.Background())
	if err != nil {
		t.Fatalf("ListCacheParameterGroups() error = %v", err)
	}
	if parameterGroups[0].Family != "redis7" {
		t.Fatalf("parameterGroups[0].Family = %q, want redis7", parameterGroups[0].Family)
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
	rawUserStruct := userStructFields(user)
	for _, forbidden := range []string{"AccessString", "Passwords", "Password", "AuthToken"} {
		if _, exists := rawUserStruct[forbidden]; exists {
			t.Fatalf("User struct exposes %q; ElastiCache adapter must drop AccessString/Passwords before scanner sees them", forbidden)
		}
	}

	userGroups, err := adapter.ListUserGroups(context.Background())
	if err != nil {
		t.Fatalf("ListUserGroups() error = %v", err)
	}
	if got, want := userGroups[0].UserIDs, []string{"orders-app"}; !stringSlicesEqual(got, want) {
		t.Fatalf("userGroups[0].UserIDs = %#v, want %#v", got, want)
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
	if snapshot.SourceCacheClusterID != "orders-cache-001" {
		t.Fatalf("snapshot.SourceCacheClusterID = %q, want orders-cache-001", snapshot.SourceCacheClusterID)
	}
	if snapshot.Status != "available" {
		t.Fatalf("snapshot.Status = %q, want available", snapshot.Status)
	}
	rawSnapshotStruct := snapshotStructFields(snapshot)
	for _, forbidden := range []string{"NodeSnapshots", "Engine", "EngineVersion", "KmsKeyId", "Port", "AuthToken"} {
		if _, exists := rawSnapshotStruct[forbidden]; exists {
			t.Fatalf("SnapshotMetadata exposes %q; ElastiCache adapter must persist name/source/status only", forbidden)
		}
	}
}

func TestClientPaginatesCacheClusters(t *testing.T) {
	api := &fakeElastiCacheAPI{
		cacheClusterPages: []*awselasticache.DescribeCacheClustersOutput{{
			CacheClusters: []awselasticachetypes.CacheCluster{{CacheClusterId: awsv2.String("first")}},
			Marker:        awsv2.String("next"),
		}, {
			CacheClusters: []awselasticachetypes.CacheCluster{{CacheClusterId: awsv2.String("second")}},
		}},
	}
	adapter := &Client{client: api, boundary: testBoundary()}

	clusters, err := adapter.ListCacheClusters(context.Background())
	if err != nil {
		t.Fatalf("ListCacheClusters() error = %v", err)
	}
	if got, want := len(clusters), 2; got != want {
		t.Fatalf("len(clusters) = %d, want %d", got, want)
	}
	if got, want := api.cacheClusterMarkers, []string{"", "next"}; !stringSlicesEqual(got, want) {
		t.Fatalf("DescribeCacheClusters markers = %#v, want %#v", got, want)
	}
}

func testBoundary() aws.Boundary {
	return aws.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: aws.ServiceElastiCache,
	}
}

type fakeElastiCacheAPI struct {
	cacheClusterPages   []*awselasticache.DescribeCacheClustersOutput
	cacheClusterCalls   int
	cacheClusterMarkers []string

	replicationGroupPages []*awselasticache.DescribeReplicationGroupsOutput
	replicationGroupCalls int

	subnetGroupPages []*awselasticache.DescribeCacheSubnetGroupsOutput
	subnetGroupCalls int

	parameterGroupPages []*awselasticache.DescribeCacheParameterGroupsOutput
	parameterGroupCalls int

	userPages []*awselasticache.DescribeUsersOutput
	userCalls int

	userGroupPages []*awselasticache.DescribeUserGroupsOutput
	userGroupCalls int

	snapshotPages []*awselasticache.DescribeSnapshotsOutput
	snapshotCalls int

	tags map[string][]awselasticachetypes.Tag
}

func (f *fakeElastiCacheAPI) DescribeCacheClusters(
	_ context.Context,
	input *awselasticache.DescribeCacheClustersInput,
	_ ...func(*awselasticache.Options),
) (*awselasticache.DescribeCacheClustersOutput, error) {
	f.cacheClusterMarkers = append(f.cacheClusterMarkers, awsv2.ToString(input.Marker))
	if f.cacheClusterCalls >= len(f.cacheClusterPages) {
		return &awselasticache.DescribeCacheClustersOutput{}, nil
	}
	page := f.cacheClusterPages[f.cacheClusterCalls]
	f.cacheClusterCalls++
	return page, nil
}

func (f *fakeElastiCacheAPI) DescribeReplicationGroups(
	_ context.Context,
	_ *awselasticache.DescribeReplicationGroupsInput,
	_ ...func(*awselasticache.Options),
) (*awselasticache.DescribeReplicationGroupsOutput, error) {
	if f.replicationGroupCalls >= len(f.replicationGroupPages) {
		return &awselasticache.DescribeReplicationGroupsOutput{}, nil
	}
	page := f.replicationGroupPages[f.replicationGroupCalls]
	f.replicationGroupCalls++
	return page, nil
}

func (f *fakeElastiCacheAPI) DescribeCacheSubnetGroups(
	_ context.Context,
	_ *awselasticache.DescribeCacheSubnetGroupsInput,
	_ ...func(*awselasticache.Options),
) (*awselasticache.DescribeCacheSubnetGroupsOutput, error) {
	if f.subnetGroupCalls >= len(f.subnetGroupPages) {
		return &awselasticache.DescribeCacheSubnetGroupsOutput{}, nil
	}
	page := f.subnetGroupPages[f.subnetGroupCalls]
	f.subnetGroupCalls++
	return page, nil
}

func (f *fakeElastiCacheAPI) DescribeCacheParameterGroups(
	_ context.Context,
	_ *awselasticache.DescribeCacheParameterGroupsInput,
	_ ...func(*awselasticache.Options),
) (*awselasticache.DescribeCacheParameterGroupsOutput, error) {
	if f.parameterGroupCalls >= len(f.parameterGroupPages) {
		return &awselasticache.DescribeCacheParameterGroupsOutput{}, nil
	}
	page := f.parameterGroupPages[f.parameterGroupCalls]
	f.parameterGroupCalls++
	return page, nil
}

func (f *fakeElastiCacheAPI) DescribeUsers(
	_ context.Context,
	_ *awselasticache.DescribeUsersInput,
	_ ...func(*awselasticache.Options),
) (*awselasticache.DescribeUsersOutput, error) {
	if f.userCalls >= len(f.userPages) {
		return &awselasticache.DescribeUsersOutput{}, nil
	}
	page := f.userPages[f.userCalls]
	f.userCalls++
	return page, nil
}

func (f *fakeElastiCacheAPI) DescribeUserGroups(
	_ context.Context,
	_ *awselasticache.DescribeUserGroupsInput,
	_ ...func(*awselasticache.Options),
) (*awselasticache.DescribeUserGroupsOutput, error) {
	if f.userGroupCalls >= len(f.userGroupPages) {
		return &awselasticache.DescribeUserGroupsOutput{}, nil
	}
	page := f.userGroupPages[f.userGroupCalls]
	f.userGroupCalls++
	return page, nil
}

func (f *fakeElastiCacheAPI) DescribeSnapshots(
	_ context.Context,
	_ *awselasticache.DescribeSnapshotsInput,
	_ ...func(*awselasticache.Options),
) (*awselasticache.DescribeSnapshotsOutput, error) {
	if f.snapshotCalls >= len(f.snapshotPages) {
		return &awselasticache.DescribeSnapshotsOutput{}, nil
	}
	page := f.snapshotPages[f.snapshotCalls]
	f.snapshotCalls++
	return page, nil
}

func (f *fakeElastiCacheAPI) ListTagsForResource(
	_ context.Context,
	input *awselasticache.ListTagsForResourceInput,
	_ ...func(*awselasticache.Options),
) (*awselasticache.ListTagsForResourceOutput, error) {
	if f.tags == nil {
		return &awselasticache.ListTagsForResourceOutput{}, nil
	}
	tags := f.tags[awsv2.ToString(input.ResourceName)]
	return &awselasticache.ListTagsForResourceOutput{TagList: tags}, nil
}

var _ apiClient = (*fakeElastiCacheAPI)(nil)

// userStructFields returns the exported field names present on the
// scanner-owned User type so reviewers cannot accidentally widen the user
// surface beyond metadata fields documented in CLAUDE.md and #713.
func userStructFields(_ elasticacheservice.User) map[string]struct{} {
	fields := map[string]struct{}{
		"ARN":                  {},
		"ID":                   {},
		"Name":                 {},
		"Engine":               {},
		"Status":               {},
		"AuthenticationType":   {},
		"PasswordCount":        {},
		"MinimumEngineVersion": {},
		"UserGroupIDs":         {},
		"Tags":                 {},
	}
	return fields
}

// snapshotStructFields enumerates the metadata-only fields on the scanner-owned
// SnapshotMetadata type. ElastiCache snapshots can carry node-level backup
// metadata and KMS evidence that the scanner must never persist.
func snapshotStructFields(_ elasticacheservice.SnapshotMetadata) map[string]struct{} {
	fields := map[string]struct{}{
		"ARN":                  {},
		"Name":                 {},
		"Status":               {},
		"SnapshotSource":       {},
		"SourceCacheClusterID": {},
		"SourceReplicationGrp": {},
		"Tags":                 {},
	}
	return fields
}

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
