// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsdatasync "github.com/aws/aws-sdk-go-v2/service/datasync"
	awsdatasynctypes "github.com/aws/aws-sdk-go-v2/service/datasync/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// TestAPIClientInterfaceExcludesMutationAndUnsafeReadAPIs is the primary contract
// guard. apiClient is the single seam between the DataSync adapter and the AWS
// SDK client (Client.client is typed as apiClient, pinned by
// var _ apiClient = (*awsdatasync.Client)(nil) in client.go), so any SDK method
// the adapter could call must be listed here. A regression that added a
// create/start/update/delete API would either fail to compile against this
// interface or trip this shape assertion.
func TestAPIClientInterfaceExcludesMutationAndUnsafeReadAPIs(t *testing.T) {
	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	want := map[string]bool{
		"ListTasks":                  true,
		"DescribeTask":               true,
		"ListLocations":              true,
		"DescribeLocationS3":         true,
		"DescribeLocationEfs":        true,
		"DescribeLocationFsxLustre":  true,
		"DescribeLocationFsxOntap":   true,
		"DescribeLocationFsxOpenZfs": true,
		"DescribeLocationFsxWindows": true,
		"ListAgents":                 true,
		"DescribeAgent":              true,
	}
	have := map[string]bool{}
	for i := 0; i < ifaceType.NumMethod(); i++ {
		have[ifaceType.Method(i).Name] = true
	}
	for name := range want {
		if !have[name] {
			t.Errorf("apiClient missing required metadata-read method %q", name)
		}
	}
	for name := range have {
		if !want[name] {
			t.Errorf("apiClient exposes unexpected method %q; metadata-only contract violated", name)
		}
	}

	forbiddenSubstrings := []string{
		"Create", "Update", "Delete", "Start", "Stop", "Cancel", "Put",
		"Add", "Remove", "Tag", "Untag", "Execution",
	}
	for name := range have {
		for _, forbidden := range forbiddenSubstrings {
			if strings.Contains(name, forbidden) {
				t.Errorf("apiClient method %q contains forbidden substring %q", name, forbidden)
			}
		}
	}
}

func TestClientListTasksResolvesTaskMetadata(t *testing.T) {
	taskARN := "arn:aws:datasync:us-east-1:123456789012:task/task-0"
	fake := &fakeDataSyncAPI{
		tasks: []awsdatasynctypes.TaskListEntry{{TaskArn: aws.String(taskARN)}},
		describeTask: &awsdatasync.DescribeTaskOutput{
			TaskArn:                aws.String(taskARN),
			Name:                   aws.String("nightly"),
			Status:                 awsdatasynctypes.TaskStatusAvailable,
			SourceLocationArn:      aws.String("arn:aws:datasync:us-east-1:123456789012:location/loc-s3"),
			DestinationLocationArn: aws.String("arn:aws:datasync:us-east-1:123456789012:location/loc-efs"),
			CloudWatchLogGroupArn:  aws.String("arn:aws:logs:us-east-1:123456789012:log-group:/aws/datasync"),
			Schedule:               &awsdatasynctypes.TaskSchedule{ScheduleExpression: aws.String("cron(0 2 * * ? *)")},
		},
	}
	client := newTestClient(fake)

	tasks, err := client.ListTasks(context.Background())
	if err != nil {
		t.Fatalf("ListTasks() error = %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("ListTasks() len = %d, want 1", len(tasks))
	}
	task := tasks[0]
	if task.ARN != taskARN {
		t.Fatalf("task ARN = %q, want %q", task.ARN, taskARN)
	}
	if task.Status != "AVAILABLE" {
		t.Fatalf("task Status = %q, want AVAILABLE", task.Status)
	}
	if task.ScheduleExpression != "cron(0 2 * * ? *)" {
		t.Fatalf("task ScheduleExpression = %q", task.ScheduleExpression)
	}
}

func TestClientListLocationsResolvesBackingIdentity(t *testing.T) {
	s3ARN := "arn:aws:datasync:us-east-1:123456789012:location/loc-s3"
	efsARN := "arn:aws:datasync:us-east-1:123456789012:location/loc-efs"
	ontapARN := "arn:aws:datasync:us-east-1:123456789012:location/loc-ontap"
	fsxARN := "arn:aws:fsx:us-east-1:123456789012:file-system/fs-0ontap"
	fake := &fakeDataSyncAPI{
		locations: []awsdatasynctypes.LocationListEntry{
			{LocationArn: aws.String(s3ARN), LocationUri: aws.String("s3://archive/incoming/")},
			{LocationArn: aws.String(efsARN), LocationUri: aws.String("efs://us-east-1.fs-0123/backups/")},
			{LocationArn: aws.String(ontapARN), LocationUri: aws.String("fsxn://us-east-1.fs-0ontap/vol1/")},
		},
		describeS3: &awsdatasync.DescribeLocationS3Output{
			LocationUri: aws.String("s3://archive/incoming/"),
			S3Config:    &awsdatasynctypes.S3Config{BucketAccessRoleArn: aws.String("arn:aws:iam::123456789012:role/datasync-s3")},
		},
		describeEFS: &awsdatasync.DescribeLocationEfsOutput{
			LocationUri:             aws.String("efs://us-east-1.fs-0123/backups/"),
			FileSystemAccessRoleArn: aws.String("arn:aws:iam::123456789012:role/datasync-efs"),
		},
		describeOntap: &awsdatasync.DescribeLocationFsxOntapOutput{
			LocationUri:      aws.String("fsxn://us-east-1.fs-0ontap/vol1/"),
			FsxFilesystemArn: aws.String(fsxARN),
		},
	}
	client := newTestClient(fake)

	locations, err := client.ListLocations(context.Background())
	if err != nil {
		t.Fatalf("ListLocations() error = %v", err)
	}
	if len(locations) != 3 {
		t.Fatalf("ListLocations() len = %d, want 3", len(locations))
	}
	byARN := map[string]int{}
	for i, location := range locations {
		byARN[location.ARN] = i
	}

	s3 := locations[byARN[s3ARN]]
	if s3.S3BucketName != "archive" {
		t.Fatalf("s3 bucket = %q, want archive", s3.S3BucketName)
	}
	if s3.IAMRoleARN != "arn:aws:iam::123456789012:role/datasync-s3" {
		t.Fatalf("s3 IAMRoleARN = %q", s3.IAMRoleARN)
	}

	efs := locations[byARN[efsARN]]
	if efs.EFSFileSystemID != "fs-0123" {
		t.Fatalf("efs file system id = %q, want fs-0123", efs.EFSFileSystemID)
	}
	if efs.IAMRoleARN != "arn:aws:iam::123456789012:role/datasync-efs" {
		t.Fatalf("efs IAMRoleARN = %q", efs.IAMRoleARN)
	}

	ontap := locations[byARN[ontapARN]]
	if ontap.FSxFileSystemARN != fsxARN {
		t.Fatalf("ontap FSxFileSystemARN = %q, want %q", ontap.FSxFileSystemARN, fsxARN)
	}
}

func TestClientListAgentsResolvesAgentMetadata(t *testing.T) {
	agentARN := "arn:aws:datasync:us-east-1:123456789012:agent/agent-0"
	fake := &fakeDataSyncAPI{
		agents: []awsdatasynctypes.AgentListEntry{{AgentArn: aws.String(agentARN), Name: aws.String("a")}},
		describeAgent: &awsdatasync.DescribeAgentOutput{
			AgentArn:     aws.String(agentARN),
			Name:         aws.String("on-prem"),
			Status:       awsdatasynctypes.AgentStatusOnline,
			EndpointType: awsdatasynctypes.EndpointTypePublic,
			Platform:     &awsdatasynctypes.Platform{Version: aws.String("1.2.3")},
		},
	}
	client := newTestClient(fake)

	agents, err := client.ListAgents(context.Background())
	if err != nil {
		t.Fatalf("ListAgents() error = %v", err)
	}
	if len(agents) != 1 {
		t.Fatalf("ListAgents() len = %d, want 1", len(agents))
	}
	agent := agents[0]
	if agent.Status != "ONLINE" || agent.EndpointType != "PUBLIC" || agent.PlatformVersion != "1.2.3" {
		t.Fatalf("agent metadata = %+v", agent)
	}
}

func newTestClient(fake *fakeDataSyncAPI) *Client {
	return &Client{
		client:   fake,
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceDataSync},
	}
}

type fakeDataSyncAPI struct {
	tasks         []awsdatasynctypes.TaskListEntry
	locations     []awsdatasynctypes.LocationListEntry
	agents        []awsdatasynctypes.AgentListEntry
	describeTask  *awsdatasync.DescribeTaskOutput
	describeS3    *awsdatasync.DescribeLocationS3Output
	describeEFS   *awsdatasync.DescribeLocationEfsOutput
	describeOntap *awsdatasync.DescribeLocationFsxOntapOutput
	describeAgent *awsdatasync.DescribeAgentOutput
}

func (f *fakeDataSyncAPI) ListTasks(context.Context, *awsdatasync.ListTasksInput, ...func(*awsdatasync.Options)) (*awsdatasync.ListTasksOutput, error) {
	return &awsdatasync.ListTasksOutput{Tasks: f.tasks}, nil
}

func (f *fakeDataSyncAPI) DescribeTask(context.Context, *awsdatasync.DescribeTaskInput, ...func(*awsdatasync.Options)) (*awsdatasync.DescribeTaskOutput, error) {
	return f.describeTask, nil
}

func (f *fakeDataSyncAPI) ListLocations(context.Context, *awsdatasync.ListLocationsInput, ...func(*awsdatasync.Options)) (*awsdatasync.ListLocationsOutput, error) {
	return &awsdatasync.ListLocationsOutput{Locations: f.locations}, nil
}

func (f *fakeDataSyncAPI) DescribeLocationS3(context.Context, *awsdatasync.DescribeLocationS3Input, ...func(*awsdatasync.Options)) (*awsdatasync.DescribeLocationS3Output, error) {
	return f.describeS3, nil
}

func (f *fakeDataSyncAPI) DescribeLocationEfs(context.Context, *awsdatasync.DescribeLocationEfsInput, ...func(*awsdatasync.Options)) (*awsdatasync.DescribeLocationEfsOutput, error) {
	return f.describeEFS, nil
}

func (f *fakeDataSyncAPI) DescribeLocationFsxLustre(context.Context, *awsdatasync.DescribeLocationFsxLustreInput, ...func(*awsdatasync.Options)) (*awsdatasync.DescribeLocationFsxLustreOutput, error) {
	return &awsdatasync.DescribeLocationFsxLustreOutput{}, nil
}

func (f *fakeDataSyncAPI) DescribeLocationFsxOntap(context.Context, *awsdatasync.DescribeLocationFsxOntapInput, ...func(*awsdatasync.Options)) (*awsdatasync.DescribeLocationFsxOntapOutput, error) {
	return f.describeOntap, nil
}

func (f *fakeDataSyncAPI) DescribeLocationFsxOpenZfs(context.Context, *awsdatasync.DescribeLocationFsxOpenZfsInput, ...func(*awsdatasync.Options)) (*awsdatasync.DescribeLocationFsxOpenZfsOutput, error) {
	return &awsdatasync.DescribeLocationFsxOpenZfsOutput{}, nil
}

func (f *fakeDataSyncAPI) DescribeLocationFsxWindows(context.Context, *awsdatasync.DescribeLocationFsxWindowsInput, ...func(*awsdatasync.Options)) (*awsdatasync.DescribeLocationFsxWindowsOutput, error) {
	return &awsdatasync.DescribeLocationFsxWindowsOutput{}, nil
}

func (f *fakeDataSyncAPI) ListAgents(context.Context, *awsdatasync.ListAgentsInput, ...func(*awsdatasync.Options)) (*awsdatasync.ListAgentsOutput, error) {
	return &awsdatasync.ListAgentsOutput{Agents: f.agents}, nil
}

func (f *fakeDataSyncAPI) DescribeAgent(context.Context, *awsdatasync.DescribeAgentInput, ...func(*awsdatasync.Options)) (*awsdatasync.DescribeAgentOutput, error) {
	return f.describeAgent, nil
}
