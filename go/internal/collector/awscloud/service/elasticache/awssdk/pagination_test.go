// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awselasticache "github.com/aws/aws-sdk-go-v2/service/elasticache"
	awselasticachetypes "github.com/aws/aws-sdk-go-v2/service/elasticache/types"
)

// TestClientPaginatesReplicationGroups proves ListReplicationGroups follows the
// Marker token across pages (client.go fetchReplicationGroups). A page with a
// non-empty Marker triggers a second DescribeReplicationGroups call; a page
// without a Marker terminates the loop.
func TestClientPaginatesReplicationGroups(t *testing.T) {
	api := &fakeElastiCacheAPI{
		replicationGroupPages: []*awselasticache.DescribeReplicationGroupsOutput{{
			ReplicationGroups: []awselasticachetypes.ReplicationGroup{{
				ARN:                aws.String("arn:aws:elasticache:us-east-1:123456789012:replicationgroup:first"),
				ReplicationGroupId: aws.String("first"),
			}},
			Marker: aws.String("rg-next"),
		}, {
			ReplicationGroups: []awselasticachetypes.ReplicationGroup{{
				ARN:                aws.String("arn:aws:elasticache:us-east-1:123456789012:replicationgroup:second"),
				ReplicationGroupId: aws.String("second"),
			}},
		}},
	}
	adapter := &Client{client: api, boundary: testBoundary()}

	groups, err := adapter.ListReplicationGroups(context.Background())
	if err != nil {
		t.Fatalf("ListReplicationGroups() error = %v", err)
	}
	if got, want := len(groups), 2; got != want {
		t.Fatalf("len(replicationGroups) = %d, want %d", got, want)
	}
	if groups[0].ID != "first" || groups[1].ID != "second" {
		t.Fatalf("replication group IDs = %q / %q, want first / second", groups[0].ID, groups[1].ID)
	}
}

// TestClientPaginatesSubnetGroups proves ListCacheSubnetGroups follows the
// Marker token across pages (client.go fetchSubnetGroups). Subnet-group field
// mapping is covered by TestClientListsElastiCacheMetadataOnly; only the
// cross-page count is checked here.
func TestClientPaginatesSubnetGroups(t *testing.T) {
	api := &fakeElastiCacheAPI{
		subnetGroupPages: []*awselasticache.DescribeCacheSubnetGroupsOutput{{
			CacheSubnetGroups: []awselasticachetypes.CacheSubnetGroup{{
				ARN:                  aws.String("arn:aws:elasticache:us-east-1:123456789012:subnetgroup:sg-a"),
				CacheSubnetGroupName: aws.String("sg-a"),
				VpcId:                aws.String("vpc-a"),
			}},
			Marker: aws.String("sg-next"),
		}, {
			CacheSubnetGroups: []awselasticachetypes.CacheSubnetGroup{{
				ARN:                  aws.String("arn:aws:elasticache:us-east-1:123456789012:subnetgroup:sg-b"),
				CacheSubnetGroupName: aws.String("sg-b"),
				VpcId:                aws.String("vpc-b"),
			}},
		}},
	}
	adapter := &Client{client: api, boundary: testBoundary()}

	groups, err := adapter.ListCacheSubnetGroups(context.Background())
	if err != nil {
		t.Fatalf("ListCacheSubnetGroups() error = %v", err)
	}
	if got, want := len(groups), 2; got != want {
		t.Fatalf("len(subnetGroups) = %d, want %d", got, want)
	}
}

// TestClientPaginatesParameterGroups proves ListCacheParameterGroups follows
// the Marker token across pages (client.go ListCacheParameterGroups).
func TestClientPaginatesParameterGroups(t *testing.T) {
	api := &fakeElastiCacheAPI{
		parameterGroupPages: []*awselasticache.DescribeCacheParameterGroupsOutput{{
			CacheParameterGroups: []awselasticachetypes.CacheParameterGroup{{
				ARN:                     aws.String("arn:aws:elasticache:us-east-1:123456789012:parametergroup:pg-a"),
				CacheParameterGroupName: aws.String("pg-a"),
			}},
			Marker: aws.String("pg-next"),
		}, {
			CacheParameterGroups: []awselasticachetypes.CacheParameterGroup{{
				ARN:                     aws.String("arn:aws:elasticache:us-east-1:123456789012:parametergroup:pg-b"),
				CacheParameterGroupName: aws.String("pg-b"),
			}},
		}},
	}
	adapter := &Client{client: api, boundary: testBoundary()}

	groups, err := adapter.ListCacheParameterGroups(context.Background())
	if err != nil {
		t.Fatalf("ListCacheParameterGroups() error = %v", err)
	}
	if got, want := len(groups), 2; got != want {
		t.Fatalf("len(parameterGroups) = %d, want %d", got, want)
	}
}

// TestClientCacheClusterWithoutReplicationGroupHasNoKMSKey proves that a
// standalone cluster (no ReplicationGroupId) ends up with an empty KMSKeyID
// after the replication-group KMS join, rather than borrowing a key from
// another group. This exercises the miss path on the replicationGroupKMS index
// in mapper.go mapCacheCluster (lines 51–53).
func TestClientCacheClusterWithoutReplicationGroupHasNoKMSKey(t *testing.T) {
	api := &fakeElastiCacheAPI{
		cacheClusterPages: []*awselasticache.DescribeCacheClustersOutput{{
			CacheClusters: []awselasticachetypes.CacheCluster{{
				ARN:            aws.String("arn:aws:elasticache:us-east-1:123456789012:cluster:standalone"),
				CacheClusterId: aws.String("standalone"),
				Engine:         aws.String("memcached"),
				// ReplicationGroupId intentionally absent.
			}},
		}},
		// A separate replication group exists but must NOT lend its KMS key
		// to the standalone cluster.
		replicationGroupPages: []*awselasticache.DescribeReplicationGroupsOutput{{
			ReplicationGroups: []awselasticachetypes.ReplicationGroup{{
				ARN:                aws.String("arn:aws:elasticache:us-east-1:123456789012:replicationgroup:other"),
				ReplicationGroupId: aws.String("other"),
				KmsKeyId:           aws.String("arn:aws:kms:us-east-1:123456789012:key/other"),
			}},
		}},
	}
	adapter := &Client{client: api, boundary: testBoundary()}

	clusters, err := adapter.ListCacheClusters(context.Background())
	if err != nil {
		t.Fatalf("ListCacheClusters() error = %v", err)
	}
	if got, want := len(clusters), 1; got != want {
		t.Fatalf("len(clusters) = %d, want %d", got, want)
	}
	if clusters[0].KMSKeyID != "" {
		t.Fatalf("cluster.KMSKeyID = %q, want empty for a standalone cluster with no replication group", clusters[0].KMSKeyID)
	}
}

// TestClientCacheClusterWithNoSubnetGroupHasNoVPC proves that a cluster with
// no CacheSubnetGroupName resolves to empty VPCID and SubnetIDs rather than
// matching against a random subnet group in the index. This exercises the miss
// path on the subnetGroups index lookup in mapper.go mapCacheCluster
// (lines 47–50).
func TestClientCacheClusterWithNoSubnetGroupHasNoVPC(t *testing.T) {
	api := &fakeElastiCacheAPI{
		cacheClusterPages: []*awselasticache.DescribeCacheClustersOutput{{
			CacheClusters: []awselasticachetypes.CacheCluster{{
				ARN:            aws.String("arn:aws:elasticache:us-east-1:123456789012:cluster:no-subnet"),
				CacheClusterId: aws.String("no-subnet"),
				Engine:         aws.String("memcached"),
				// CacheSubnetGroupName intentionally absent.
			}},
		}},
		subnetGroupPages: []*awselasticache.DescribeCacheSubnetGroupsOutput{{
			CacheSubnetGroups: []awselasticachetypes.CacheSubnetGroup{{
				ARN:                  aws.String("arn:aws:elasticache:us-east-1:123456789012:subnetgroup:other-sg"),
				CacheSubnetGroupName: aws.String("other-sg"),
				VpcId:                aws.String("vpc-other"),
			}},
		}},
	}
	adapter := &Client{client: api, boundary: testBoundary()}

	clusters, err := adapter.ListCacheClusters(context.Background())
	if err != nil {
		t.Fatalf("ListCacheClusters() error = %v", err)
	}
	if got, want := len(clusters), 1; got != want {
		t.Fatalf("len(clusters) = %d, want %d", got, want)
	}
	if clusters[0].VPCID != "" {
		t.Fatalf("cluster.VPCID = %q, want empty for a cluster with no subnet group name", clusters[0].VPCID)
	}
	if len(clusters[0].SubnetIDs) != 0 {
		t.Fatalf("cluster.SubnetIDs = %#v, want empty", clusters[0].SubnetIDs)
	}
}
