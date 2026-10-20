// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsefs "github.com/aws/aws-sdk-go-v2/service/efs"
	awsefstypes "github.com/aws/aws-sdk-go-v2/service/efs/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// TestAPIClientInterfaceExcludesMutationAndPolicyAPIs is the primary contract
// guard for issue #734. apiClient is the single seam between the EFS adapter
// and the AWS SDK client (Client.client is typed as apiClient, pinned by
// var _ apiClient = (*awsefs.Client)(nil) in client.go), so any SDK method the
// adapter could call must be listed here. A regression that added a mutation
// API (Create/Delete/Put/Update) or an NFS-policy read would either fail to
// compile against this interface or trip this shape assertion.
func TestAPIClientInterfaceExcludesMutationAndPolicyAPIs(t *testing.T) {
	ifaceType := reflect.TypeOf((*apiClient)(nil)).Elem()
	want := map[string]bool{
		"DescribeFileSystems":               true,
		"DescribeAccessPoints":              true,
		"DescribeMountTargets":              true,
		"DescribeMountTargetSecurityGroups": true,
		"DescribeLifecycleConfiguration":    true,
		"DescribeReplicationConfigurations": true,
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

	// Defensive check: no method on the SDK seam may name a forbidden mutation
	// API or an NFS file system policy read/write. Mirrors the issue #734
	// forbidden-API language.
	forbiddenSubstrings := []string{
		"Create", "Update", "Delete", "Put", "Modify", "Tag", "Untag",
		"FileSystemPolicy", "BackupPolicy",
	}
	for name := range have {
		for _, forbidden := range forbiddenSubstrings {
			if strings.Contains(name, forbidden) {
				t.Errorf("apiClient method %q contains forbidden substring %q", name, forbidden)
			}
		}
	}
}

func TestClientListFileSystemsReadsMetadataAndChildResources(t *testing.T) {
	fsID := "fs-01234567"
	fsARN := "arn:aws:elasticfilesystem:us-east-1:123456789012:file-system/fs-01234567"
	fake := &fakeEFSAPI{
		fileSystems: []awsefstypes.FileSystemDescription{{
			FileSystemId:    aws.String(fsID),
			FileSystemArn:   aws.String(fsARN),
			Name:            aws.String("prod-data"),
			OwnerId:         aws.String("123456789012"),
			LifeCycleState:  awsefstypes.LifeCycleStateAvailable,
			PerformanceMode: awsefstypes.PerformanceModeGeneralPurpose,
			ThroughputMode:  awsefstypes.ThroughputModeBursting,
			Encrypted:       aws.Bool(true),
			KmsKeyId:        aws.String("arn:aws:kms:us-east-1:123456789012:key/abcd"),
			Tags: []awsefstypes.Tag{
				{Key: aws.String("Environment"), Value: aws.String("prod")},
			},
		}},
		accessPoints: map[string][]awsefstypes.AccessPointDescription{
			fsID: {{
				AccessPointId:  aws.String("fsap-0001"),
				AccessPointArn: aws.String("arn:aws:elasticfilesystem:us-east-1:123456789012:access-point/fsap-0001"),
				Name:           aws.String("app-ap"),
				FileSystemId:   aws.String(fsID),
				LifeCycleState: awsefstypes.LifeCycleStateAvailable,
				RootDirectory:  &awsefstypes.RootDirectory{Path: aws.String("/app")},
				PosixUser:      &awsefstypes.PosixUser{Uid: aws.Int64(1000), Gid: aws.Int64(1000)},
			}},
		},
		mountTargets: map[string][]awsefstypes.MountTargetDescription{
			fsID: {{
				MountTargetId:  aws.String("fsmt-0001"),
				FileSystemId:   aws.String(fsID),
				SubnetId:       aws.String("subnet-aaa"),
				VpcId:          aws.String("vpc-bbb"),
				LifeCycleState: awsefstypes.LifeCycleStateAvailable,
				IpAddress:      aws.String("10.0.0.5"),
			}},
		},
		securityGroups: map[string][]string{
			"fsmt-0001": {"sg-111", "sg-222"},
		},
		lifecyclePolicies: map[string][]awsefstypes.LifecyclePolicy{
			fsID: {{TransitionToIA: awsefstypes.TransitionToIARulesAfter30Days}},
		},
	}
	adapter := &Client{
		client:   fake,
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceEFS},
	}

	systems, err := adapter.ListFileSystems(context.Background())
	if err != nil {
		t.Fatalf("ListFileSystems() error = %v, want nil", err)
	}
	if got, want := len(systems), 1; got != want {
		t.Fatalf("len(systems) = %d, want %d", got, want)
	}
	fs := systems[0]
	if fs.ID != fsID || fs.ARN != fsARN {
		t.Fatalf("file system identity = %q/%q", fs.ID, fs.ARN)
	}
	if fs.PerformanceMode != "generalPurpose" {
		t.Fatalf("PerformanceMode = %q, want generalPurpose", fs.PerformanceMode)
	}
	if fs.ThroughputMode != "bursting" {
		t.Fatalf("ThroughputMode = %q, want bursting", fs.ThroughputMode)
	}
	if !fs.Encrypted {
		t.Fatalf("Encrypted = false, want true")
	}
	if fs.KMSKeyID != "arn:aws:kms:us-east-1:123456789012:key/abcd" {
		t.Fatalf("KMSKeyID = %q", fs.KMSKeyID)
	}
	if fs.Tags["Environment"] != "prod" {
		t.Fatalf("Tags = %#v, want Environment=prod", fs.Tags)
	}
	if fs.LifecyclePolicy.TransitionToIA != "AFTER_30_DAYS" {
		t.Fatalf("LifecyclePolicy.TransitionToIA = %q, want AFTER_30_DAYS", fs.LifecyclePolicy.TransitionToIA)
	}
	if got, want := len(fs.AccessPoints), 1; got != want {
		t.Fatalf("len(AccessPoints) = %d, want %d", got, want)
	}
	if fs.AccessPoints[0].RootDirectory != "/app" {
		t.Fatalf("AccessPoint RootDirectory = %q, want /app", fs.AccessPoints[0].RootDirectory)
	}
	if got, want := len(fs.MountTargets), 1; got != want {
		t.Fatalf("len(MountTargets) = %d, want %d", got, want)
	}
	mt := fs.MountTargets[0]
	if mt.SubnetID != "subnet-aaa" || mt.VPCID != "vpc-bbb" {
		t.Fatalf("MountTarget network = %q/%q", mt.SubnetID, mt.VPCID)
	}
	if got, want := mt.SecurityGroupIDs, []string{"sg-111", "sg-222"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("SecurityGroupIDs = %#v, want %#v", got, want)
	}
}

func TestClientListReplicationConfigurationsRecordsDestinations(t *testing.T) {
	fake := &fakeEFSAPI{
		replications: []awsefstypes.ReplicationConfigurationDescription{{
			SourceFileSystemId:  aws.String("fs-source"),
			SourceFileSystemArn: aws.String("arn:aws:elasticfilesystem:us-east-1:123456789012:file-system/fs-source"),
			Destinations: []awsefstypes.Destination{{
				FileSystemId: aws.String("fs-dest"),
				Region:       aws.String("us-west-2"),
				Status:       awsefstypes.ReplicationStatusEnabled,
			}},
		}},
	}
	adapter := &Client{
		client:   fake,
		boundary: awscloud.Boundary{AccountID: "123456789012", Region: "us-east-1", ServiceKind: awscloud.ServiceEFS},
	}

	configs, err := adapter.ListReplicationConfigurations(context.Background())
	if err != nil {
		t.Fatalf("ListReplicationConfigurations() error = %v, want nil", err)
	}
	if got, want := len(configs), 1; got != want {
		t.Fatalf("len(configs) = %d, want %d", got, want)
	}
	cfg := configs[0]
	if cfg.SourceFileSystemID != "fs-source" {
		t.Fatalf("SourceFileSystemID = %q, want fs-source", cfg.SourceFileSystemID)
	}
	if got, want := len(cfg.Destinations), 1; got != want {
		t.Fatalf("len(Destinations) = %d, want %d", got, want)
	}
	if cfg.Destinations[0].FileSystemID != "fs-dest" {
		t.Fatalf("Destination FileSystemID = %q, want fs-dest", cfg.Destinations[0].FileSystemID)
	}
	if cfg.Destinations[0].Region != "us-west-2" {
		t.Fatalf("Destination Region = %q, want us-west-2", cfg.Destinations[0].Region)
	}
}

type fakeEFSAPI struct {
	fileSystems       []awsefstypes.FileSystemDescription
	accessPoints      map[string][]awsefstypes.AccessPointDescription
	mountTargets      map[string][]awsefstypes.MountTargetDescription
	securityGroups    map[string][]string
	lifecyclePolicies map[string][]awsefstypes.LifecyclePolicy
	replications      []awsefstypes.ReplicationConfigurationDescription
}

func (f *fakeEFSAPI) DescribeFileSystems(
	_ context.Context,
	_ *awsefs.DescribeFileSystemsInput,
	_ ...func(*awsefs.Options),
) (*awsefs.DescribeFileSystemsOutput, error) {
	return &awsefs.DescribeFileSystemsOutput{FileSystems: f.fileSystems}, nil
}

func (f *fakeEFSAPI) DescribeAccessPoints(
	_ context.Context,
	input *awsefs.DescribeAccessPointsInput,
	_ ...func(*awsefs.Options),
) (*awsefs.DescribeAccessPointsOutput, error) {
	return &awsefs.DescribeAccessPointsOutput{AccessPoints: f.accessPoints[aws.ToString(input.FileSystemId)]}, nil
}

func (f *fakeEFSAPI) DescribeMountTargets(
	_ context.Context,
	input *awsefs.DescribeMountTargetsInput,
	_ ...func(*awsefs.Options),
) (*awsefs.DescribeMountTargetsOutput, error) {
	return &awsefs.DescribeMountTargetsOutput{MountTargets: f.mountTargets[aws.ToString(input.FileSystemId)]}, nil
}

func (f *fakeEFSAPI) DescribeMountTargetSecurityGroups(
	_ context.Context,
	input *awsefs.DescribeMountTargetSecurityGroupsInput,
	_ ...func(*awsefs.Options),
) (*awsefs.DescribeMountTargetSecurityGroupsOutput, error) {
	return &awsefs.DescribeMountTargetSecurityGroupsOutput{SecurityGroups: f.securityGroups[aws.ToString(input.MountTargetId)]}, nil
}

func (f *fakeEFSAPI) DescribeLifecycleConfiguration(
	_ context.Context,
	input *awsefs.DescribeLifecycleConfigurationInput,
	_ ...func(*awsefs.Options),
) (*awsefs.DescribeLifecycleConfigurationOutput, error) {
	return &awsefs.DescribeLifecycleConfigurationOutput{LifecyclePolicies: f.lifecyclePolicies[aws.ToString(input.FileSystemId)]}, nil
}

func (f *fakeEFSAPI) DescribeReplicationConfigurations(
	_ context.Context,
	_ *awsefs.DescribeReplicationConfigurationsInput,
	_ ...func(*awsefs.Options),
) (*awsefs.DescribeReplicationConfigurationsOutput, error) {
	return &awsefs.DescribeReplicationConfigurationsOutput{Replications: f.replications}, nil
}

var _ apiClient = (*fakeEFSAPI)(nil)
