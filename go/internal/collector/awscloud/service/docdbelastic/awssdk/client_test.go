// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsdocdbelastic "github.com/aws/aws-sdk-go-v2/service/docdbelastic"
	awsdocdbelastictypes "github.com/aws/aws-sdk-go-v2/service/docdbelastic/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestClientSnapshotsClusterMetadataOnly(t *testing.T) {
	clusterARN := "arn:aws:docdb-elastic:us-east-1:123456789012:cluster/abcd1234"
	kmsARN := "arn:aws:kms:us-east-1:123456789012:key/1234abcd"
	secretARN := "arn:aws:secretsmanager:us-east-1:123456789012:secret:docdb/admin-AbCdEf"

	api := &fakeDocDBElasticAPI{
		listPages: []*awsdocdbelastic.ListClustersOutput{{
			Clusters: []awsdocdbelastictypes.ClusterInList{{
				ClusterArn:  aws.String(clusterARN),
				ClusterName: aws.String("analytics"),
				Status:      awsdocdbelastictypes.StatusActive,
			}},
		}},
		clusters: map[string]*awsdocdbelastictypes.Cluster{
			clusterARN: {
				ClusterArn:                 aws.String(clusterARN),
				ClusterName:                aws.String("analytics"),
				Status:                     awsdocdbelastictypes.StatusActive,
				AuthType:                   awsdocdbelastictypes.AuthSecretArn,
				AdminUserName:              aws.String(secretARN),
				ClusterEndpoint:            aws.String("analytics.cluster-abcd.us-east-1.docdb-elastic.amazonaws.com:27017"),
				KmsKeyId:                   aws.String(kmsARN),
				ShardCapacity:              aws.Int32(4),
				ShardCount:                 aws.Int32(2),
				ShardInstanceCount:         aws.Int32(3),
				BackupRetentionPeriod:      aws.Int32(7),
				PreferredBackupWindow:      aws.String("02:00-03:00"),
				PreferredMaintenanceWindow: aws.String("sun:05:00-sun:06:00"),
				SubnetIds:                  []string{"subnet-0a1b2c3d", "subnet-4e5f6a7b"},
				VpcSecurityGroupIds:        []string{"sg-0123456789abcdef0"},
				CreateTime:                 aws.String("2026-05-14T12:00:00Z"),
			},
		},
		tags: map[string]map[string]string{
			clusterARN: {"Environment": "prod"},
		},
	}

	client := &Client{client: api, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if len(snapshot.Clusters) != 1 {
		t.Fatalf("len(Clusters) = %d, want 1", len(snapshot.Clusters))
	}
	cluster := snapshot.Clusters[0]
	if cluster.ARN != clusterARN {
		t.Fatalf("cluster ARN = %q, want %q", cluster.ARN, clusterARN)
	}
	if cluster.Status != "ACTIVE" {
		t.Fatalf("cluster Status = %q, want ACTIVE", cluster.Status)
	}
	if cluster.AuthType != "SECRET_ARN" {
		t.Fatalf("cluster AuthType = %q, want SECRET_ARN", cluster.AuthType)
	}
	if cluster.AdminSecretARN != secretARN {
		t.Fatalf("cluster AdminSecretARN = %q, want %q", cluster.AdminSecretARN, secretARN)
	}
	if cluster.KMSKeyID != kmsARN {
		t.Fatalf("cluster KMSKeyID = %q, want %q", cluster.KMSKeyID, kmsARN)
	}
	if cluster.ShardCapacity != 4 || cluster.ShardCount != 2 || cluster.ShardInstanceCount != 3 {
		t.Fatalf("shard topology = %d/%d/%d, want 4/2/3", cluster.ShardCapacity, cluster.ShardCount, cluster.ShardInstanceCount)
	}
	if len(cluster.SubnetIDs) != 2 || cluster.SubnetIDs[0] != "subnet-0a1b2c3d" {
		t.Fatalf("SubnetIDs = %#v, want two subnet ids", cluster.SubnetIDs)
	}
	if len(cluster.SecurityGroupIDs) != 1 || cluster.SecurityGroupIDs[0] != "sg-0123456789abcdef0" {
		t.Fatalf("SecurityGroupIDs = %#v, want one sg id", cluster.SecurityGroupIDs)
	}
	if cluster.CreateTime.IsZero() {
		t.Fatalf("CreateTime = zero, want parsed time")
	}
	if cluster.Tags["Environment"] != "prod" {
		t.Fatalf("cluster tag Environment = %q, want prod", cluster.Tags["Environment"])
	}
}

func TestClientDropsAdminUserNameForPlainTextAuth(t *testing.T) {
	clusterARN := "arn:aws:docdb-elastic:us-east-1:123456789012:cluster/plain"
	api := &fakeDocDBElasticAPI{
		listPages: []*awsdocdbelastic.ListClustersOutput{{
			Clusters: []awsdocdbelastictypes.ClusterInList{{
				ClusterArn:  aws.String(clusterARN),
				ClusterName: aws.String("plain"),
				Status:      awsdocdbelastictypes.StatusActive,
			}},
		}},
		clusters: map[string]*awsdocdbelastictypes.Cluster{
			clusterARN: {
				ClusterArn:    aws.String(clusterARN),
				ClusterName:   aws.String("plain"),
				Status:        awsdocdbelastictypes.StatusActive,
				AuthType:      awsdocdbelastictypes.AuthPlainText,
				AdminUserName: aws.String("dbadmin"),
			},
		},
	}

	client := &Client{client: api, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	cluster := snapshot.Clusters[0]
	if cluster.AdminSecretARN != "" {
		t.Fatalf("AdminSecretARN = %q, want empty for PLAIN_TEXT auth (admin user name must never be persisted)", cluster.AdminSecretARN)
	}
}

type fakeDocDBElasticAPI struct {
	listPages []*awsdocdbelastic.ListClustersOutput
	listCall  int
	clusters  map[string]*awsdocdbelastictypes.Cluster
	tags      map[string]map[string]string
}

func (f *fakeDocDBElasticAPI) ListClusters(
	_ context.Context,
	_ *awsdocdbelastic.ListClustersInput,
	_ ...func(*awsdocdbelastic.Options),
) (*awsdocdbelastic.ListClustersOutput, error) {
	if f.listCall >= len(f.listPages) {
		return &awsdocdbelastic.ListClustersOutput{}, nil
	}
	page := f.listPages[f.listCall]
	f.listCall++
	return page, nil
}

func (f *fakeDocDBElasticAPI) GetCluster(
	_ context.Context,
	input *awsdocdbelastic.GetClusterInput,
	_ ...func(*awsdocdbelastic.Options),
) (*awsdocdbelastic.GetClusterOutput, error) {
	return &awsdocdbelastic.GetClusterOutput{
		Cluster: f.clusters[aws.ToString(input.ClusterArn)],
	}, nil
}

func (f *fakeDocDBElasticAPI) ListTagsForResource(
	_ context.Context,
	input *awsdocdbelastic.ListTagsForResourceInput,
	_ ...func(*awsdocdbelastic.Options),
) (*awsdocdbelastic.ListTagsForResourceOutput, error) {
	return &awsdocdbelastic.ListTagsForResourceOutput{
		Tags: f.tags[aws.ToString(input.ResourceArn)],
	}, nil
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceDocDBElastic,
	}
}
