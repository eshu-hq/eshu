// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsappflow "github.com/aws/aws-sdk-go-v2/service/appflow"
	awsappflowtypes "github.com/aws/aws-sdk-go-v2/service/appflow/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

type fakeAPI struct {
	flows                []awsappflowtypes.FlowDefinition
	describe             map[string]*awsappflow.DescribeFlowOutput
	profiles             []awsappflowtypes.ConnectorProfile
	describeFlowCalls    int
	listFlowsCalls       int
	describeProfileCalls int
}

func (f *fakeAPI) ListFlows(_ context.Context, _ *awsappflow.ListFlowsInput, _ ...func(*awsappflow.Options)) (*awsappflow.ListFlowsOutput, error) {
	f.listFlowsCalls++
	return &awsappflow.ListFlowsOutput{Flows: f.flows}, nil
}

func (f *fakeAPI) DescribeFlow(_ context.Context, in *awsappflow.DescribeFlowInput, _ ...func(*awsappflow.Options)) (*awsappflow.DescribeFlowOutput, error) {
	f.describeFlowCalls++
	return f.describe[aws.ToString(in.FlowName)], nil
}

func (f *fakeAPI) DescribeConnectorProfiles(_ context.Context, _ *awsappflow.DescribeConnectorProfilesInput, _ ...func(*awsappflow.Options)) (*awsappflow.DescribeConnectorProfilesOutput, error) {
	f.describeProfileCalls++
	return &awsappflow.DescribeConnectorProfilesOutput{ConnectorProfileDetails: f.profiles}, nil
}

func TestClientListFlowsMapsSafeMetadata(t *testing.T) {
	api := &fakeAPI{
		flows: []awsappflowtypes.FlowDefinition{{
			FlowArn:                  aws.String("arn:aws:appflow:us-east-1:123456789012:flow/orders"),
			FlowName:                 aws.String("orders"),
			FlowStatus:               awsappflowtypes.FlowStatusActive,
			SourceConnectorType:      awsappflowtypes.ConnectorTypeS3,
			DestinationConnectorType: awsappflowtypes.ConnectorTypeSalesforce,
			TriggerType:              awsappflowtypes.TriggerTypeScheduled,
		}},
		describe: map[string]*awsappflow.DescribeFlowOutput{
			"orders": {
				FlowArn:    aws.String("arn:aws:appflow:us-east-1:123456789012:flow/orders"),
				FlowStatus: awsappflowtypes.FlowStatusActive,
				KmsArn:     aws.String("arn:aws:kms:us-east-1:123456789012:key/abcd"),
				SourceFlowConfig: &awsappflowtypes.SourceFlowConfig{
					ConnectorType: awsappflowtypes.ConnectorTypeS3,
					SourceConnectorProperties: &awsappflowtypes.SourceConnectorProperties{
						S3: &awsappflowtypes.S3SourceProperties{BucketName: aws.String("orders-landing")},
					},
				},
				DestinationFlowConfigList: []awsappflowtypes.DestinationFlowConfig{{
					ConnectorType:        awsappflowtypes.ConnectorTypeSalesforce,
					ConnectorProfileName: aws.String("salesforce-prod"),
				}},
				// Tasks (field mappings) intentionally populated to prove the
				// adapter never reads them.
				Tasks: []awsappflowtypes.Task{{
					TaskType:         awsappflowtypes.TaskTypeMap,
					SourceFields:     []string{"ssn", "credit_card"},
					DestinationField: aws.String("secret_field"),
				}},
			},
		},
	}
	client := &Client{client: api, boundary: testBoundary()}

	flows, err := client.ListFlows(context.Background())
	if err != nil {
		t.Fatalf("ListFlows() error = %v", err)
	}
	if len(flows) != 1 {
		t.Fatalf("ListFlows() returned %d flows, want 1", len(flows))
	}
	flow := flows[0]
	if flow.SourceS3Bucket != "orders-landing" {
		t.Fatalf("SourceS3Bucket = %q, want orders-landing", flow.SourceS3Bucket)
	}
	if flow.DestinationConnectorProfileName != "salesforce-prod" {
		t.Fatalf("DestinationConnectorProfileName = %q, want salesforce-prod", flow.DestinationConnectorProfileName)
	}
	if flow.KMSKeyARN != "arn:aws:kms:us-east-1:123456789012:key/abcd" {
		t.Fatalf("KMSKeyARN = %q", flow.KMSKeyARN)
	}
	if flow.TriggerType != "Scheduled" {
		t.Fatalf("TriggerType = %q, want Scheduled", flow.TriggerType)
	}
	// The scanner-owned Flow type has no field that can hold field mappings or
	// run records; this is the structural guarantee the data values never leak.
	flowType := reflect.TypeOf(flow)
	for i := 0; i < flowType.NumField(); i++ {
		name := flowType.Field(i).Name
		for _, banned := range []string{"Task", "Mapping", "Field", "Record", "Credential", "Token", "Secret"} {
			if strings.Contains(name, banned) {
				t.Fatalf("scanner Flow type field %q resembles forbidden data %q", name, banned)
			}
		}
	}
}

// TestClientListFlowsCapturesEveryDestination pins that a flow with more than
// one destination in DestinationFlowConfigList carries every destination
// forward, not just the first. AppFlow supports fan-out flows (for example two
// S3 buckets, or an S3 bucket plus a connector-profile destination); flattening
// to a single destination would drop the additional destination's S3 and
// connector-profile graph edges, leaving incomplete graph evidence.
func TestClientListFlowsCapturesEveryDestination(t *testing.T) {
	api := &fakeAPI{
		flows: []awsappflowtypes.FlowDefinition{{
			FlowArn:  aws.String("arn:aws:appflow:us-east-1:123456789012:flow/fanout"),
			FlowName: aws.String("fanout"),
		}},
		describe: map[string]*awsappflow.DescribeFlowOutput{
			"fanout": {
				FlowArn: aws.String("arn:aws:appflow:us-east-1:123456789012:flow/fanout"),
				DestinationFlowConfigList: []awsappflowtypes.DestinationFlowConfig{
					{
						ConnectorType: awsappflowtypes.ConnectorTypeS3,
						DestinationConnectorProperties: &awsappflowtypes.DestinationConnectorProperties{
							S3: &awsappflowtypes.S3DestinationProperties{BucketName: aws.String("primary-out")},
						},
					},
					{
						ConnectorType: awsappflowtypes.ConnectorTypeS3,
						DestinationConnectorProperties: &awsappflowtypes.DestinationConnectorProperties{
							S3: &awsappflowtypes.S3DestinationProperties{BucketName: aws.String("secondary-out")},
						},
					},
					{
						ConnectorType:        awsappflowtypes.ConnectorTypeSalesforce,
						ConnectorProfileName: aws.String("salesforce-prod"),
					},
				},
			},
		},
	}
	client := &Client{client: api, boundary: testBoundary()}

	flows, err := client.ListFlows(context.Background())
	if err != nil {
		t.Fatalf("ListFlows() error = %v", err)
	}
	if len(flows) != 1 {
		t.Fatalf("ListFlows() returned %d flows, want 1", len(flows))
	}
	flow := flows[0]
	if len(flow.Destinations) != 3 {
		t.Fatalf("flow.Destinations length = %d, want 3 (every reported destination)", len(flow.Destinations))
	}
	wantBuckets := []string{"primary-out", "secondary-out", ""}
	wantProfiles := []string{"", "", "salesforce-prod"}
	for i, dest := range flow.Destinations {
		if dest.S3Bucket != wantBuckets[i] {
			t.Fatalf("Destinations[%d].S3Bucket = %q, want %q", i, dest.S3Bucket, wantBuckets[i])
		}
		if dest.ConnectorProfileName != wantProfiles[i] {
			t.Fatalf("Destinations[%d].ConnectorProfileName = %q, want %q", i, dest.ConnectorProfileName, wantProfiles[i])
		}
	}
	// The flattened summary fields still reflect the first destination for the
	// resource attributes, preserving existing behaviour.
	if flow.DestinationS3Bucket != "primary-out" {
		t.Fatalf("DestinationS3Bucket = %q, want primary-out (first destination)", flow.DestinationS3Bucket)
	}
}

func TestClientListConnectorProfilesForwardsOnlyCredentialARN(t *testing.T) {
	api := &fakeAPI{
		profiles: []awsappflowtypes.ConnectorProfile{{
			ConnectorProfileArn:  aws.String("arn:aws:appflow:us-east-1:123456789012:connectorprofile/sf"),
			ConnectorProfileName: aws.String("sf"),
			ConnectorType:        awsappflowtypes.ConnectorTypeSalesforce,
			ConnectionMode:       awsappflowtypes.ConnectionModePublic,
			CredentialsArn:       aws.String("arn:aws:secretsmanager:us-east-1:123456789012:secret:appflow!sf-Ab3"),
		}},
	}
	client := &Client{client: api, boundary: testBoundary()}

	profiles, err := client.ListConnectorProfiles(context.Background())
	if err != nil {
		t.Fatalf("ListConnectorProfiles() error = %v", err)
	}
	if len(profiles) != 1 {
		t.Fatalf("ListConnectorProfiles() returned %d, want 1", len(profiles))
	}
	if profiles[0].CredentialsARN != "arn:aws:secretsmanager:us-east-1:123456789012:secret:appflow!sf-Ab3" {
		t.Fatalf("CredentialsARN = %q", profiles[0].CredentialsARN)
	}
	// The scanner-owned ConnectorProfile type carries no credential-value field.
	profileType := reflect.TypeOf(profiles[0])
	for i := 0; i < profileType.NumField(); i++ {
		name := profileType.Field(i).Name
		if name == "CredentialsARN" {
			continue
		}
		for _, banned := range []string{"Credential", "Token", "Password", "Secret"} {
			if strings.Contains(name, banned) {
				t.Fatalf("scanner ConnectorProfile field %q resembles forbidden credential %q", name, banned)
			}
		}
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceAppFlow,
	}
}
