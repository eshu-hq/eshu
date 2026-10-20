// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsdrs "github.com/aws/aws-sdk-go-v2/service/drs"
	awsdrstypes "github.com/aws/aws-sdk-go-v2/service/drs/types"
	"github.com/aws/smithy-go"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestClientSnapshotsDRSMetadataOnly(t *testing.T) {
	api := &fakeDRSAPI{
		sourceServerPages: []*awsdrs.DescribeSourceServersOutput{
			{
				Items: []awsdrstypes.SourceServer{{
					SourceServerID:     aws.String("s-1234567890abcdef0"),
					Arn:                aws.String("arn:aws:drs:us-east-1:123456789012:source-server/s-1234567890abcdef0"),
					RecoveryInstanceId: aws.String("i-0fedcba9876543210"),
					DataReplicationInfo: &awsdrstypes.DataReplicationInfo{
						DataReplicationState: awsdrstypes.DataReplicationStateContinuous,
					},
					SourceProperties: &awsdrstypes.SourceProperties{
						RecommendedInstanceType: aws.String("m5.large"),
						Os:                      &awsdrstypes.OS{FullString: aws.String("Ubuntu 22.04")},
						IdentificationHints: &awsdrstypes.IdentificationHints{
							Hostname: aws.String("web-01"),
							Fqdn:     aws.String("web-01.example.com"),
						},
					},
					SourceCloudProperties: &awsdrstypes.SourceCloudProperties{
						OriginAccountID: aws.String("123456789012"),
						OriginRegion:    aws.String("us-west-2"),
					},
					Tags: map[string]string{"Environment": "prod"},
				}},
				NextToken: aws.String("page2"),
			},
			{
				Items: []awsdrstypes.SourceServer{{
					SourceServerID: aws.String("s-aaaaaaaaaaaaaaaaa"),
					Arn:            aws.String("arn:aws:drs:us-east-1:123456789012:source-server/s-aaaaaaaaaaaaaaaaa"),
				}},
			},
		},
		recoveryInstancePages: []*awsdrs.DescribeRecoveryInstancesOutput{{
			Items: []awsdrstypes.RecoveryInstance{{
				RecoveryInstanceID: aws.String("i-0fedcba9876543210"),
				Arn:                aws.String("arn:aws:drs:us-east-1:123456789012:recovery-instance/i-0fedcba9876543210"),
				Ec2InstanceID:      aws.String("i-0123456789abcdef0"),
				Ec2InstanceState:   awsdrstypes.EC2InstanceStateRunning,
				SourceServerID:     aws.String("s-1234567890abcdef0"),
				IsDrill:            aws.Bool(true),
				OriginEnvironment:  awsdrstypes.OriginEnvironmentOnPremises,
				Tags:               map[string]string{"Team": "dr"},
			}},
		}},
		templatePages: []*awsdrs.DescribeReplicationConfigurationTemplatesOutput{{
			Items: []awsdrstypes.ReplicationConfigurationTemplate{{
				ReplicationConfigurationTemplateID: aws.String("rct-0123456789abcdef0"),
				Arn:                                aws.String("arn:aws:drs:us-east-1:123456789012:replication-configuration-template/rct-0123456789abcdef0"),
				EbsEncryption:                      awsdrstypes.ReplicationConfigurationEbsEncryptionDefault,
				StagingAreaSubnetId:                aws.String("subnet-0abc1234"),
				ReplicationServerInstanceType:      aws.String("t3.small"),
				UseDedicatedReplicationServer:      aws.Bool(false),
				AssociateDefaultSecurityGroup:      aws.Bool(true),
				Tags:                               map[string]string{"Owner": "platform"},
			}},
		}},
	}

	client := &Client{client: api, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}

	if len(snapshot.SourceServers) != 2 {
		t.Fatalf("len(SourceServers) = %d, want 2 (pagination)", len(snapshot.SourceServers))
	}
	server := snapshot.SourceServers[0]
	if server.SourceServerID != "s-1234567890abcdef0" {
		t.Fatalf("SourceServerID = %q", server.SourceServerID)
	}
	if server.RecoveryInstanceID != "i-0fedcba9876543210" {
		t.Fatalf("RecoveryInstanceID = %q", server.RecoveryInstanceID)
	}
	if server.Hostname != "web-01" {
		t.Fatalf("Hostname = %q, want web-01", server.Hostname)
	}
	if server.OperatingSystem != "Ubuntu 22.04" {
		t.Fatalf("OperatingSystem = %q", server.OperatingSystem)
	}
	if server.RecommendedInstanceType != "m5.large" {
		t.Fatalf("RecommendedInstanceType = %q", server.RecommendedInstanceType)
	}
	if server.DataReplicationState != "CONTINUOUS" {
		t.Fatalf("DataReplicationState = %q, want CONTINUOUS", server.DataReplicationState)
	}
	if server.OriginRegion != "us-west-2" {
		t.Fatalf("OriginRegion = %q", server.OriginRegion)
	}
	if server.Tags["Environment"] != "prod" {
		t.Fatalf("tag Environment = %q", server.Tags["Environment"])
	}

	if len(snapshot.RecoveryInstances) != 1 {
		t.Fatalf("len(RecoveryInstances) = %d, want 1", len(snapshot.RecoveryInstances))
	}
	instance := snapshot.RecoveryInstances[0]
	if instance.EC2InstanceID != "i-0123456789abcdef0" {
		t.Fatalf("EC2InstanceID = %q", instance.EC2InstanceID)
	}
	if instance.EC2InstanceState != "RUNNING" {
		t.Fatalf("EC2InstanceState = %q, want RUNNING", instance.EC2InstanceState)
	}
	if !instance.IsDrill {
		t.Fatalf("IsDrill = false, want true")
	}
	if instance.OriginEnvironment != "ON_PREMISES" {
		t.Fatalf("OriginEnvironment = %q", instance.OriginEnvironment)
	}

	if len(snapshot.ReplicationConfigurationTemplates) != 1 {
		t.Fatalf("len(Templates) = %d, want 1", len(snapshot.ReplicationConfigurationTemplates))
	}
	template := snapshot.ReplicationConfigurationTemplates[0]
	if template.TemplateID != "rct-0123456789abcdef0" {
		t.Fatalf("TemplateID = %q", template.TemplateID)
	}
	if template.EBSEncryption != "DEFAULT" {
		t.Fatalf("EBSEncryption = %q, want DEFAULT", template.EBSEncryption)
	}
	if template.StagingAreaSubnetID != "subnet-0abc1234" {
		t.Fatalf("StagingAreaSubnetID = %q", template.StagingAreaSubnetID)
	}
	if !template.AssociateDefaultSecurityGroup {
		t.Fatalf("AssociateDefaultSecurityGroup = false, want true")
	}
}

func TestIsThrottleError(t *testing.T) {
	if isThrottleError(nil) {
		t.Fatalf("isThrottleError(nil) = true, want false")
	}
	throttle := &smithy.GenericAPIError{Code: "ThrottlingException", Message: "slow down"}
	if !isThrottleError(throttle) {
		t.Fatalf("isThrottleError(ThrottlingException) = false, want true")
	}
	other := &smithy.GenericAPIError{Code: "ValidationException", Message: "bad input"}
	if isThrottleError(other) {
		t.Fatalf("isThrottleError(ValidationException) = true, want false")
	}
}

type fakeDRSAPI struct {
	sourceServerPages     []*awsdrs.DescribeSourceServersOutput
	sourceServerCall      int
	recoveryInstancePages []*awsdrs.DescribeRecoveryInstancesOutput
	recoveryInstanceCall  int
	templatePages         []*awsdrs.DescribeReplicationConfigurationTemplatesOutput
	templateCall          int
}

func (f *fakeDRSAPI) DescribeSourceServers(
	_ context.Context,
	_ *awsdrs.DescribeSourceServersInput,
	_ ...func(*awsdrs.Options),
) (*awsdrs.DescribeSourceServersOutput, error) {
	if f.sourceServerCall >= len(f.sourceServerPages) {
		return &awsdrs.DescribeSourceServersOutput{}, nil
	}
	page := f.sourceServerPages[f.sourceServerCall]
	f.sourceServerCall++
	return page, nil
}

func (f *fakeDRSAPI) DescribeRecoveryInstances(
	_ context.Context,
	_ *awsdrs.DescribeRecoveryInstancesInput,
	_ ...func(*awsdrs.Options),
) (*awsdrs.DescribeRecoveryInstancesOutput, error) {
	if f.recoveryInstanceCall >= len(f.recoveryInstancePages) {
		return &awsdrs.DescribeRecoveryInstancesOutput{}, nil
	}
	page := f.recoveryInstancePages[f.recoveryInstanceCall]
	f.recoveryInstanceCall++
	return page, nil
}

func (f *fakeDRSAPI) DescribeReplicationConfigurationTemplates(
	_ context.Context,
	_ *awsdrs.DescribeReplicationConfigurationTemplatesInput,
	_ ...func(*awsdrs.Options),
) (*awsdrs.DescribeReplicationConfigurationTemplatesOutput, error) {
	if f.templateCall >= len(f.templatePages) {
		return &awsdrs.DescribeReplicationConfigurationTemplatesOutput{}, nil
	}
	page := f.templatePages[f.templateCall]
	f.templateCall++
	return page, nil
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceDRS,
	}
}
