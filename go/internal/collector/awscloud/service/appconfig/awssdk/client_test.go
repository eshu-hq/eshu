// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsappconfig "github.com/aws/aws-sdk-go-v2/service/appconfig"
	awsappconfigtypes "github.com/aws/aws-sdk-go-v2/service/appconfig/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func TestClientSnapshotsAppConfigMetadataOnly(t *testing.T) {
	alarmARN := "arn:aws:cloudwatch:us-east-1:123456789012:alarm:high-error-rate"
	roleARN := "arn:aws:iam::123456789012:role/appconfig-monitor"

	api := &fakeAppConfigAPI{
		applicationPages: []*awsappconfig.ListApplicationsOutput{{
			Items: []awsappconfigtypes.Application{{
				Id:          aws.String("app123"),
				Name:        aws.String("checkout"),
				Description: aws.String("checkout config"),
			}},
		}},
		environmentPages: map[string][]*awsappconfig.ListEnvironmentsOutput{
			"app123": {{
				Items: []awsappconfigtypes.Environment{{
					Id:            aws.String("env456"),
					ApplicationId: aws.String("app123"),
					Name:          aws.String("prod"),
					State:         awsappconfigtypes.EnvironmentStateReadyForDeployment,
					Monitors: []awsappconfigtypes.Monitor{{
						AlarmArn:     aws.String(alarmARN),
						AlarmRoleArn: aws.String(roleARN),
					}},
				}},
			}},
		},
		profilePages: map[string][]*awsappconfig.ListConfigurationProfilesOutput{
			"app123": {{
				Items: []awsappconfigtypes.ConfigurationProfileSummary{{
					Id:             aws.String("prof789"),
					ApplicationId:  aws.String("app123"),
					Name:           aws.String("feature-flags"),
					Type:           aws.String("AWS.AppConfig.FeatureFlags"),
					LocationUri:    aws.String("hosted"),
					ValidatorTypes: []awsappconfigtypes.ValidatorType{awsappconfigtypes.ValidatorTypeJsonSchema},
				}},
			}},
		},
		strategyPages: []*awsappconfig.ListDeploymentStrategiesOutput{{
			Items: []awsappconfigtypes.DeploymentStrategy{{
				Id:                          aws.String("strat012"),
				Name:                        aws.String("Canary10Percent20Minutes"),
				DeploymentDurationInMinutes: 20,
				FinalBakeTimeInMinutes:      10,
				GrowthFactor:                aws.Float32(10),
				GrowthType:                  awsappconfigtypes.GrowthTypeExponential,
				ReplicateTo:                 awsappconfigtypes.ReplicateToSsmDocument,
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
	application := snapshot.Applications[0]
	if application.ID != "app123" {
		t.Fatalf("application ID = %q, want app123", application.ID)
	}
	if len(application.Environments) != 1 {
		t.Fatalf("len(Environments) = %d, want 1", len(application.Environments))
	}
	environment := application.Environments[0]
	if environment.State != "READY_FOR_DEPLOYMENT" {
		t.Fatalf("environment State = %q, want READY_FOR_DEPLOYMENT", environment.State)
	}
	if len(environment.Monitors) != 1 {
		t.Fatalf("len(Monitors) = %d, want 1", len(environment.Monitors))
	}
	if environment.Monitors[0].AlarmARN != alarmARN {
		t.Fatalf("monitor AlarmARN = %q, want %q", environment.Monitors[0].AlarmARN, alarmARN)
	}
	if environment.Monitors[0].AlarmRoleARN != roleARN {
		t.Fatalf("monitor AlarmRoleARN = %q, want %q", environment.Monitors[0].AlarmRoleARN, roleARN)
	}
	if len(application.Profiles) != 1 {
		t.Fatalf("len(Profiles) = %d, want 1", len(application.Profiles))
	}
	profile := application.Profiles[0]
	if profile.Type != "AWS.AppConfig.FeatureFlags" {
		t.Fatalf("profile Type = %q, want AWS.AppConfig.FeatureFlags", profile.Type)
	}
	if len(profile.ValidatorTypes) != 1 || profile.ValidatorTypes[0] != "JSON_SCHEMA" {
		t.Fatalf("profile ValidatorTypes = %#v, want [JSON_SCHEMA]", profile.ValidatorTypes)
	}
	if len(snapshot.DeploymentStrategies) != 1 {
		t.Fatalf("len(DeploymentStrategies) = %d, want 1", len(snapshot.DeploymentStrategies))
	}
	strategy := snapshot.DeploymentStrategies[0]
	if strategy.GrowthType != "EXPONENTIAL" {
		t.Fatalf("strategy GrowthType = %q, want EXPONENTIAL", strategy.GrowthType)
	}
	if strategy.ReplicateTo != "SSM_DOCUMENT" {
		t.Fatalf("strategy ReplicateTo = %q, want SSM_DOCUMENT", strategy.ReplicateTo)
	}
	if strategy.DeploymentDurationInMinutes != 20 {
		t.Fatalf("strategy DeploymentDurationInMinutes = %d, want 20", strategy.DeploymentDurationInMinutes)
	}
}

func TestClientReturnsCleanlyForEmptyAccount(t *testing.T) {
	client := &Client{client: &fakeAppConfigAPI{}, boundary: testBoundary()}
	snapshot, err := client.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v, want nil", err)
	}
	if len(snapshot.Applications) != 0 {
		t.Fatalf("len(Applications) = %d, want 0 for empty account", len(snapshot.Applications))
	}
	if len(snapshot.DeploymentStrategies) != 0 {
		t.Fatalf("len(DeploymentStrategies) = %d, want 0 for empty account", len(snapshot.DeploymentStrategies))
	}
}

type fakeAppConfigAPI struct {
	applicationPages []*awsappconfig.ListApplicationsOutput
	applicationCall  int
	environmentPages map[string][]*awsappconfig.ListEnvironmentsOutput
	environmentCalls map[string]int
	profilePages     map[string][]*awsappconfig.ListConfigurationProfilesOutput
	profileCalls     map[string]int
	strategyPages    []*awsappconfig.ListDeploymentStrategiesOutput
	strategyCall     int
}

func (f *fakeAppConfigAPI) ListApplications(
	_ context.Context,
	_ *awsappconfig.ListApplicationsInput,
	_ ...func(*awsappconfig.Options),
) (*awsappconfig.ListApplicationsOutput, error) {
	if f.applicationCall >= len(f.applicationPages) {
		return &awsappconfig.ListApplicationsOutput{}, nil
	}
	page := f.applicationPages[f.applicationCall]
	f.applicationCall++
	return page, nil
}

func (f *fakeAppConfigAPI) ListEnvironments(
	_ context.Context,
	input *awsappconfig.ListEnvironmentsInput,
	_ ...func(*awsappconfig.Options),
) (*awsappconfig.ListEnvironmentsOutput, error) {
	if f.environmentCalls == nil {
		f.environmentCalls = map[string]int{}
	}
	id := aws.ToString(input.ApplicationId)
	pages := f.environmentPages[id]
	idx := f.environmentCalls[id]
	if idx >= len(pages) {
		return &awsappconfig.ListEnvironmentsOutput{}, nil
	}
	f.environmentCalls[id] = idx + 1
	return pages[idx], nil
}

func (f *fakeAppConfigAPI) ListConfigurationProfiles(
	_ context.Context,
	input *awsappconfig.ListConfigurationProfilesInput,
	_ ...func(*awsappconfig.Options),
) (*awsappconfig.ListConfigurationProfilesOutput, error) {
	if f.profileCalls == nil {
		f.profileCalls = map[string]int{}
	}
	id := aws.ToString(input.ApplicationId)
	pages := f.profilePages[id]
	idx := f.profileCalls[id]
	if idx >= len(pages) {
		return &awsappconfig.ListConfigurationProfilesOutput{}, nil
	}
	f.profileCalls[id] = idx + 1
	return pages[idx], nil
}

func (f *fakeAppConfigAPI) ListDeploymentStrategies(
	_ context.Context,
	_ *awsappconfig.ListDeploymentStrategiesInput,
	_ ...func(*awsappconfig.Options),
) (*awsappconfig.ListDeploymentStrategiesOutput, error) {
	if f.strategyCall >= len(f.strategyPages) {
		return &awsappconfig.ListDeploymentStrategiesOutput{}, nil
	}
	page := f.strategyPages[f.strategyCall]
	f.strategyCall++
	return page, nil
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:   "123456789012",
		Region:      "us-east-1",
		ServiceKind: awscloud.ServiceAppConfig,
	}
}
