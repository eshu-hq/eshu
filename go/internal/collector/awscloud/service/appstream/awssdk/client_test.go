// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsappstream "github.com/aws/aws-sdk-go-v2/service/appstream"
	awsappstreamtypes "github.com/aws/aws-sdk-go-v2/service/appstream/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestClientSnapshotsAppStreamMetadataOnly(t *testing.T) {
	fleetARN := "arn:aws:appstream:us-east-1:123456789012:fleet/sales-fleet"
	stackARN := "arn:aws:appstream:us-east-1:123456789012:stack/sales-stack"
	builderARN := "arn:aws:appstream:us-east-1:123456789012:image-builder/builder-1"
	imageARN := "arn:aws:appstream:us-east-1:123456789012:image/custom-image"
	roleARN := "arn:aws:iam::123456789012:role/appstream-machine-role"
	createdAt := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)

	api := &fakeAppStreamAPI{
		fleets: [][]awsappstreamtypes.Fleet{{{
			Arn:          aws.String(fleetARN),
			Name:         aws.String("sales-fleet"),
			State:        awsappstreamtypes.FleetStateRunning,
			FleetType:    awsappstreamtypes.FleetTypeOnDemand,
			InstanceType: aws.String("stream.standard.medium"),
			IamRoleArn:   aws.String(roleARN),
			ImageArn:     aws.String(imageARN),
			CreatedTime:  aws.Time(createdAt),
			VpcConfig: &awsappstreamtypes.VpcConfig{
				SubnetIds:        []string{"subnet-aaa", " ", "subnet-bbb"},
				SecurityGroupIds: []string{"sg-111"},
			},
		}}},
		stacks: [][]awsappstreamtypes.Stack{{{
			Arn:  aws.String(stackARN),
			Name: aws.String("sales-stack"),
			ApplicationSettings: &awsappstreamtypes.ApplicationSettingsResponse{
				Enabled:      aws.Bool(true),
				S3BucketName: aws.String("appstream-settings-bucket"),
			},
			StorageConnectors: []awsappstreamtypes.StorageConnector{
				{
					ConnectorType:      awsappstreamtypes.StorageConnectorTypeHomefolders,
					ResourceIdentifier: aws.String("appstream-home-folders"),
				},
				{
					ConnectorType: awsappstreamtypes.StorageConnectorTypeGoogleDrive,
					Domains:       []string{"example.com"},
				},
			},
		}}},
		builders: [][]awsappstreamtypes.ImageBuilder{{{
			Arn:          aws.String(builderARN),
			Name:         aws.String("builder-1"),
			State:        awsappstreamtypes.ImageBuilderStateRunning,
			InstanceType: aws.String("stream.standard.large"),
			IamRoleArn:   aws.String(roleARN),
			ImageArn:     aws.String(imageARN),
			VpcConfig: &awsappstreamtypes.VpcConfig{
				SubnetIds:        []string{"subnet-ccc"},
				SecurityGroupIds: []string{"sg-222"},
			},
		}}},
		imagesByType: map[awsappstreamtypes.VisibilityType][][]awsappstreamtypes.Image{
			awsappstreamtypes.VisibilityTypePrivate: {{{
				Arn:        aws.String(imageARN),
				Name:       aws.String("custom-image"),
				State:      awsappstreamtypes.ImageStateAvailable,
				Visibility: awsappstreamtypes.VisibilityTypePrivate,
				ImageType:  awsappstreamtypes.ImageTypeCustom,
			}}},
		},
		associatedStacks: map[string][][]string{
			"sales-fleet": {{"sales-stack"}},
		},
		tags: map[string]map[string]string{
			fleetARN: {"Environment": "prod"},
		},
	}

	client := &Client{client: api, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}

	if len(snapshot.Fleets) != 1 {
		t.Fatalf("len(Fleets) = %d, want 1", len(snapshot.Fleets))
	}
	fleet := snapshot.Fleets[0]
	if fleet.ARN != fleetARN {
		t.Fatalf("fleet ARN = %q, want %q", fleet.ARN, fleetARN)
	}
	if fleet.State != "RUNNING" {
		t.Fatalf("fleet State = %q, want RUNNING", fleet.State)
	}
	if fleet.IAMRoleARN != roleARN {
		t.Fatalf("fleet IAMRoleARN = %q, want %q", fleet.IAMRoleARN, roleARN)
	}
	if len(fleet.SubnetIDs) != 2 || fleet.SubnetIDs[0] != "subnet-aaa" || fleet.SubnetIDs[1] != "subnet-bbb" {
		t.Fatalf("fleet SubnetIDs = %#v, want [subnet-aaa subnet-bbb] (blank dropped)", fleet.SubnetIDs)
	}
	if fleet.Tags["Environment"] != "prod" {
		t.Fatalf("fleet tag Environment = %q, want prod", fleet.Tags["Environment"])
	}

	if len(snapshot.Stacks) != 1 {
		t.Fatalf("len(Stacks) = %d, want 1", len(snapshot.Stacks))
	}
	stack := snapshot.Stacks[0]
	if !stack.ApplicationSettingsEnabled {
		t.Fatalf("stack ApplicationSettingsEnabled = false, want true")
	}
	if stack.ApplicationSettingsS3Bucket != "appstream-settings-bucket" {
		t.Fatalf("stack ApplicationSettingsS3Bucket = %q, want appstream-settings-bucket", stack.ApplicationSettingsS3Bucket)
	}
	if len(stack.StorageConnectorBuckets) != 1 || stack.StorageConnectorBuckets[0] != "appstream-home-folders" {
		t.Fatalf("stack StorageConnectorBuckets = %#v, want [appstream-home-folders] (non-HOMEFOLDERS dropped)", stack.StorageConnectorBuckets)
	}

	if len(snapshot.ImageBuilders) != 1 {
		t.Fatalf("len(ImageBuilders) = %d, want 1", len(snapshot.ImageBuilders))
	}
	if snapshot.ImageBuilders[0].SecurityGroupIDs[0] != "sg-222" {
		t.Fatalf("builder SecurityGroupIDs = %#v, want [sg-222]", snapshot.ImageBuilders[0].SecurityGroupIDs)
	}

	if len(snapshot.Images) != 1 {
		t.Fatalf("len(Images) = %d, want 1", len(snapshot.Images))
	}
	if snapshot.Images[0].Visibility != "PRIVATE" {
		t.Fatalf("image Visibility = %q, want PRIVATE", snapshot.Images[0].Visibility)
	}

	if len(snapshot.FleetStackAssociations) != 1 {
		t.Fatalf("len(FleetStackAssociations) = %d, want 1", len(snapshot.FleetStackAssociations))
	}
	assoc := snapshot.FleetStackAssociations[0]
	if assoc.FleetName != "sales-fleet" || assoc.StackName != "sales-stack" {
		t.Fatalf("association = %#v, want {sales-fleet sales-stack}", assoc)
	}
}

func TestClientDescribeImagesScopesToPrivateAndShared(t *testing.T) {
	api := &fakeAppStreamAPI{}
	client := &Client{client: api, boundary: testBoundary()}
	if _, err := client.Snapshot(context.Background()); err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	want := []awsappstreamtypes.VisibilityType{
		awsappstreamtypes.VisibilityTypePrivate,
		awsappstreamtypes.VisibilityTypeShared,
	}
	if len(api.imageVisibilities) != len(want) {
		t.Fatalf("DescribeImages visibilities = %#v, want %#v", api.imageVisibilities, want)
	}
	for i := range want {
		if api.imageVisibilities[i] != want[i] {
			t.Fatalf("DescribeImages visibility[%d] = %q, want %q", i, api.imageVisibilities[i], want[i])
		}
	}
	if api.requestedPublic {
		t.Fatalf("DescribeImages requested PUBLIC visibility; the AWS-managed catalog must not be scanned")
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceAppStream,
	}
}

type fakeAppStreamAPI struct {
	fleets   [][]awsappstreamtypes.Fleet
	fleetIdx int

	stacks   [][]awsappstreamtypes.Stack
	stackIdx int

	builders   [][]awsappstreamtypes.ImageBuilder
	builderIdx int

	imagesByType map[awsappstreamtypes.VisibilityType][][]awsappstreamtypes.Image
	imageIdx     map[awsappstreamtypes.VisibilityType]int

	imageVisibilities []awsappstreamtypes.VisibilityType
	requestedPublic   bool

	associatedStacks map[string][][]string
	associatedIdx    map[string]int

	tags map[string]map[string]string
}

func (f *fakeAppStreamAPI) DescribeFleets(
	_ context.Context,
	_ *awsappstream.DescribeFleetsInput,
	_ ...func(*awsappstream.Options),
) (*awsappstream.DescribeFleetsOutput, error) {
	if f.fleetIdx >= len(f.fleets) {
		return &awsappstream.DescribeFleetsOutput{}, nil
	}
	page := f.fleets[f.fleetIdx]
	f.fleetIdx++
	return &awsappstream.DescribeFleetsOutput{Fleets: page}, nil
}

func (f *fakeAppStreamAPI) DescribeStacks(
	_ context.Context,
	_ *awsappstream.DescribeStacksInput,
	_ ...func(*awsappstream.Options),
) (*awsappstream.DescribeStacksOutput, error) {
	if f.stackIdx >= len(f.stacks) {
		return &awsappstream.DescribeStacksOutput{}, nil
	}
	page := f.stacks[f.stackIdx]
	f.stackIdx++
	return &awsappstream.DescribeStacksOutput{Stacks: page}, nil
}

func (f *fakeAppStreamAPI) DescribeImageBuilders(
	_ context.Context,
	_ *awsappstream.DescribeImageBuildersInput,
	_ ...func(*awsappstream.Options),
) (*awsappstream.DescribeImageBuildersOutput, error) {
	if f.builderIdx >= len(f.builders) {
		return &awsappstream.DescribeImageBuildersOutput{}, nil
	}
	page := f.builders[f.builderIdx]
	f.builderIdx++
	return &awsappstream.DescribeImageBuildersOutput{ImageBuilders: page}, nil
}

func (f *fakeAppStreamAPI) DescribeImages(
	_ context.Context,
	input *awsappstream.DescribeImagesInput,
	_ ...func(*awsappstream.Options),
) (*awsappstream.DescribeImagesOutput, error) {
	if input.NextToken == nil {
		f.imageVisibilities = append(f.imageVisibilities, input.Type)
	}
	if input.Type == awsappstreamtypes.VisibilityTypePublic {
		f.requestedPublic = true
	}
	if f.imageIdx == nil {
		f.imageIdx = map[awsappstreamtypes.VisibilityType]int{}
	}
	pages := f.imagesByType[input.Type]
	idx := f.imageIdx[input.Type]
	if idx >= len(pages) {
		return &awsappstream.DescribeImagesOutput{}, nil
	}
	f.imageIdx[input.Type] = idx + 1
	return &awsappstream.DescribeImagesOutput{Images: pages[idx]}, nil
}

func (f *fakeAppStreamAPI) ListAssociatedStacks(
	_ context.Context,
	input *awsappstream.ListAssociatedStacksInput,
	_ ...func(*awsappstream.Options),
) (*awsappstream.ListAssociatedStacksOutput, error) {
	if f.associatedIdx == nil {
		f.associatedIdx = map[string]int{}
	}
	name := aws.ToString(input.FleetName)
	pages := f.associatedStacks[name]
	idx := f.associatedIdx[name]
	if idx >= len(pages) {
		return &awsappstream.ListAssociatedStacksOutput{}, nil
	}
	f.associatedIdx[name] = idx + 1
	return &awsappstream.ListAssociatedStacksOutput{Names: pages[idx]}, nil
}

func (f *fakeAppStreamAPI) ListTagsForResource(
	_ context.Context,
	input *awsappstream.ListTagsForResourceInput,
	_ ...func(*awsappstream.Options),
) (*awsappstream.ListTagsForResourceOutput, error) {
	return &awsappstream.ListTagsForResourceOutput{
		Tags: f.tags[aws.ToString(input.ResourceArn)],
	}, nil
}
