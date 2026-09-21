// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsdms "github.com/aws/aws-sdk-go-v2/service/databasemigrationservice"
	awsdmstypes "github.com/aws/aws-sdk-go-v2/service/databasemigrationservice/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestClientSnapshotsDMSMetadataOnly(t *testing.T) {
	instanceARN := "arn:aws:dms:us-east-1:123456789012:rep:ABCDEFGHIJKLMNOP"
	endpointARN := "arn:aws:dms:us-east-1:123456789012:endpoint:SOURCEENDPOINTAAA"
	taskARN := "arn:aws:dms:us-east-1:123456789012:task:MIGRATIONTASKCCCC"
	kmsARN := "arn:aws:kms:us-east-1:123456789012:key/1234abcd"
	streamARN := "arn:aws:kinesis:us-east-1:123456789012:stream/cdc"
	secretARN := "arn:aws:secretsmanager:us-east-1:123456789012:secret:dms/src-AbCdEf"
	createdAt := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)

	api := &fakeDMSAPI{
		subnetGroupPages: pagedSubnetGroups([]awsdmstypes.ReplicationSubnetGroup{{
			ReplicationSubnetGroupIdentifier: aws.String("dms-subnet-group"),
			SubnetGroupStatus:                aws.String("Complete"),
			VpcId:                            aws.String("vpc-0abc1234"),
			Subnets: []awsdmstypes.Subnet{
				{SubnetIdentifier: aws.String("subnet-0a1b2c3d")},
				{SubnetIdentifier: aws.String("subnet-0e5f6a7b")},
			},
		}}),
		instancePages: pagedInstances([]awsdmstypes.ReplicationInstance{{
			ReplicationInstanceArn:        aws.String(instanceARN),
			ReplicationInstanceIdentifier: aws.String("dms-prod"),
			ReplicationInstanceClass:      aws.String("dms.r5.large"),
			EngineVersion:                 aws.String("3.5.2"),
			ReplicationInstanceStatus:     aws.String("available"),
			AllocatedStorage:              100,
			MultiAZ:                       true,
			KmsKeyId:                      aws.String(kmsARN),
			InstanceCreateTime:            aws.Time(createdAt),
			VpcSecurityGroups: []awsdmstypes.VpcSecurityGroupMembership{
				{VpcSecurityGroupId: aws.String("sg-0aabbccdd")},
			},
			ReplicationSubnetGroup: &awsdmstypes.ReplicationSubnetGroup{
				ReplicationSubnetGroupIdentifier: aws.String("dms-subnet-group"),
				VpcId:                            aws.String("vpc-0abc1234"),
				Subnets: []awsdmstypes.Subnet{
					{SubnetIdentifier: aws.String("subnet-0a1b2c3d")},
				},
			},
		}}),
		endpointPages: pagedEndpoints([]awsdmstypes.Endpoint{
			{
				EndpointArn:        aws.String(endpointARN),
				EndpointIdentifier: aws.String("source-postgres"),
				EndpointType:       awsdmstypes.ReplicationEndpointTypeValueSource,
				EngineName:         aws.String("postgres"),
				SslMode:            awsdmstypes.DmsSslModeValueRequire,
				Status:             aws.String("active"),
				DatabaseName:       aws.String("appdb"),
				Port:               aws.Int32(5432),
				KmsKeyId:           aws.String(kmsARN),
				PostgreSQLSettings: &awsdmstypes.PostgreSQLSettings{
					SecretsManagerSecretId: aws.String(secretARN),
				},
			},
			{
				EndpointArn:        aws.String("arn:aws:dms:us-east-1:123456789012:endpoint:TARGETENDPOINTBBB"),
				EndpointIdentifier: aws.String("target-stream"),
				EndpointType:       awsdmstypes.ReplicationEndpointTypeValueTarget,
				EngineName:         aws.String("kinesis"),
				Status:             aws.String("active"),
				KinesisSettings:    &awsdmstypes.KinesisSettings{StreamArn: aws.String(streamARN)},
				S3Settings:         &awsdmstypes.S3Settings{BucketName: aws.String("dms-cdc-bucket")},
			},
		}),
		taskPages: pagedTasks([]awsdmstypes.ReplicationTask{{
			ReplicationTaskArn:          aws.String(taskARN),
			ReplicationTaskIdentifier:   aws.String("prod-migration"),
			MigrationType:               awsdmstypes.MigrationTypeValueFullLoadAndCdc,
			Status:                      aws.String("running"),
			SourceEndpointArn:           aws.String(endpointARN),
			TargetEndpointArn:           aws.String("arn:aws:dms:us-east-1:123456789012:endpoint:TARGETENDPOINTBBB"),
			ReplicationInstanceArn:      aws.String(instanceARN),
			ReplicationTaskCreationDate: aws.Time(createdAt),
		}}),
		tags: map[string][]awsdmstypes.Tag{
			instanceARN: {{Key: aws.String("Team"), Value: aws.String("data")}},
			taskARN:     {{Key: aws.String("Pipeline"), Value: aws.String("cdc")}},
		},
	}

	client := &Client{client: api, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}

	if len(snapshot.SubnetGroups) != 1 {
		t.Fatalf("len(SubnetGroups) = %d, want 1", len(snapshot.SubnetGroups))
	}
	if got := snapshot.SubnetGroups[0].SubnetIDs; len(got) != 2 {
		t.Fatalf("subnet group subnet ids = %#v, want 2", got)
	}

	if len(snapshot.ReplicationInstances) != 1 {
		t.Fatalf("len(ReplicationInstances) = %d, want 1", len(snapshot.ReplicationInstances))
	}
	instance := snapshot.ReplicationInstances[0]
	if instance.ARN != instanceARN {
		t.Fatalf("instance ARN = %q, want %q", instance.ARN, instanceARN)
	}
	if instance.SubnetGroupIdentifier != "dms-subnet-group" {
		t.Fatalf("instance subnet group = %q, want dms-subnet-group", instance.SubnetGroupIdentifier)
	}
	if instance.VPCID != "vpc-0abc1234" {
		t.Fatalf("instance VPCID = %q, want vpc-0abc1234", instance.VPCID)
	}
	if len(instance.SecurityGroupIDs) != 1 || instance.SecurityGroupIDs[0] != "sg-0aabbccdd" {
		t.Fatalf("instance security groups = %#v, want [sg-0aabbccdd]", instance.SecurityGroupIDs)
	}
	if instance.Tags["Team"] != "data" {
		t.Fatalf("instance tag Team = %q, want data", instance.Tags["Team"])
	}

	if len(snapshot.Endpoints) != 2 {
		t.Fatalf("len(Endpoints) = %d, want 2", len(snapshot.Endpoints))
	}
	source := snapshot.Endpoints[0]
	if source.SecretsManagerSecretID != secretARN {
		t.Fatalf("source SecretsManagerSecretID = %q, want %q", source.SecretsManagerSecretID, secretARN)
	}
	if source.SSLMode != "require" {
		t.Fatalf("source SSLMode = %q, want require", source.SSLMode)
	}
	target := snapshot.Endpoints[1]
	if target.KinesisStreamARN != streamARN {
		t.Fatalf("target KinesisStreamARN = %q, want %q", target.KinesisStreamARN, streamARN)
	}
	if target.S3BucketName != "dms-cdc-bucket" {
		t.Fatalf("target S3BucketName = %q, want dms-cdc-bucket", target.S3BucketName)
	}

	if len(snapshot.Tasks) != 1 {
		t.Fatalf("len(Tasks) = %d, want 1", len(snapshot.Tasks))
	}
	task := snapshot.Tasks[0]
	if task.SourceEndpointARN != endpointARN {
		t.Fatalf("task SourceEndpointARN = %q, want %q", task.SourceEndpointARN, endpointARN)
	}
	if task.ReplicationInstanceARN != instanceARN {
		t.Fatalf("task ReplicationInstanceARN = %q, want %q", task.ReplicationInstanceARN, instanceARN)
	}
	if task.MigrationType != "full-load-and-cdc" {
		t.Fatalf("task MigrationType = %q, want full-load-and-cdc", task.MigrationType)
	}
}

func TestClientPaginatesInstancesByMarker(t *testing.T) {
	api := &fakeDMSAPI{
		instancePages: []*awsdms.DescribeReplicationInstancesOutput{
			{
				ReplicationInstances: []awsdmstypes.ReplicationInstance{{
					ReplicationInstanceArn: aws.String("arn:aws:dms:us-east-1:123456789012:rep:PAGEONEINSTANCEAA"),
				}},
				Marker: aws.String("next"),
			},
			{
				ReplicationInstances: []awsdmstypes.ReplicationInstance{{
					ReplicationInstanceArn: aws.String("arn:aws:dms:us-east-1:123456789012:rep:PAGETWOINSTANCEBB"),
				}},
			},
		},
	}
	client := &Client{client: api, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if len(snapshot.ReplicationInstances) != 2 {
		t.Fatalf("len(ReplicationInstances) = %d, want 2 across pages", len(snapshot.ReplicationInstances))
	}
}

func pagedSubnetGroups(groups []awsdmstypes.ReplicationSubnetGroup) []*awsdms.DescribeReplicationSubnetGroupsOutput {
	return []*awsdms.DescribeReplicationSubnetGroupsOutput{{ReplicationSubnetGroups: groups}}
}

func pagedInstances(instances []awsdmstypes.ReplicationInstance) []*awsdms.DescribeReplicationInstancesOutput {
	return []*awsdms.DescribeReplicationInstancesOutput{{ReplicationInstances: instances}}
}

func pagedEndpoints(endpoints []awsdmstypes.Endpoint) []*awsdms.DescribeEndpointsOutput {
	return []*awsdms.DescribeEndpointsOutput{{Endpoints: endpoints}}
}

func pagedTasks(tasks []awsdmstypes.ReplicationTask) []*awsdms.DescribeReplicationTasksOutput {
	return []*awsdms.DescribeReplicationTasksOutput{{ReplicationTasks: tasks}}
}

type fakeDMSAPI struct {
	subnetGroupPages []*awsdms.DescribeReplicationSubnetGroupsOutput
	subnetGroupCall  int
	instancePages    []*awsdms.DescribeReplicationInstancesOutput
	instanceCall     int
	endpointPages    []*awsdms.DescribeEndpointsOutput
	endpointCall     int
	taskPages        []*awsdms.DescribeReplicationTasksOutput
	taskCall         int
	tags             map[string][]awsdmstypes.Tag
}

func (f *fakeDMSAPI) DescribeReplicationSubnetGroups(
	_ context.Context,
	_ *awsdms.DescribeReplicationSubnetGroupsInput,
	_ ...func(*awsdms.Options),
) (*awsdms.DescribeReplicationSubnetGroupsOutput, error) {
	if f.subnetGroupCall >= len(f.subnetGroupPages) {
		return &awsdms.DescribeReplicationSubnetGroupsOutput{}, nil
	}
	page := f.subnetGroupPages[f.subnetGroupCall]
	f.subnetGroupCall++
	return page, nil
}

func (f *fakeDMSAPI) DescribeReplicationInstances(
	_ context.Context,
	_ *awsdms.DescribeReplicationInstancesInput,
	_ ...func(*awsdms.Options),
) (*awsdms.DescribeReplicationInstancesOutput, error) {
	if f.instanceCall >= len(f.instancePages) {
		return &awsdms.DescribeReplicationInstancesOutput{}, nil
	}
	page := f.instancePages[f.instanceCall]
	f.instanceCall++
	return page, nil
}

func (f *fakeDMSAPI) DescribeEndpoints(
	_ context.Context,
	_ *awsdms.DescribeEndpointsInput,
	_ ...func(*awsdms.Options),
) (*awsdms.DescribeEndpointsOutput, error) {
	if f.endpointCall >= len(f.endpointPages) {
		return &awsdms.DescribeEndpointsOutput{}, nil
	}
	page := f.endpointPages[f.endpointCall]
	f.endpointCall++
	return page, nil
}

func (f *fakeDMSAPI) DescribeReplicationTasks(
	_ context.Context,
	_ *awsdms.DescribeReplicationTasksInput,
	_ ...func(*awsdms.Options),
) (*awsdms.DescribeReplicationTasksOutput, error) {
	if f.taskCall >= len(f.taskPages) {
		return &awsdms.DescribeReplicationTasksOutput{}, nil
	}
	page := f.taskPages[f.taskCall]
	f.taskCall++
	return page, nil
}

func (f *fakeDMSAPI) ListTagsForResource(
	_ context.Context,
	input *awsdms.ListTagsForResourceInput,
	_ ...func(*awsdms.Options),
) (*awsdms.ListTagsForResourceOutput, error) {
	return &awsdms.ListTagsForResourceOutput{
		TagList: f.tags[aws.ToString(input.ResourceArn)],
	}, nil
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceDMS,
	}
}
