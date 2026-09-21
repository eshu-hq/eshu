// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsmgn "github.com/aws/aws-sdk-go-v2/service/mgn"
	awsmgntypes "github.com/aws/aws-sdk-go-v2/service/mgn/types"
	"github.com/aws/smithy-go"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestClientSnapshotsMGNMetadataOnly(t *testing.T) {
	const (
		appARN      = "arn:aws:mgn:us-east-1:123456789012:application/app-1"
		serverARN   = "arn:aws:mgn:us-east-1:123456789012:source-server/s-1"
		jobARN      = "arn:aws:mgn:us-east-1:123456789012:job/mgnjob-1"
		instanceID  = "i-0abc123def4567890"
		templateID  = "lt-0fedcba9876543210"
		serverID    = "s-1"
		appID       = "app-1"
		jobID       = "mgnjob-1"
		nextToken   = "page-2"
		serverIDTwo = "s-2"
	)

	api := &fakeMGNAPI{
		applicationPages: []*awsmgn.ListApplicationsOutput{{
			Items: []awsmgntypes.Application{{
				Arn:           aws.String(appARN),
				ApplicationID: aws.String(appID),
				Name:          aws.String("payments"),
				WaveID:        aws.String("wave-1"),
				ApplicationAggregatedStatus: &awsmgntypes.ApplicationAggregatedStatus{
					HealthStatus:       awsmgntypes.ApplicationHealthStatusHealthy,
					ProgressStatus:     awsmgntypes.ApplicationProgressStatusInProgress,
					TotalSourceServers: 2,
				},
			}},
		}},
		serverPages: []*awsmgn.DescribeSourceServersOutput{
			{
				NextToken: aws.String(nextToken),
				Items: []awsmgntypes.SourceServer{{
					Arn:            aws.String(serverARN),
					SourceServerID: aws.String(serverID),
					ApplicationID:  aws.String(appID),
					LifeCycle:      &awsmgntypes.LifeCycle{State: awsmgntypes.LifeCycleStateReadyForCutover},
					DataReplicationInfo: &awsmgntypes.DataReplicationInfo{
						DataReplicationState: awsmgntypes.DataReplicationStateContinuous,
						// A replicator id is present on the wire but must never be mapped.
						ReplicatorId: aws.String("i-secretreplicator"),
					},
					LaunchedInstance: &awsmgntypes.LaunchedInstance{Ec2InstanceID: aws.String(instanceID)},
					SourceProperties: &awsmgntypes.SourceProperties{
						RecommendedInstanceType: aws.String("m5.large"),
						Os:                      &awsmgntypes.OS{FullString: aws.String("Ubuntu 22.04")},
						IdentificationHints:     &awsmgntypes.IdentificationHints{Hostname: aws.String("web01")},
					},
				}},
			},
			{
				Items: []awsmgntypes.SourceServer{{
					Arn:            aws.String("arn:aws:mgn:us-east-1:123456789012:source-server/s-2"),
					SourceServerID: aws.String(serverIDTwo),
				}},
			},
		},
		launchConfigs: map[string]*awsmgn.GetLaunchConfigurationOutput{
			serverID: {
				Name:                aws.String("web01-launch"),
				LaunchDisposition:   awsmgntypes.LaunchDispositionStarted,
				BootMode:            awsmgntypes.BootModeLegacyBios,
				Ec2LaunchTemplateID: aws.String(templateID),
				CopyTags:            aws.Bool(true),
			},
			// serverIDTwo has no launch configuration: a not-found error.
		},
		jobPages: []*awsmgn.DescribeJobsOutput{{
			Items: []awsmgntypes.Job{{
				Arn:                  aws.String(jobARN),
				JobID:                aws.String(jobID),
				Type:                 awsmgntypes.JobTypeLaunch,
				Status:               awsmgntypes.JobStatusCompleted,
				InitiatedBy:          awsmgntypes.InitiatedByStartCutover,
				ParticipatingServers: []awsmgntypes.ParticipatingServer{{SourceServerID: aws.String(serverID)}},
			}},
		}},
	}

	client := &Client{client: api, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}

	if len(snapshot.Applications) != 1 {
		t.Fatalf("len(Applications) = %d, want 1", len(snapshot.Applications))
	}
	app := snapshot.Applications[0]
	if app.HealthStatus != "HEALTHY" || app.TotalSourceServers != 2 {
		t.Fatalf("application status = %q/%d, want HEALTHY/2", app.HealthStatus, app.TotalSourceServers)
	}

	if len(snapshot.SourceServers) != 2 {
		t.Fatalf("len(SourceServers) = %d, want 2 (pagination must follow NextToken)", len(snapshot.SourceServers))
	}
	server := snapshot.SourceServers[0]
	if server.LifeCycleState != "READY_FOR_CUTOVER" {
		t.Fatalf("server lifecycle = %q, want READY_FOR_CUTOVER", server.LifeCycleState)
	}
	if server.DataReplicationState != "CONTINUOUS" {
		t.Fatalf("server replication state = %q, want CONTINUOUS", server.DataReplicationState)
	}
	if server.LaunchedEC2InstanceID != instanceID {
		t.Fatalf("server launched instance = %q, want %q", server.LaunchedEC2InstanceID, instanceID)
	}
	if server.RecommendedInstanceType != "m5.large" || server.Hostname != "web01" {
		t.Fatalf("server props = %q/%q, want m5.large/web01", server.RecommendedInstanceType, server.Hostname)
	}
	if server.LaunchConfiguration == nil || server.LaunchConfiguration.EC2LaunchTemplateID != templateID {
		t.Fatalf("server launch config = %#v, want template %q", server.LaunchConfiguration, templateID)
	}
	if snapshot.SourceServers[1].LaunchConfiguration != nil {
		t.Fatalf("server 2 launch config = %#v, want nil for not-found", snapshot.SourceServers[1].LaunchConfiguration)
	}

	if len(snapshot.Jobs) != 1 {
		t.Fatalf("len(Jobs) = %d, want 1", len(snapshot.Jobs))
	}
	job := snapshot.Jobs[0]
	if len(job.ParticipatingSourceServerIDs) != 1 || job.ParticipatingSourceServerIDs[0] != serverID {
		t.Fatalf("job participants = %#v, want [%q]", job.ParticipatingSourceServerIDs, serverID)
	}
}

type fakeMGNAPI struct {
	applicationPages []*awsmgn.ListApplicationsOutput
	applicationCall  int
	serverPages      []*awsmgn.DescribeSourceServersOutput
	serverCall       int
	launchConfigs    map[string]*awsmgn.GetLaunchConfigurationOutput
	jobPages         []*awsmgn.DescribeJobsOutput
	jobCall          int
}

func (f *fakeMGNAPI) ListApplications(
	_ context.Context,
	_ *awsmgn.ListApplicationsInput,
	_ ...func(*awsmgn.Options),
) (*awsmgn.ListApplicationsOutput, error) {
	if f.applicationCall >= len(f.applicationPages) {
		return &awsmgn.ListApplicationsOutput{}, nil
	}
	page := f.applicationPages[f.applicationCall]
	f.applicationCall++
	return page, nil
}

func (f *fakeMGNAPI) DescribeSourceServers(
	_ context.Context,
	_ *awsmgn.DescribeSourceServersInput,
	_ ...func(*awsmgn.Options),
) (*awsmgn.DescribeSourceServersOutput, error) {
	if f.serverCall >= len(f.serverPages) {
		return &awsmgn.DescribeSourceServersOutput{}, nil
	}
	page := f.serverPages[f.serverCall]
	f.serverCall++
	return page, nil
}

func (f *fakeMGNAPI) GetLaunchConfiguration(
	_ context.Context,
	input *awsmgn.GetLaunchConfigurationInput,
	_ ...func(*awsmgn.Options),
) (*awsmgn.GetLaunchConfigurationOutput, error) {
	output, ok := f.launchConfigs[aws.ToString(input.SourceServerID)]
	if !ok {
		return nil, &smithy.GenericAPIError{Code: "ResourceNotFoundException", Message: "no launch configuration"}
	}
	return output, nil
}

func (f *fakeMGNAPI) DescribeJobs(
	_ context.Context,
	_ *awsmgn.DescribeJobsInput,
	_ ...func(*awsmgn.Options),
) (*awsmgn.DescribeJobsOutput, error) {
	if f.jobCall >= len(f.jobPages) {
		return &awsmgn.DescribeJobsOutput{}, nil
	}
	page := f.jobPages[f.jobCall]
	f.jobCall++
	return page, nil
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceMGN,
	}
}
