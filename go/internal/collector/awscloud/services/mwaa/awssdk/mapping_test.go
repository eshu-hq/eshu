// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"context"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsmwaa "github.com/aws/aws-sdk-go-v2/service/mwaa"
	awsmwaatypes "github.com/aws/aws-sdk-go-v2/service/mwaa/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	mwaaservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/mwaa"
)

func TestClientListEnvironmentsMapsSafeMetadataAndDropsAirflowConfig(t *testing.T) {
	fake := &fakeMWAAAPI{
		environments: []string{"analytics-airflow"},
		details: map[string]awsmwaatypes.Environment{
			"analytics-airflow": {
				Name:                aws.String("analytics-airflow"),
				Arn:                 aws.String("arn:aws:airflow:us-east-1:123456789012:environment/analytics-airflow"),
				Status:              awsmwaatypes.EnvironmentStatusAvailable,
				AirflowVersion:      aws.String("2.10.1"),
				WebserverAccessMode: awsmwaatypes.WebserverAccessModePublicOnly,
				EnvironmentClass:    aws.String("mw1.small"),
				SourceBucketArn:     aws.String("arn:aws:s3:::analytics-airflow-dags"),
				ExecutionRoleArn:    aws.String("arn:aws:iam::123456789012:role/mwaa-execution"),
				KmsKey:              aws.String("arn:aws:kms:us-east-1:123456789012:key/abcd"),
				// AirflowConfigurationOptions carries secret-shaped Airflow option
				// values and MUST NOT survive the mapper.
				AirflowConfigurationOptions: map[string]string{
					"core.fernet_key":      "super-secret-fernet-key",
					"smtp.smtp_password":   "smtp-secret",
					"webserver.secret_key": "flask-secret",
				},
				CeleryExecutorQueue: aws.String("arn:aws:sqs:us-east-1:123456789012:celery-queue"),
				WebserverUrl:        aws.String("https://example.c2.us-east-1.airflow.amazonaws.com"),
				NetworkConfiguration: &awsmwaatypes.NetworkConfiguration{
					SubnetIds:        []string{"subnet-aaa", "subnet-bbb"},
					SecurityGroupIds: []string{"sg-111"},
				},
				LoggingConfiguration: &awsmwaatypes.LoggingConfiguration{
					DagProcessingLogs: &awsmwaatypes.ModuleLoggingConfiguration{
						CloudWatchLogGroupArn: aws.String("arn:aws:logs:us-east-1:123456789012:log-group:airflow-analytics-DAGProcessing:*"),
						Enabled:               aws.Bool(true),
						LogLevel:              awsmwaatypes.LoggingLevelInfo,
					},
				},
				Tags: map[string]string{"team": "data"},
			},
		},
	}

	client := &Client{client: fake, boundary: awscloud.Boundary{ServiceKind: awscloud.ServiceMWAA}}
	environments, err := client.ListEnvironments(context.Background())
	if err != nil {
		t.Fatalf("ListEnvironments() error = %v, want nil", err)
	}
	if len(environments) != 1 {
		t.Fatalf("environment count = %d, want 1", len(environments))
	}
	environment := environments[0]
	if environment.Name != "analytics-airflow" {
		t.Fatalf("environment name = %q, want analytics-airflow", environment.Name)
	}
	if environment.SourceBucketARN != "arn:aws:s3:::analytics-airflow-dags" {
		t.Fatalf("source bucket arn = %q", environment.SourceBucketARN)
	}
	if got := len(environment.SubnetIDs); got != 2 {
		t.Fatalf("subnet id count = %d, want 2", got)
	}
	if got := len(environment.SecurityGroupIDs); got != 1 {
		t.Fatalf("security group id count = %d, want 1", got)
	}
	if got := len(environment.LogGroups); got != 1 {
		t.Fatalf("log group count = %d, want 1", got)
	}
	if environment.LogGroups[0].ARN != "arn:aws:logs:us-east-1:123456789012:log-group:airflow-analytics-DAGProcessing:*" {
		t.Fatalf("log group arn = %q; mapper must preserve the raw ARN for the scanner to trim", environment.LogGroups[0].ARN)
	}
	// The scanner-owned Environment type cannot hold AirflowConfigurationOptions
	// at all; this assertion documents that the values never escape the mapper.
	assertNoSecretSubstring(t, environment)
}

func TestClientGetEnvironmentNilEnvironmentFallsBackToName(t *testing.T) {
	fake := &fakeMWAAAPI{environments: []string{"empty-env"}, details: map[string]awsmwaatypes.Environment{}}
	client := &Client{client: fake, boundary: awscloud.Boundary{ServiceKind: awscloud.ServiceMWAA}}
	environments, err := client.ListEnvironments(context.Background())
	if err != nil {
		t.Fatalf("ListEnvironments() error = %v, want nil", err)
	}
	if len(environments) != 1 || environments[0].Name != "empty-env" {
		t.Fatalf("environments = %#v, want one named empty-env", environments)
	}
}

func assertNoSecretSubstring(t *testing.T, environment mwaaservice.Environment) {
	t.Helper()
	for _, value := range []string{
		environment.Name, environment.ARN, environment.Status, environment.AirflowVersion,
		environment.WebserverAccessMode, environment.EnvironmentClass, environment.EndpointManagement,
		environment.SourceBucketARN, environment.ExecutionRoleARN, environment.ServiceRoleARN,
		environment.KMSKey,
	} {
		for _, secret := range []string{"super-secret-fernet-key", "smtp-secret", "flask-secret", "celery-queue", "airflow.amazonaws.com"} {
			if strings.Contains(value, secret) {
				t.Fatalf("mapped field %q leaked forbidden value %q", value, secret)
			}
		}
	}
}

type fakeMWAAAPI struct {
	environments []string
	details      map[string]awsmwaatypes.Environment
}

func (f *fakeMWAAAPI) ListEnvironments(
	_ context.Context,
	_ *awsmwaa.ListEnvironmentsInput,
	_ ...func(*awsmwaa.Options),
) (*awsmwaa.ListEnvironmentsOutput, error) {
	return &awsmwaa.ListEnvironmentsOutput{Environments: f.environments}, nil
}

func (f *fakeMWAAAPI) GetEnvironment(
	_ context.Context,
	input *awsmwaa.GetEnvironmentInput,
	_ ...func(*awsmwaa.Options),
) (*awsmwaa.GetEnvironmentOutput, error) {
	name := aws.ToString(input.Name)
	detail, ok := f.details[name]
	if !ok {
		return &awsmwaa.GetEnvironmentOutput{}, nil
	}
	return &awsmwaa.GetEnvironmentOutput{Environment: &detail}, nil
}
