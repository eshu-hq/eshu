// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsbatchtypes "github.com/aws/aws-sdk-go-v2/service/batch/types"

	batchservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/batch"
)

// TestMapJobDefinitionDropsCommandAndKeepsSecretReference proves the adapter
// never carries a container command list across the boundary and that secret
// references survive as ARN refs while the environment value is preserved only
// for downstream scanner redaction.
func TestMapJobDefinitionDropsCommandAndKeepsSecretReference(t *testing.T) {
	jobDefinition := mapJobDefinition(awsbatchtypes.JobDefinition{
		JobDefinitionArn:  aws.String("arn:aws:batch:us-east-1:123456789012:job-definition/etl:3"),
		JobDefinitionName: aws.String("etl"),
		Revision:          aws.Int32(3),
		Type:              aws.String("container"),
		Status:            aws.String("ACTIVE"),
		Parameters:        map[string]string{"forbidden": "should-not-be-mapped"},
		ContainerProperties: &awsbatchtypes.ContainerProperties{
			Image:            aws.String("123456789012.dkr.ecr.us-east-1.amazonaws.com/etl:prod"),
			JobRoleArn:       aws.String("arn:aws:iam::123456789012:role/etl-job"),
			ExecutionRoleArn: aws.String("arn:aws:iam::123456789012:role/etl-exec"),
			Command:          []string{"python", "/app/run.py", "--password", "hunter2"},
			Environment: []awsbatchtypes.KeyValuePair{{
				Name:  aws.String("DATABASE_URL"),
				Value: aws.String("postgres://user:password@db.internal/app"),
			}},
			Secrets: []awsbatchtypes.Secret{{
				Name:      aws.String("API_TOKEN"),
				ValueFrom: aws.String("arn:aws:secretsmanager:us-east-1:123456789012:secret:api-token"),
			}},
		},
	})

	if jobDefinition.Container == nil {
		t.Fatalf("container mapping missing")
	}
	// The scanner-owned Container type has no Command field, so the command list
	// is structurally unrepresentable. Assert that explicitly.
	if _, ok := reflect.TypeOf(batchservice.Container{}).FieldByName("Command"); ok {
		t.Fatalf("Container type must not declare a Command field")
	}
	if _, ok := reflect.TypeOf(batchservice.JobDefinition{}).FieldByName("Parameters"); ok {
		t.Fatalf("JobDefinition type must not declare a Parameters field")
	}
	if got := jobDefinition.Container.Secrets[0].ValueFrom; got != "arn:aws:secretsmanager:us-east-1:123456789012:secret:api-token" {
		t.Fatalf("secret ValueFrom = %q, want ARN reference", got)
	}
	if jobDefinition.Container.Environment[0].Value != "postgres://user:password@db.internal/app" {
		t.Fatalf("environment value was not preserved for scanner redaction")
	}
}

// TestMapSchedulingPolicyDropsFairShareState proves the adapter discards the
// FairsharePolicy weight state. The scanner-owned SchedulingPolicy type has no
// FairsharePolicy field, so the state is structurally unrepresentable.
func TestMapSchedulingPolicyDropsFairShareState(t *testing.T) {
	policy := mapSchedulingPolicy(awsbatchtypes.SchedulingPolicyDetail{
		Arn:  aws.String("arn:aws:batch:us-east-1:123456789012:scheduling-policy/fair"),
		Name: aws.String("fair"),
		FairsharePolicy: &awsbatchtypes.FairsharePolicy{
			ShareDecaySeconds:  aws.Int32(3600),
			ComputeReservation: aws.Int32(50),
			ShareDistribution: []awsbatchtypes.ShareAttributes{{
				ShareIdentifier: aws.String("tenant-a"),
				WeightFactor:    aws.Float32(0.1),
			}},
		},
	})
	if policy.Name != "fair" {
		t.Fatalf("policy name = %q, want fair", policy.Name)
	}
	for _, field := range []string{"FairsharePolicy", "ShareDistribution", "WeightFactor", "ComputeReservation"} {
		if _, ok := reflect.TypeOf(batchservice.SchedulingPolicy{}).FieldByName(field); ok {
			t.Fatalf("SchedulingPolicy type must not declare a %q field", field)
		}
	}
}

func TestMapComputeEnvironmentMapsNetworkingAndLaunchTemplate(t *testing.T) {
	computeEnvironment := mapComputeEnvironment(awsbatchtypes.ComputeEnvironmentDetail{
		ComputeEnvironmentArn:  aws.String("arn:aws:batch:us-east-1:123456789012:compute-environment/ec2-ce"),
		ComputeEnvironmentName: aws.String("ec2-ce"),
		Type:                   awsbatchtypes.CETypeManaged,
		State:                  awsbatchtypes.CEStateEnabled,
		ServiceRole:            aws.String("arn:aws:iam::123456789012:role/batch-service"),
		ComputeResources: &awsbatchtypes.ComputeResource{
			Type:             awsbatchtypes.CRTypeEc2,
			Subnets:          []string{"subnet-aaa", "subnet-bbb"},
			SecurityGroupIds: []string{"sg-111"},
			InstanceRole:     aws.String("arn:aws:iam::123456789012:instance-profile/ecsInstanceRole"),
			LaunchTemplate: &awsbatchtypes.LaunchTemplateSpecification{
				LaunchTemplateId: aws.String("lt-0abc123"),
			},
		},
	})
	if computeEnvironment.ComputeResource.LaunchTemplateID != "lt-0abc123" {
		t.Fatalf("launch template ID = %q", computeEnvironment.ComputeResource.LaunchTemplateID)
	}
	if computeEnvironment.InstanceRoleARN != "arn:aws:iam::123456789012:instance-profile/ecsInstanceRole" {
		t.Fatalf("instance role ARN = %q", computeEnvironment.InstanceRoleARN)
	}
	if len(computeEnvironment.ComputeResource.SubnetIDs) != 2 {
		t.Fatalf("subnet count = %d, want 2", len(computeEnvironment.ComputeResource.SubnetIDs))
	}
}

func TestMapJobCarriesIdentityOnly(t *testing.T) {
	job := mapJob(awsbatchtypes.JobSummary{
		JobId:         aws.String("job-1"),
		JobArn:        aws.String("arn:aws:batch:us-east-1:123456789012:job/job-1"),
		JobName:       aws.String("etl-run"),
		Status:        awsbatchtypes.JobStatusRunning,
		JobDefinition: aws.String("arn:aws:batch:us-east-1:123456789012:job-definition/etl:3"),
		CreatedAt:     aws.Int64(1748390400000),
	}, "arn:aws:batch:us-east-1:123456789012:job-queue/prod-queue")
	if job.Status != "RUNNING" {
		t.Fatalf("job status = %q, want RUNNING", job.Status)
	}
	if job.JobQueueARN != "arn:aws:batch:us-east-1:123456789012:job-queue/prod-queue" {
		t.Fatalf("job queue ARN = %q", job.JobQueueARN)
	}
	if job.CreatedAt.IsZero() {
		t.Fatalf("job created at not mapped")
	}
}

func TestChunkStringsSplitsAPILimits(t *testing.T) {
	chunks := chunkStrings([]string{"a", "b", "c"}, 2)
	if len(chunks) != 2 {
		t.Fatalf("chunk count = %d, want 2", len(chunks))
	}
	if len(chunks[0]) != 2 || len(chunks[1]) != 1 {
		t.Fatalf("chunks = %#v, want 2 then 1", chunks)
	}
}
