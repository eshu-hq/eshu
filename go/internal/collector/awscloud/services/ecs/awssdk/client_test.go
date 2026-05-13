package awssdk

import (
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
)

func TestMapTaskDefinitionPreservesSecretReferencesAndEnvValuesForScannerRedaction(t *testing.T) {
	registeredAt := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	taskDefinition := mapTaskDefinition(awsecstypes.TaskDefinition{
		ContainerDefinitions: []awsecstypes.ContainerDefinition{{
			Environment: []awsecstypes.KeyValuePair{{
				Name:  aws.String("DATABASE_URL"),
				Value: aws.String("postgres://user:password@example.internal/app"),
			}},
			Essential: aws.Bool(true),
			Image:     aws.String("123456789012.dkr.ecr.us-east-1.amazonaws.com/team/api:prod"),
			Name:      aws.String("api"),
			Secrets: []awsecstypes.Secret{{
				Name:      aws.String("API_TOKEN"),
				ValueFrom: aws.String("arn:aws:secretsmanager:us-east-1:123456789012:secret:api-token"),
			}},
		}},
		Cpu:               aws.String("256"),
		ExecutionRoleArn:  aws.String("arn:aws:iam::123456789012:role/api-exec"),
		Family:            aws.String("api"),
		Memory:            aws.String("512"),
		NetworkMode:       awsecstypes.NetworkModeAwsvpc,
		RegisteredAt:      aws.Time(registeredAt),
		Revision:          7,
		Status:            awsecstypes.TaskDefinitionStatusActive,
		TaskDefinitionArn: aws.String("arn:aws:ecs:us-east-1:123456789012:task-definition/api:7"),
		TaskRoleArn:       aws.String("arn:aws:iam::123456789012:role/api-task"),
	})

	if taskDefinition.ARN != "arn:aws:ecs:us-east-1:123456789012:task-definition/api:7" {
		t.Fatalf("ARN = %q", taskDefinition.ARN)
	}
	if taskDefinition.Containers[0].Environment[0].Value != "postgres://user:password@example.internal/app" {
		t.Fatalf("environment value was not preserved for scanner redaction")
	}
	if got := taskDefinition.Containers[0].Secrets[0].ValueFrom; got != "arn:aws:secretsmanager:us-east-1:123456789012:secret:api-token" {
		t.Fatalf("secret ValueFrom = %q, want ARN reference", got)
	}
}

func TestChunkStringsSplitsAPILimits(t *testing.T) {
	values := []string{"a", "b", "c"}
	chunks := chunkStrings(values, 2)

	if len(chunks) != 2 {
		t.Fatalf("chunk count = %d, want 2", len(chunks))
	}
	if len(chunks[0]) != 2 || len(chunks[1]) != 1 {
		t.Fatalf("chunks = %#v, want 2 then 1", chunks)
	}
}
