// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	cdtypes "github.com/aws/aws-sdk-go-v2/service/codedeploy/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/codedeploy"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// TestAPIClientInterfaceExcludesMutationAndRevisionAPIs proves the AWS SDK
// surface this adapter accepts never lists a CodeDeploy mutation, deployment
// data-plane, or instance data-plane API as a callable method. It is the
// reflective guard the issue requires first: a maintainer cannot widen the
// metadata-only contract to reach mutation or revision-body APIs without
// failing this test.
func TestAPIClientInterfaceExcludesMutationAndRevisionAPIs(t *testing.T) {
	clientType := reflect.TypeOf((*apiClient)(nil)).Elem()
	forbidden := []string{
		// Application/group/config mutation.
		"CreateApplication",
		"UpdateApplication",
		"DeleteApplication",
		"CreateDeploymentGroup",
		"UpdateDeploymentGroup",
		"DeleteDeploymentGroup",
		"CreateDeploymentConfig",
		"DeleteDeploymentConfig",
		// Deployment data-plane mutation.
		"CreateDeployment",
		"StopDeployment",
		"ContinueDeployment",
		"RegisterApplicationRevision",
		"DeleteResourcesByExternalId",
		// On-premises instance mutation.
		"RegisterOnPremisesInstance",
		"DeregisterOnPremisesInstance",
		"AddTagsToOnPremisesInstances",
		"RemoveTagsFromOnPremisesInstances",
		// Lifecycle and GitHub mutation.
		"PutLifecycleEventHookExecutionStatus",
		"DeleteGitHubAccountToken",
		"TagResource",
		"UntagResource",
		// Instance data-plane reads excluded from the metadata scanner.
		"BatchGetDeploymentInstances",
		"GetDeploymentInstance",
		"ListDeploymentInstances",
		"BatchGetDeploymentTargets",
		"GetDeploymentTarget",
		"ListDeploymentTargets",
		// Revision-body reads (carry appspec.yml content).
		"GetApplicationRevision",
		"BatchGetApplicationRevisions",
	}
	for _, name := range forbidden {
		if _, ok := clientType.MethodByName(name); ok {
			t.Fatalf("apiClient exposes forbidden method %q; CodeDeploy SDK adapter must stay metadata-only", name)
		}
	}
}

func testKey(t *testing.T) redact.Key {
	t.Helper()
	key, err := redact.NewKey([]byte("codedeploy-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	return key
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceCodeDeploy,
	}
}

func TestClientListApplicationsMapsMetadataAndTags(t *testing.T) {
	api := &fakeCodeDeployAPI{
		applications: []string{"checkout"},
		applicationInfo: map[string]cdtypes.ApplicationInfo{
			"checkout": {
				ApplicationName: aws.String("checkout"),
				ApplicationId:   aws.String("app-123"),
				ComputePlatform: cdtypes.ComputePlatformServer,
				CreateTime:      aws.Time(testTime),
			},
		},
		tags: map[string]map[string]string{
			"arn:aws:codedeploy:us-east-1:123456789012:application:checkout": {"Team": "payments"},
		},
	}
	client := newTestClient(api, testKey(t))

	apps, err := client.ListApplications(context.Background())
	if err != nil {
		t.Fatalf("ListApplications() error = %v", err)
	}
	if len(apps) != 1 {
		t.Fatalf("applications = %d, want 1", len(apps))
	}
	if apps[0].ComputePlatform != "Server" {
		t.Fatalf("ComputePlatform = %q, want Server", apps[0].ComputePlatform)
	}
	if apps[0].Tags["Team"] != "payments" {
		t.Fatalf("Tags = %#v, want Team=payments", apps[0].Tags)
	}
}

func TestClientListDeploymentGroupsRedactsOnPremisesTagValues(t *testing.T) {
	api := &fakeCodeDeployAPI{
		deploymentGroupsByApp: map[string][]string{"checkout": {"checkout-prod"}},
		deploymentGroupInfo: map[string]cdtypes.DeploymentGroupInfo{
			"checkout-prod": {
				DeploymentGroupName: aws.String("checkout-prod"),
				ApplicationName:     aws.String("checkout"),
				ComputePlatform:     cdtypes.ComputePlatformServer,
				ServiceRoleArn:      aws.String("arn:aws:iam::123456789012:role/CodeDeployServiceRole"),
				DeploymentStyle: &cdtypes.DeploymentStyle{
					DeploymentType:   cdtypes.DeploymentTypeBlueGreen,
					DeploymentOption: cdtypes.DeploymentOptionWithTrafficControl,
				},
				AutoRollbackConfiguration: &cdtypes.AutoRollbackConfiguration{
					Enabled: true,
					Events:  []cdtypes.AutoRollbackEvent{cdtypes.AutoRollbackEventDeploymentFailure},
				},
				AutoScalingGroups: []cdtypes.AutoScalingGroup{{Name: aws.String("checkout-asg")}},
				TriggerConfigurations: []cdtypes.TriggerConfig{{
					TriggerName:      aws.String("prod-alerts"),
					TriggerTargetArn: aws.String("arn:aws:sns:us-east-1:123456789012:codedeploy-alerts"),
				}},
				OnPremisesInstanceTagFilters: []cdtypes.TagFilter{{
					Key:   aws.String("owner-email"),
					Value: aws.String("john.doe@example.com"),
					Type:  cdtypes.TagFilterTypeKeyAndValue,
				}},
			},
		},
	}
	client := newTestClient(api, testKey(t))

	groups, err := client.ListDeploymentGroups(context.Background(), "checkout")
	if err != nil {
		t.Fatalf("ListDeploymentGroups() error = %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("groups = %d, want 1", len(groups))
	}
	group := groups[0]
	if group.DeploymentStyle.DeploymentType != "BLUE_GREEN" {
		t.Fatalf("DeploymentType = %q, want BLUE_GREEN", group.DeploymentStyle.DeploymentType)
	}
	if len(group.OnPremisesTagFilterSummary) != 1 {
		t.Fatalf("on-prem tag filters = %d, want 1", len(group.OnPremisesTagFilterSummary))
	}
	filter := group.OnPremisesTagFilterSummary[0]
	if filter.Key != "owner-email" {
		t.Fatalf("filter key = %q, want owner-email", filter.Key)
	}
	if !filter.HasValue {
		t.Fatalf("filter HasValue = false, want true")
	}
	marker, _ := filter.ValueMarker["marker"].(string)
	if marker == "" {
		t.Fatalf("on-prem tag value not redacted: %#v", filter.ValueMarker)
	}
	if marker == "john.doe@example.com" {
		t.Fatalf("on-prem tag value leaked raw value")
	}
}

func TestClientListDeploymentGroupsChunksBatchGetByAWSLimit(t *testing.T) {
	const total = awsBatchGetDeploymentGroupsLimit + 5
	names := make([]string, 0, total)
	info := make(map[string]cdtypes.DeploymentGroupInfo, total)
	for i := 0; i < total; i++ {
		name := fmt.Sprintf("group-%03d", i)
		names = append(names, name)
		info[name] = cdtypes.DeploymentGroupInfo{
			DeploymentGroupName: aws.String(name),
			ApplicationName:     aws.String("checkout"),
			ComputePlatform:     cdtypes.ComputePlatformServer,
		}
	}
	api := &fakeCodeDeployAPI{
		deploymentGroupsByApp: map[string][]string{"checkout": names},
		deploymentGroupInfo:   info,
	}
	client := newTestClient(api, testKey(t))

	groups, err := client.ListDeploymentGroups(context.Background(), "checkout")
	if err != nil {
		t.Fatalf("ListDeploymentGroups() error = %v", err)
	}
	if len(groups) != total {
		t.Fatalf("groups = %d, want %d (BatchGet must chunk by the AWS 100-name cap)", len(groups), total)
	}
}

func TestClientListRecentDeploymentsKeepsSafeRevisionRefsAndDropsAppSpec(t *testing.T) {
	api := &fakeCodeDeployAPI{
		deploymentIDs: []string{"d-ABCDE1234"},
		deploymentInfo: map[string]cdtypes.DeploymentInfo{
			"d-ABCDE1234": {
				DeploymentId:        aws.String("d-ABCDE1234"),
				ApplicationName:     aws.String("checkout"),
				DeploymentGroupName: aws.String("checkout-prod"),
				Status:              cdtypes.DeploymentStatusSucceeded,
				ComputePlatform:     cdtypes.ComputePlatformServer,
				Revision: &cdtypes.RevisionLocation{
					RevisionType: cdtypes.RevisionLocationTypeS3,
					S3Location: &cdtypes.S3Location{
						Bucket:     aws.String("checkout-artifacts"),
						Key:        aws.String("releases/checkout-1.2.3.zip"),
						Version:    aws.String("v42"),
						BundleType: cdtypes.BundleTypeZip,
					},
					// AppSpecContent and String_ carry lifecycle-hook bodies and
					// must never reach the scanner-owned RevisionSummary.
					AppSpecContent: &cdtypes.AppSpecContent{
						Content: aws.String("hooks:\n  BeforeInstall:\n    - location: scripts/rm-rf.sh"),
						Sha256:  aws.String("abc123"),
					},
					String_: &cdtypes.RawString{
						Content: aws.String("version: 0.0\nhooks: {BeforeAllowTraffic: [{location: validate.sh}]}"),
					},
				},
			},
		},
	}
	client := newTestClient(api, testKey(t))

	deployments, err := client.ListRecentDeployments(context.Background())
	if err != nil {
		t.Fatalf("ListRecentDeployments() error = %v", err)
	}
	if len(deployments) != 1 {
		t.Fatalf("deployments = %d, want 1", len(deployments))
	}
	revision := deployments[0].RevisionSummary
	if revision.S3Bucket != "checkout-artifacts" || revision.S3Key != "releases/checkout-1.2.3.zip" {
		t.Fatalf("revision S3 refs = %#v, want safe bucket/key", revision)
	}
	if revision.S3Version != "v42" {
		t.Fatalf("revision S3 version = %q, want v42", revision.S3Version)
	}
	// The scanner-owned RevisionSummary has no field able to hold appspec
	// bodies. Re-marshalling proves the adapter never copies hook content.
	if leaks := revisionSummaryLeaksAppSpec(revision); leaks != "" {
		t.Fatalf("revision summary leaked appspec content: %q", leaks)
	}
}

func revisionSummaryLeaksAppSpec(revision codedeploy.RevisionSummary) string {
	value := reflect.ValueOf(revision)
	forbidden := []string{"rm-rf.sh", "BeforeInstall", "BeforeAllowTraffic", "validate.sh", "hooks"}
	for i := 0; i < value.NumField(); i++ {
		field := value.Field(i)
		if field.Kind() != reflect.String {
			continue
		}
		for _, needle := range forbidden {
			if strings.Contains(strings.ToLower(field.String()), strings.ToLower(needle)) {
				return field.String()
			}
		}
	}
	return ""
}
