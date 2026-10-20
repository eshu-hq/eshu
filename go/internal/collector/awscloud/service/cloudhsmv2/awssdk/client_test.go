// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscloudhsmv2 "github.com/aws/aws-sdk-go-v2/service/cloudhsmv2"
	awscloudhsmv2types "github.com/aws/aws-sdk-go-v2/service/cloudhsmv2/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestClientSnapshotsCloudHSMMetadataOnly(t *testing.T) {
	createdAt := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	backupARN := "arn:aws:cloudhsm:us-east-1:123456789012:backup/backup-test1234567"

	api := &fakeCloudHSMAPI{
		clusterPages: []*awscloudhsmv2.DescribeClustersOutput{{
			Clusters: []awscloudhsmv2types.Cluster{{
				ClusterId:     aws.String("cluster-test1234567"),
				State:         awscloudhsmv2types.ClusterStateActive,
				StateMessage:  aws.String("Cluster is active."),
				HsmType:       aws.String("hsm1.medium"),
				Mode:          awscloudhsmv2types.ClusterModeFips,
				NetworkType:   awscloudhsmv2types.NetworkTypeIpv4,
				VpcId:         aws.String("vpc-0123456789abcdef0"),
				SecurityGroup: aws.String("sg-0123456789abcdef0"),
				BackupPolicy:  awscloudhsmv2types.BackupPolicyDefault,
				BackupRetentionPolicy: &awscloudhsmv2types.BackupRetentionPolicy{
					Type:  awscloudhsmv2types.BackupRetentionTypeDays,
					Value: aws.String("90"),
				},
				SubnetMapping: map[string]string{
					"us-east-1a": "subnet-0aaa1111bbbb2222a",
				},
				Hsms: []awscloudhsmv2types.Hsm{{
					HsmId:            aws.String("hsm-aaaa1111bbbb2222"),
					State:            awscloudhsmv2types.HsmStateActive,
					AvailabilityZone: aws.String("us-east-1a"),
					SubnetId:         aws.String("subnet-0aaa1111bbbb2222a"),
					EniId:            aws.String("eni-0123456789abcdef0"),
					EniIp:            aws.String("10.0.1.10"),
				}},
				Certificates: &awscloudhsmv2types.Certificates{
					ClusterCertificate:     aws.String("-----BEGIN CERTIFICATE-----\nMIIB...\n-----END CERTIFICATE-----"),
					HsmCertificate:         aws.String("-----BEGIN CERTIFICATE-----\nMIIC...\n-----END CERTIFICATE-----"),
					AwsHardwareCertificate: aws.String("-----BEGIN CERTIFICATE-----\nMIID...\n-----END CERTIFICATE-----"),
				},
				CreateTimestamp: aws.Time(createdAt),
				TagList: []awscloudhsmv2types.Tag{
					{Key: aws.String("Environment"), Value: aws.String("prod")},
				},
				PreCoPassword: aws.String("super-secret-preco-password"),
			}},
		}},
		backupPages: []*awscloudhsmv2.DescribeBackupsOutput{{
			Backups: []awscloudhsmv2types.Backup{{
				BackupId:        aws.String("backup-test1234567"),
				BackupArn:       aws.String(backupARN),
				BackupState:     awscloudhsmv2types.BackupStateReady,
				ClusterId:       aws.String("cluster-test1234567"),
				HsmType:         aws.String("hsm1.medium"),
				Mode:            awscloudhsmv2types.ClusterModeFips,
				NeverExpires:    aws.Bool(true),
				CreateTimestamp: aws.Time(createdAt),
				TagList: []awscloudhsmv2types.Tag{
					{Key: aws.String("Team"), Value: aws.String("security")},
				},
			}},
		}},
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
	if cluster.ID != "cluster-test1234567" {
		t.Fatalf("cluster ID = %q", cluster.ID)
	}
	if cluster.State != "ACTIVE" {
		t.Fatalf("cluster State = %q, want ACTIVE", cluster.State)
	}
	if cluster.Mode != "FIPS" {
		t.Fatalf("cluster Mode = %q, want FIPS", cluster.Mode)
	}
	if cluster.VPCID != "vpc-0123456789abcdef0" {
		t.Fatalf("cluster VPCID = %q", cluster.VPCID)
	}
	if cluster.SecurityGroupID != "sg-0123456789abcdef0" {
		t.Fatalf("cluster SecurityGroupID = %q", cluster.SecurityGroupID)
	}
	if cluster.BackupRetentionType != "DAYS" || cluster.BackupRetentionValue != "90" {
		t.Fatalf("retention = %q/%q, want DAYS/90", cluster.BackupRetentionType, cluster.BackupRetentionValue)
	}
	if len(cluster.SubnetMappings) != 1 || cluster.SubnetMappings[0].SubnetID != "subnet-0aaa1111bbbb2222a" {
		t.Fatalf("SubnetMappings = %#v", cluster.SubnetMappings)
	}
	if len(cluster.HSMs) != 1 || cluster.HSMs[0].ENIIP != "10.0.1.10" {
		t.Fatalf("HSMs = %#v", cluster.HSMs)
	}
	if !cluster.CertificatePresence.ClusterCertificate || !cluster.CertificatePresence.HSMCertificate {
		t.Fatalf("certificate presence = %#v, want cluster+HSM present", cluster.CertificatePresence)
	}
	if cluster.CertificatePresence.ClusterCSR {
		t.Fatalf("ClusterCSR present = true, want false (no CSR supplied)")
	}
	if cluster.Tags["Environment"] != "prod" {
		t.Fatalf("cluster tag Environment = %q", cluster.Tags["Environment"])
	}

	if len(snapshot.Backups) != 1 {
		t.Fatalf("len(Backups) = %d, want 1", len(snapshot.Backups))
	}
	backup := snapshot.Backups[0]
	if backup.ID != "backup-test1234567" || backup.ARN != backupARN {
		t.Fatalf("backup id/arn = %q/%q", backup.ID, backup.ARN)
	}
	if backup.State != "READY" {
		t.Fatalf("backup State = %q, want READY", backup.State)
	}
	if !backup.NeverExpires {
		t.Fatalf("backup NeverExpires = false, want true")
	}
	if backup.Tags["Team"] != "security" {
		t.Fatalf("backup tag Team = %q", backup.Tags["Team"])
	}
}

func TestClientPaginatesClustersAndBackups(t *testing.T) {
	api := &fakeCloudHSMAPI{
		clusterPages: []*awscloudhsmv2.DescribeClustersOutput{
			{
				Clusters:  []awscloudhsmv2types.Cluster{{ClusterId: aws.String("cluster-page1aaaaaaa")}},
				NextToken: aws.String("c2"),
			},
			{Clusters: []awscloudhsmv2types.Cluster{{ClusterId: aws.String("cluster-page2bbbbbbb")}}},
		},
		backupPages: []*awscloudhsmv2.DescribeBackupsOutput{
			{
				Backups:   []awscloudhsmv2types.Backup{{BackupId: aws.String("backup-page1aaaaaaa")}},
				NextToken: aws.String("b2"),
			},
			{Backups: []awscloudhsmv2types.Backup{{BackupId: aws.String("backup-page2bbbbbbb")}}},
		},
	}

	client := &Client{client: api, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if len(snapshot.Clusters) != 2 {
		t.Fatalf("len(Clusters) = %d, want 2 across pages", len(snapshot.Clusters))
	}
	if len(snapshot.Backups) != 2 {
		t.Fatalf("len(Backups) = %d, want 2 across pages", len(snapshot.Backups))
	}
}

type fakeCloudHSMAPI struct {
	clusterPages []*awscloudhsmv2.DescribeClustersOutput
	clusterCall  int
	backupPages  []*awscloudhsmv2.DescribeBackupsOutput
	backupCall   int
}

func (f *fakeCloudHSMAPI) DescribeClusters(
	_ context.Context,
	_ *awscloudhsmv2.DescribeClustersInput,
	_ ...func(*awscloudhsmv2.Options),
) (*awscloudhsmv2.DescribeClustersOutput, error) {
	if f.clusterCall >= len(f.clusterPages) {
		return &awscloudhsmv2.DescribeClustersOutput{}, nil
	}
	page := f.clusterPages[f.clusterCall]
	f.clusterCall++
	return page, nil
}

func (f *fakeCloudHSMAPI) DescribeBackups(
	_ context.Context,
	_ *awscloudhsmv2.DescribeBackupsInput,
	_ ...func(*awscloudhsmv2.Options),
) (*awscloudhsmv2.DescribeBackupsOutput, error) {
	if f.backupCall >= len(f.backupPages) {
		return &awscloudhsmv2.DescribeBackupsOutput{}, nil
	}
	page := f.backupPages[f.backupCall]
	f.backupCall++
	return page, nil
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceCloudHSMV2,
	}
}
